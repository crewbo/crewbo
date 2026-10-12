package server

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/crewbo/crewbo/internal/auth"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/store"
)

// claimRoom is a room with two members on two registered devices.
type claimRoom struct {
	s      *Server
	url    string
	room   string
	tokenB string
	keyA   string
	keyB   string
}

func newClaimRoom(t *testing.T) claimRoom {
	t.Helper()
	ctx := context.Background()
	s := newTestServer(t) // testToken is user 1, "tester"
	room, err := s.rooms.Create(ctx, auth.User{ID: 1, Login: "tester"}, "claims")
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := s.rooms.Invite(ctx, auth.User{ID: 1, Login: "tester"}, room.ID, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b := auth.User{ID: 2, Login: "gavriel953"}
	if _, err := s.rooms.Join(ctx, b, code); err != nil {
		t.Fatal(err)
	}
	tokB, _, err := s.sessions.Create(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	for key, uid := range map[string]int64{"ed25519:aaaa": 1, "ed25519:bbbb": 2} {
		if _, err := s.sessions.db.Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, ?, zeroblob(32), 'd', 'linux', 0)`, key, uid); err != nil {
			t.Fatal(err)
		}
	}
	return claimRoom{s: s, url: testServer(t, s), room: room.ID, tokenB: tokB, keyA: "ed25519:aaaa", keyB: "ed25519:bbbb"}
}

func (cr claimRoom) connect(t *testing.T, token, key string) *websocket.Conn {
	t.Helper()
	c := dial(t, cr.url)
	sendEnv(t, c, &crewbov1.Envelope{Id: "h", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{
		ProtocolVersion: 1, SessionToken: token, DeviceKeyId: key,
		Rooms: []*crewbov1.RoomCursor{{RoomId: cr.room}},
	}}})
	env, err := recv(t, c)
	if err != nil || env.GetWelcome() == nil || env.GetWelcome().GetServerTimeUnixMs() == 0 {
		t.Fatalf("welcome: %v %v", env, err)
	}
	return c
}

// request sends env and returns the reply to it, skipping events.
func request(t *testing.T, c *websocket.Conn, env *crewbov1.Envelope) *crewbov1.Envelope {
	t.Helper()
	sendEnv(t, c, env)
	for {
		got, err := recv(t, c)
		if err != nil {
			t.Fatalf("reply to %s: %v", env.GetId(), err)
		}
		if got.GetEvent() == nil {
			return got
		}
	}
}

func claimReq(id, room, session string, patterns, ids []string) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_ClaimRequest{ClaimRequest: &crewbov1.ClaimRequest{
		RoomId: room, SessionId: session, Patterns: patterns, ClaimIds: ids, LeaseSeconds: 3600,
	}}}
}

func nextEvent(t *testing.T, c *websocket.Conn) *crewbov1.Event {
	t.Helper()
	for {
		env, err := recv(t, c)
		if err != nil {
			t.Fatalf("waiting for an event: %v", err)
		}
		if env.GetEvent() != nil {
			return env.GetEvent()
		}
	}
}

func TestClaimsAcrossTwoDevices(t *testing.T) {
	cr := newClaimRoom(t)
	a := cr.connect(t, testToken, cr.keyA)
	b := cr.connect(t, cr.tokenB, cr.keyB)
	sa, sb := cr.keyA+"/terminal", cr.keyB+"/terminal"

	// A claims; both devices see the grant, held by A's GitHub login.
	if r := request(t, a, claimReq("r1", cr.room, sa, []string{"src/a.go"}, []string{"c-a1"})); r.GetAck().GetSeq() != 1 {
		t.Fatalf("grant: %v", r)
	}
	if ev := nextEvent(t, b); ev.GetSeq() != 1 || ev.GetClaimsGranted().GetClaims()[0].GetMemberId() != "tester" || ev.GetActorMemberId() != "tester" {
		t.Fatalf("B saw %v", ev)
	}
	// B conflicts on the same file and on a glob covering it, and is told
	// who holds it.
	for _, p := range []string{"src/a.go", "src/**"} {
		r := request(t, b, claimReq("r2", cr.room, sb, []string{p}, []string{"c-b1"}))
		e := r.GetError()
		if e.GetCode() != crewbov1.ErrorCode_ERROR_CODE_CONFLICT || !strings.Contains(e.GetMessage(), "claimed by tester") || e.GetBlockingClaims()[0].GetId() != "c-a1" {
			t.Errorf("B claims %s: %v", p, r)
		}
	}
	if r := request(t, b, claimReq("r3", cr.room, sb, []string{"src/b.go"}, []string{"c-b2"})); r.GetAck().GetSeq() != 2 {
		t.Errorf("B claims another file: %v", r)
	}
	// A resend of an already granted request is acknowledged, not granted again.
	if r := request(t, a, claimReq("r1", cr.room, sa, []string{"src/a.go"}, []string{"c-a1"})); r.GetAck() == nil || r.GetAck().GetSeq() != 0 {
		t.Errorf("resend: %v", r)
	}
	// Session and room rules.
	for name, tc := range map[string]struct {
		env  *crewbov1.Envelope
		code crewbov1.ErrorCode
	}{
		"session of another device": {claimReq("x1", cr.room, sa, []string{"src/c.go"}, []string{"c-x1"}), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"no claim ids":              {claimReq("x2", cr.room, sb, []string{"src/c.go"}, nil), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"bad pattern":               {claimReq("x3", cr.room, sb, []string{"../etc/passwd"}, []string{"c-x3"}), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"reused claim id":           {claimReq("x4", cr.room, sb, []string{"src/d.go"}, []string{"c-a1"}), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"room not joined":           {claimReq("x5", "r_other", sb, []string{"src/c.go"}, []string{"c-x5"}), crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER},
		"release someone else's": {
			&crewbov1.Envelope{Id: "x6", Body: &crewbov1.Envelope_ClaimRelease{ClaimRelease: &crewbov1.ClaimRelease{RoomId: cr.room, ClaimIds: []string{"c-a1"}}}},
			crewbov1.ErrorCode_ERROR_CODE_INVALID,
		},
	} {
		if r := request(t, b, tc.env); r.GetError().GetCode() != tc.code {
			t.Errorf("%s: %v, want %v", name, r, tc.code)
		}
	}
	// A renews (its lease is an hour, so a 5-minute renewal changes
	// nothing and records nothing), then releases; B sees the release.
	renew := &crewbov1.Envelope{Id: "r4", Body: &crewbov1.Envelope_ClaimRenew{ClaimRenew: &crewbov1.ClaimRenew{RoomId: cr.room, SessionId: sa}}}
	if r := request(t, a, renew); r.GetAck() == nil || r.GetAck().GetSeq() != 0 {
		t.Errorf("renew without effect: %v", r)
	}
	release := &crewbov1.Envelope{Id: "r5", Body: &crewbov1.Envelope_ClaimRelease{ClaimRelease: &crewbov1.ClaimRelease{RoomId: cr.room, ClaimIds: []string{"c-a1", "unknown"}}}}
	if r := request(t, a, release); r.GetAck().GetSeq() != 3 {
		t.Errorf("release: %v", r)
	}
	for nextEvent(t, b).GetSeq() != 3 { // skip to the release
		continue
	}
	if r := request(t, b, claimReq("r6", cr.room, sb, []string{"src/a.go"}, []string{"c-b3"})); r.GetAck().GetSeq() != 4 {
		t.Errorf("B claims the released file: %v", r)
	}
}

func TestHelloWithSomeoneElsesDeviceKey(t *testing.T) {
	cr := newClaimRoom(t)
	c := dial(t, cr.url)
	sendEnv(t, c, &crewbov1.Envelope{Id: "h", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{ProtocolVersion: 1, SessionToken: testToken, DeviceKeyId: cr.keyB}}})
	if env, err := recv(t, c); env.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
		t.Fatalf("want UNAUTHENTICATED, got %v %v", env, err)
	}
}

// roomWithClock runs a Room directly with a clock the test moves.
func roomWithClock(t *testing.T, path string) (*Room, *Log, func(time.Duration), func()) {
	t.Helper()
	db, err := store.Open(context.Background(), path, store.Server)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	log := NewLog(db.SQL())
	r := NewRoom("r", log, clock)
	r.expireEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	stop := func() { cancel(); <-done; _ = db.Close() }
	return r, log, advance, stop
}

func TestLeasesExpireAndClaimsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "server.db")
	db, err := store.Open(ctx, path, store.Server)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewLog(db.SQL()).CreateRoom(ctx, "r", "r", time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	a := Caller{Member: "tester", DeviceKey: "ed25519:aaaa"}
	short := claimReq("q1", "r", "ed25519:aaaa/agent", []string{"x.go"}, []string{"c-x"})
	short.GetClaimRequest().LeaseSeconds = 60
	long := claimReq("q2", "r", "ed25519:aaaa/terminal", []string{"y.go"}, []string{"c-y"})

	r, log, advance, stop := roomWithClock(t, path)
	for _, env := range []*crewbov1.Envelope{short, long} {
		if reply := r.Handle(ctx, a, env); reply.GetAck() == nil {
			t.Fatalf("grant: %v", reply)
		}
	}
	advance(61 * time.Second) // the 60 s lease lapses; the hour lease does not
	deadline := time.Now().Add(5 * time.Second)
	for {
		evs, _ := log.Since(ctx, "r", 2)
		if len(evs) == 1 && evs[0].GetClaimsExpired().GetClaimIds()[0] == "c-x" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no expiry event: %v", evs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()

	// A new server process rebuilds the claims from the log: y.go is still
	// held, x.go is free.
	r, _, _, stop = roomWithClock(t, path)
	defer stop()
	b := Caller{Member: "gavriel953", DeviceKey: "ed25519:bbbb"}
	if reply := r.Handle(ctx, b, claimReq("q3", "r", "ed25519:bbbb/terminal", []string{"y.go"}, []string{"c-b"})); reply.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_CONFLICT {
		t.Errorf("y.go after restart: %v", reply)
	}
	if reply := r.Handle(ctx, b, claimReq("q4", "r", "ed25519:bbbb/terminal", []string{"x.go"}, []string{"c-b2"})); reply.GetAck() == nil {
		t.Errorf("x.go after its lease lapsed: %v", reply)
	}
}
