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

func handoffReq(id, room, rid, claim, reason string) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_HandoffRequest{HandoffRequest: &crewbov1.HandoffRequest{RoomId: room, RequestId: rid, ClaimId: claim, Reason: reason}}}
}

func handoffAns(id, room, rid string, approve bool) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_HandoffAnswer{HandoffAnswer: &crewbov1.HandoffAnswer{RoomId: room, RequestId: rid, Approve: approve}}}
}

func override(id, room, claim, reason string) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_LeadOverride{LeadOverride: &crewbov1.LeadOverride{RoomId: room, ClaimId: claim, Reason: reason}}}
}

// M5-03: the escalation ladder. A member asks the holder for a claim; the
// holder declines or approves (which releases it); the lead can release a
// claim with a reason. Every step is a room event.
func TestHandoffLadder(t *testing.T) {
	cr := newClaimRoom(t)
	lead := cr.connect(t, testToken, cr.keyA)
	mate := cr.connect(t, cr.tokenB, cr.keyB)
	invalid := crewbov1.ErrorCode_ERROR_CODE_INVALID
	// last returns the room's latest n events.
	last := func(n int) []*crewbov1.Event {
		t.Helper()
		evs, err := cr.s.events.Since(context.Background(), cr.room, 0)
		if err != nil || len(evs) < n {
			t.Fatalf("log: %d events, %v", len(evs), err)
		}
		return evs[len(evs)-n:]
	}
	ok := func(c *websocket.Conn, env *crewbov1.Envelope) {
		t.Helper()
		if r := request(t, c, env); r.GetAck() == nil {
			t.Fatalf("%s: %v", env.GetId(), r)
		}
	}
	ok(lead, claimReq("c", cr.room, cr.keyA+"/terminal", []string{"src/a.go"}, []string{"c1"}))

	for name, tc := range map[string]struct {
		conn *websocket.Conn
		env  *crewbov1.Envelope
	}{
		"your own claim": {lead, handoffReq("x1", cr.room, "r0", "c1", "")},
		"no such claim":  {mate, handoffReq("x2", cr.room, "r0", "nope", "")},
		"no request id":  {mate, handoffReq("x3", cr.room, "", "c1", "")},
		"answer nothing": {lead, handoffAns("x4", cr.room, "nope", true)},
	} {
		if r := request(t, tc.conn, tc.env); r.GetError().GetCode() != invalid {
			t.Errorf("%s: %v", name, r)
		}
	}

	ok(mate, handoffReq("h1", cr.room, "r1", "c1", "I need the parser"))
	if ev := last(1)[0]; ev.GetHandoffRequested().GetReason() != "I need the parser" || ev.GetActorMemberId() != "gavriel953" {
		t.Errorf("logged %v", ev)
	}
	if r := request(t, mate, handoffReq("h1b", cr.room, "r1", "c1", "I need the parser")); r.GetAck() == nil || r.GetAck().GetSeq() != 0 {
		t.Errorf("a resent request: %v", r)
	}
	if r := request(t, mate, handoffAns("a0", cr.room, "r1", true)); r.GetError().GetCode() != invalid || !strings.Contains(r.GetError().GetMessage(), "only tester") {
		t.Errorf("the requester answering: %v", r)
	}
	ok(lead, handoffAns("a1", cr.room, "r1", false))
	if ev := last(1)[0]; ev.GetHandoffAnswered() == nil || ev.GetHandoffAnswered().GetApprove() || ev.GetActorMemberId() != "tester" {
		t.Errorf("decline logged as %v", ev)
	}
	if r := request(t, lead, handoffAns("a1b", cr.room, "r1", true)); r.GetError().GetCode() != invalid {
		t.Errorf("answering a closed request: %v", r)
	}

	// Asked again and approved: the answer, then the release.
	ok(mate, handoffReq("h2", cr.room, "r2", "c1", ""))
	ok(lead, handoffAns("a2", cr.room, "r2", true))
	evs := last(2)
	if !evs[0].GetHandoffAnswered().GetApprove() || evs[1].GetClaimsReleased().GetClaimIds()[0] != "c1" {
		t.Errorf("approval logged as %v", evs)
	}

	// The lead overrides the teammate's claim, with a reason.
	ok(mate, claimReq("c2", cr.room, cr.keyB+"/terminal", []string{"src/b.go"}, []string{"c2"}))
	if r := request(t, mate, override("o0", cr.room, "c2", "mine")); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_NOT_LEAD {
		t.Errorf("a member overriding: %v", r)
	}
	if r := request(t, lead, override("o1", cr.room, "c2", "")); r.GetError().GetCode() != invalid {
		t.Errorf("an override without a reason: %v", r)
	}
	ok(lead, override("o2", cr.room, "c2", "they are offline and the demo is in an hour"))
	evs = last(2)
	if evs[0].GetClaimOverridden().GetReason() != "they are offline and the demo is in an hour" || evs[0].GetActorMemberId() != "tester" ||
		evs[1].GetClaimsReleased().GetClaimIds()[0] != "c2" {
		t.Errorf("override logged as %v", evs)
	}
	// Live members hear the steps too.
	ev := nextEvent(t, mate)
	for ev.GetClaimOverridden() == nil {
		ev = nextEvent(t, mate)
	}
}

// Open handoff requests are part of the room's projection: a restarted
// room still knows them.
func TestOpenHandoffsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	ctx := context.Background()
	db, err := store.Open(ctx, path, store.Server)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewLog(db.SQL()).CreateRoom(ctx, "r", "r", time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	r, _, _, stop := roomWithClock(t, path)
	holder, asker := Caller{Member: "a", DeviceKey: "ed25519:a"}, Caller{Member: "b", DeviceKey: "ed25519:b"}
	if rep := r.Handle(ctx, holder, claimReq("c", "r", "ed25519:a/t", []string{"x.go"}, []string{"c1"})); rep.GetAck() == nil {
		t.Fatal(rep)
	}
	if rep := r.Handle(ctx, asker, handoffReq("h", "r", "r1", "c1", "")); rep.GetAck() == nil {
		t.Fatal(rep)
	}
	stop()
	r, _, _, stop = roomWithClock(t, path)
	defer stop()
	if rep := r.Handle(ctx, holder, handoffAns("a", "r", "r1", true)); rep.GetAck() == nil {
		t.Fatalf("answer after a restart: %v", rep)
	}
	if rep := r.Handle(ctx, asker, handoffReq("h2", "r", "r2", "c1", "")); rep.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_INVALID {
		t.Errorf("the claim was not released: %v", rep)
	}
}
