package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/store"
)

var t0 = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

func openLog(t *testing.T, path string) (*Log, *store.DB) {
	t.Helper()
	db, err := store.Open(context.Background(), path, store.Server)
	if err != nil {
		t.Fatal(err)
	}
	return NewLog(db.SQL()), db
}

func newLog(t *testing.T, rooms ...string) *Log {
	t.Helper()
	l, db := openLog(t, filepath.Join(t.TempDir(), "server.db"))
	t.Cleanup(func() { _ = db.Close() })
	for _, id := range rooms {
		if err := l.CreateRoom(context.Background(), id, "room "+id, t0); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func released(id string) *crewbov1.Event {
	return &crewbov1.Event{ActorMemberId: "m1", Body: &crewbov1.Event_ClaimsReleased{ClaimsReleased: &crewbov1.ClaimsReleased{ClaimIds: []string{id}}}}
}

func seqs(evs []*crewbov1.Event) []uint64 {
	out := make([]uint64, len(evs))
	for i, e := range evs {
		out[i] = e.GetSeq()
	}
	return out
}

// contiguous reports whether evs are exactly seq from, from+1, ... .
func contiguous(evs []*crewbov1.Event, from uint64) bool {
	for i, e := range evs {
		if e.GetSeq() != from+uint64(i) {
			return false
		}
	}
	return true
}

func TestLogAppendReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "server.db")
	l, db := openLog(t, path)
	if err := l.CreateRoom(ctx, "r1", "Room one", t0); err != nil {
		t.Fatal(err)
	}
	if err := l.CreateRoom(ctx, "r1", "again", t0); err == nil {
		t.Error("duplicate room id accepted")
	}
	in := released("c1")
	for i := range 3 {
		ev, err := l.Append(ctx, "r1", in, t0.Add(time.Duration(i)*time.Second))
		if err != nil || ev.GetSeq() != uint64(i+1) || ev.GetRoomId() != "r1" || ev.GetServerTimeUnixMs() != t0.Add(time.Duration(i)*time.Second).UnixMilli() {
			t.Fatalf("append %d = %v, %v", i, ev, err)
		}
	}
	if in.GetSeq() != 0 || in.GetRoomId() != "" {
		t.Error("Append changed the caller's event")
	}
	if _, err := l.Append(ctx, "nope", in, t0); !errors.Is(err, ErrNoRoom) {
		t.Errorf("unknown room: %v", err)
	}
	for after, want := range map[uint64][]uint64{0: {1, 2, 3}, 2: {3}, 3: nil, 9: nil} {
		got, err := l.Since(ctx, "r1", after)
		if err != nil || len(got) != len(want) || !contiguous(got, after+1) {
			t.Errorf("Since(%d) = %v %v, want %v", after, seqs(got), err, want)
		}
	}
	// Restart: the log survives and numbering continues.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	l, db = openLog(t, path)
	defer func() { _ = db.Close() }()
	all, err := l.Since(ctx, "r1", 0)
	if err != nil || !contiguous(all, 1) || len(all) != 3 || all[0].GetClaimsReleased().GetClaimIds()[0] != "c1" {
		t.Fatalf("after restart: %v %v", all, err)
	}
	if ev, err := l.Append(ctx, "r1", in, t0); err != nil || ev.GetSeq() != 4 {
		t.Fatalf("append after restart = %v %v", ev, err)
	}
}

// A failed insert must roll back the seq bump, or the log has a hole.
func TestFailedAppendLeavesNoGap(t *testing.T) {
	ctx := context.Background()
	l := newLog(t, "r1")
	if _, err := l.Append(ctx, "r1", released("a"), t0); err != nil {
		t.Fatal(err)
	}
	// Plant a row where seq 2 will go so the insert fails.
	if _, err := l.db.ExecContext(ctx, `INSERT INTO events (room_id, seq, payload) VALUES ('r1', 2, x'')`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(ctx, "r1", released("b"), t0); err == nil {
		t.Fatal("append over an existing seq succeeded")
	}
	if _, err := l.db.ExecContext(ctx, `DELETE FROM events WHERE room_id = 'r1' AND seq = 2`); err != nil {
		t.Fatal(err)
	}
	if ev, err := l.Append(ctx, "r1", released("c"), t0); err != nil || ev.GetSeq() != 2 {
		t.Fatalf("append after a failed one = %v %v, want seq 2", ev, err)
	}
}

func startRoom(t *testing.T, l *Log, id string) (*Room, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := NewRoom(id, l, func() time.Time { return t0 })
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return r, cancel
}

// Concurrent writers to two rooms, through the room goroutines and straight
// against the log, still produce 1..n in each room.
func TestSeqGapFreeUnderConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	l := newLog(t, "a", "b", "c")
	ra, _ := startRoom(t, l, "a")
	rb, _ := startRoom(t, l, "b")
	const writers, each = 8, 40
	var wg sync.WaitGroup
	errs := make(chan error, 3*writers*each)
	for range writers {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for range each {
				_, err := ra.Append(ctx, released("x"))
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			for range each {
				_, err := rb.Append(ctx, released("y"))
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			for range each {
				_, err := l.Append(ctx, "c", released("z"), t0)
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		all, err := l.Since(ctx, id, 0)
		if err != nil || len(all) != writers*each || !contiguous(all, 1) {
			t.Errorf("room %s: %d events, contiguous %v, %v", id, len(all), contiguous(all, 1), err)
		}
	}
}

// Subscribers joining at any cursor while writers run see every later
// event exactly once, in order.
func TestSubscribeReplayThenLiveHasNoGap(t *testing.T) {
	ctx := context.Background()
	l := newLog(t, "r")
	r, _ := startRoom(t, l, "r")
	const total = 300
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range total {
			if _, err := r.Append(ctx, released("x")); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := range 10 {
		after := uint64(i * 20)
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := r.Subscribe(ctx, after)
			if err != nil {
				t.Error(err)
				return
			}
			defer s.Close()
			got := append([]*crewbov1.Event(nil), s.Backlog...)
			for len(got) == 0 || got[len(got)-1].GetSeq() < total {
				ev, ok := <-s.Events
				if !ok {
					t.Errorf("subscription from %d closed at %d events", after, len(got))
					return
				}
				got = append(got, ev)
			}
			if !contiguous(got, after+1) || got[len(got)-1].GetSeq() != total {
				t.Errorf("subscriber from %d saw %d events, %d to %d, contiguous %v", after, len(got), got[0].GetSeq(), got[len(got)-1].GetSeq(), contiguous(got, after+1))
			}
		}()
	}
	wg.Wait()
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	ctx := context.Background()
	r, _ := startRoom(t, newLog(t, "r"), "r")
	s, err := r.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range subBuffer + 1 {
		if _, err := r.Append(ctx, released("x")); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for range s.Events {
		n++
	}
	if n != subBuffer {
		t.Errorf("received %d before the drop, want %d", n, subBuffer)
	}
	s.Close() // already dropped: no panic, no block
}

func TestRoomStop(t *testing.T) {
	ctx := context.Background()
	r, cancel := startRoom(t, newLog(t, "r"), "r")
	s, err := r.Subscribe(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, ok := <-s.Events; ok {
		t.Error("subscription open after the room stopped")
	}
	if _, err := r.Append(ctx, released("x")); !errors.Is(err, ErrRoomClosed) {
		t.Errorf("Append after stop: %v", err)
	}
	if _, err := r.Subscribe(ctx, 0); !errors.Is(err, ErrRoomClosed) {
		t.Errorf("Subscribe after stop: %v", err)
	}
	s.Close()
	// Unknown room: the append error reaches the caller.
	r2, _ := startRoom(t, newLog(t), "ghost")
	if _, err := r2.Append(ctx, released("x")); !errors.Is(err, ErrNoRoom) {
		t.Errorf("append to a room missing from the log: %v", err)
	}
}
