package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
)

func TestRoomKeys(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t)
	lead, mate, keyless, gone := user(1), user(2), user(3), user(4)
	room, _ := s.rooms.Create(ctx, lead, "keys")
	code, _, _ := s.rooms.Invite(ctx, lead, room.ID, 3, time.Hour)
	for _, u := range []auth.User{mate, keyless, gone} {
		if _, err := s.rooms.Join(ctx, u, code); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.rooms.Leave(ctx, gone, room.ID); err != nil {
		t.Fatal(err)
	}
	for key, uid := range map[string]int64{"ed25519:lead1": 1, "ed25519:lead2": 1, "ed25519:mate": 2, "ed25519:gone": 4} {
		if _, err := s.sessions.db.Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, ?, zeroblob(32), 'd', 'linux', 0)`, key, uid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.sessions.db.Exec(`UPDATE device_keys SET revoked_unix_ms = 1 WHERE key_id = 'ed25519:lead2'`); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	as := func(u auth.User) client {
		tok, _, err := s.sessions.Create(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		return client{t, ts.URL, tok}
	}

	code2, b := as(mate).do("GET", "/v1/rooms/"+room.ID+"/keys", "")
	var got RoomKeys
	if code2 != http.StatusOK || json.Unmarshal(b, &got) != nil {
		t.Fatalf("keys: %d %s", code2, b)
	}
	if got.RoomID != room.ID || got.LeadLogin != "user1" || len(got.Members) != 3 {
		t.Fatalf("keys = %+v", got)
	}
	byLogin := map[string]MemberKeys{}
	for _, m := range got.Members {
		byLogin[m.Login] = m
	}
	if l := byLogin["user1"].Keys; len(l) != 2 || l[0].Revoked == l[1].Revoked || len(l[0].PublicKey) != 32 {
		t.Errorf("lead keys = %+v", l)
	}
	if l := byLogin["user2"].Keys; len(l) != 1 || l[0].KeyID != "ed25519:mate" || l[0].Revoked {
		t.Errorf("member keys = %+v", l)
	}
	if l, ok := byLogin["user3"]; !ok || len(l.Keys) != 0 {
		t.Errorf("member without keys = %+v %v", l, ok)
	}
	if _, ok := byLogin["user4"]; ok {
		t.Error("a member who left is listed")
	}
	if code, _ := as(user(9)).do("GET", "/v1/rooms/"+room.ID+"/keys", ""); code != http.StatusNotFound {
		t.Errorf("stranger: %d, want 404", code)
	}
	if code, _ := (client{t, ts.URL, ""}).do("GET", "/v1/rooms/"+room.ID+"/keys", ""); code != http.StatusUnauthorized {
		t.Errorf("no session: %d", code)
	}
}
