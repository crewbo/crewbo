package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
)

func user(n int) auth.User { return auth.User{ID: int64(n), Login: fmt.Sprintf("user%d", n)} }

// newRooms returns Rooms over a fresh server database with a movable clock.
func newRooms(t *testing.T) (*Rooms, *time.Time) {
	t.Helper()
	s := newTestServer(t)
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	s.rooms.now = func() time.Time { return now }
	return s.rooms, &now
}

func TestRoomLifecycle(t *testing.T) {
	ctx := context.Background()
	rs, now := newRooms(t)
	lead, mate, stranger := user(1), user(2), user(3)
	room, err := rs.Create(ctx, lead, "crewbo")
	if err != nil || room.Role != "lead" || room.Members != 1 || room.LeadLogin != "user1" {
		t.Fatalf("Create = %+v %v", room, err)
	}
	if _, _, err := rs.Invite(ctx, stranger, room.ID, 1, time.Hour); !errors.Is(err, ErrNotMember) {
		t.Errorf("stranger invites: %v", err)
	}
	code, expires, err := rs.Invite(ctx, lead, room.ID, 2, time.Hour)
	if err != nil || !expires.Equal(now.Add(time.Hour)) {
		t.Fatalf("Invite = %v %v", expires, err)
	}
	var n int
	if err := rs.db.QueryRow(`SELECT count(*) FROM invites WHERE code_hash = ? OR CAST(code_hash AS TEXT) = ?`, []byte(code), code).Scan(&n); err != nil || n != 0 {
		t.Errorf("invite code stored in clear: %d %v", n, err)
	}
	info, err := rs.Join(ctx, mate, code)
	if err != nil || info.ID != room.ID || info.Role != "member" || info.Members != 2 || info.LeadLogin != "user1" {
		t.Fatalf("Join = %+v %v", info, err)
	}
	if _, _, err := rs.Invite(ctx, mate, room.ID, 1, time.Hour); !errors.Is(err, ErrNotLead) {
		t.Errorf("member invites: %v", err)
	}
	// Joining again does not use the invite up.
	if _, err := rs.Join(ctx, mate, code); err != nil {
		t.Errorf("rejoin: %v", err)
	}
	var uses int
	_ = rs.db.QueryRow(`SELECT uses FROM invites`).Scan(&uses)
	if uses != 1 {
		t.Errorf("uses = %d after one join and one rejoin", uses)
	}
	if list, err := rs.List(ctx, mate); err != nil || len(list) != 1 || list[0].ID != room.ID {
		t.Errorf("List = %+v %v", list, err)
	}
	if _, err := rs.Join(ctx, stranger, "cbi_nope"); !errors.Is(err, ErrInviteUnknown) {
		t.Errorf("unknown code: %v", err)
	}
	// The lead cannot leave while the mate is in; the mate can.
	if err := rs.Leave(ctx, lead, room.ID); !errors.Is(err, ErrLeadMustStay) {
		t.Errorf("lead leaves first: %v", err)
	}
	if err := rs.Leave(ctx, mate, room.ID); err != nil {
		t.Errorf("mate leaves: %v", err)
	}
	if err := rs.Leave(ctx, mate, room.ID); !errors.Is(err, ErrNotMember) {
		t.Errorf("leave twice: %v", err)
	}
	if list, _ := rs.List(ctx, mate); len(list) != 0 {
		t.Errorf("left room still listed: %+v", list)
	}
	// Second and last use; then used up; then expired.
	if _, err := rs.Join(ctx, mate, code); err != nil {
		t.Errorf("rejoin after leaving: %v", err)
	}
	if _, err := rs.Join(ctx, stranger, code); !errors.Is(err, ErrInviteUsedUp) {
		t.Errorf("third use of a 2-use invite: %v", err)
	}
	code2, _, _ := rs.Invite(ctx, lead, room.ID, 5, time.Hour)
	*now = now.Add(time.Hour)
	if _, err := rs.Join(ctx, stranger, code2); !errors.Is(err, ErrInviteExpired) {
		t.Errorf("expired invite: %v", err)
	}
	if err := rs.Leave(ctx, mate, room.ID); err != nil {
		t.Fatal(err)
	}
	if err := rs.Leave(ctx, lead, room.ID); err != nil {
		t.Errorf("lead leaves last: %v", err)
	}
}

func TestMemberCap(t *testing.T) {
	ctx := context.Background()
	rs, _ := newRooms(t)
	room, _ := rs.Create(ctx, user(1), "full")
	code, _, err := rs.Invite(ctx, user(1), room.ID, MaxMembers-1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= MaxMembers; i++ {
		if _, err := rs.Join(ctx, user(i), code); err != nil {
			t.Fatalf("member %d: %v", i, err)
		}
	}
	extra, _, _ := rs.Invite(ctx, user(1), room.ID, 3, time.Hour)
	if _, err := rs.Join(ctx, user(MaxMembers+1), extra); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("11th member: %v", err)
	}
	var uses int
	_ = rs.db.QueryRow(`SELECT uses FROM invites WHERE code_hash = ?`, hashToken(extra)).Scan(&uses)
	if uses != 0 {
		t.Errorf("a refused join used the invite: uses = %d", uses)
	}
	if err := rs.Leave(ctx, user(5), room.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(ctx, user(MaxMembers+1), extra); err != nil {
		t.Errorf("join after someone left: %v", err)
	}
}

// Twenty people join at once with a 20-use invite: the room stops at 10.
func TestConcurrentJoinsRespectTheCap(t *testing.T) {
	ctx := context.Background()
	rs, _ := newRooms(t)
	room, _ := rs.Create(ctx, user(1), "rush")
	if _, err := rs.db.Exec(`INSERT INTO invites (code_hash, room_id, created_by, created_unix_ms, expires_unix_ms, max_uses) VALUES (?, ?, 1, 0, ?, 20)`,
		hashToken("cbi_rush"), room.ID, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	joined, full := 0, 0
	for i := 2; i < 22; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := rs.Join(ctx, user(i), "cbi_rush")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				joined++
			case errors.Is(err, ErrRoomFull):
				full++
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var members, uses int
	_ = rs.db.QueryRow(`SELECT count(*) FROM members WHERE room_id = ? AND left_unix_ms IS NULL`, room.ID).Scan(&members)
	_ = rs.db.QueryRow(`SELECT uses FROM invites`).Scan(&uses)
	if joined != MaxMembers-1 || full != 20-(MaxMembers-1) || members != MaxMembers || uses != MaxMembers-1 {
		t.Errorf("joined %d, full %d, members %d, uses %d", joined, full, members, uses)
	}
}
