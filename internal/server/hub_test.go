package server

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// helloRooms opens a connection as the test session with room cursors and
// returns it after the Welcome.
func helloRooms(t *testing.T, url string, cursors map[string]uint64) *websocket.Conn {
	t.Helper()
	c := dial(t, url)
	h := &crewbov1.Hello{ProtocolVersion: 1, SessionToken: testToken}
	for id, seq := range cursors {
		h.Rooms = append(h.Rooms, &crewbov1.RoomCursor{RoomId: id, LastSeq: seq})
	}
	sendEnv(t, c, &crewbov1.Envelope{Id: "h", Body: &crewbov1.Envelope_Hello{Hello: h}})
	if env, err := recv(t, c); err != nil || env.GetWelcome() == nil {
		t.Fatalf("want Welcome, got %v %v", env, err)
	}
	return c
}

// nextSeqs reads n events and returns their sequence numbers.
func nextSeqs(t *testing.T, c *websocket.Conn, n int) []uint64 {
	t.Helper()
	var out []uint64
	for range n {
		env, err := recv(t, c)
		if err != nil || env.GetEvent() == nil {
			t.Fatalf("want an Event, got %v %v", env, err)
		}
		out = append(out, env.GetEvent().GetSeq())
	}
	return out
}

func equal(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHubReplaysThenStreams(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t)
	room, err := s.rooms.Create(ctx, user(1), "hub") // user 1 is the test session
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.room(room.ID).Append(ctx, released("c")); err != nil {
			t.Fatal(err)
		}
	}
	url := testServer(t, s)
	all := helloRooms(t, url, map[string]uint64{room.ID: 0})
	mid := helloRooms(t, url, map[string]uint64{room.ID: 2})
	if got := nextSeqs(t, all, 3); !equal(got, []uint64{1, 2, 3}) {
		t.Errorf("replay from 0 = %v", got)
	}
	if got := nextSeqs(t, mid, 1); !equal(got, []uint64{3}) {
		t.Errorf("replay from 2 = %v", got)
	}
	for range 2 {
		if _, err := s.room(room.ID).Append(ctx, released("d")); err != nil {
			t.Fatal(err)
		}
	}
	for name, c := range map[string]*websocket.Conn{"from 0": all, "from 2": mid} {
		if got := nextSeqs(t, c, 2); !equal(got, []uint64{4, 5}) {
			t.Errorf("%s: live = %v", name, got)
		}
	}
	// Unknown request types answer UNSUPPORTED on a connection with rooms.
	sendEnv(t, all, &crewbov1.Envelope{Id: "r", Body: &crewbov1.Envelope_Ack{Ack: &crewbov1.Ack{}}})
	if env, err := recv(t, all); err != nil || env.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_UNSUPPORTED {
		t.Errorf("request: %v %v", env, err)
	}
}

func TestHubRefusesRoomsYouAreNotIn(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t)
	theirs, _ := s.rooms.Create(ctx, user(2), "not yours")
	c := dial(t, testServer(t, s))
	sendEnv(t, c, &crewbov1.Envelope{Id: "h", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{
		ProtocolVersion: 1, SessionToken: testToken,
		Rooms: []*crewbov1.RoomCursor{{RoomId: theirs.ID}},
	}}})
	env, err := recv(t, c)
	if env.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER {
		t.Fatalf("want NOT_MEMBER, got %v %v", env, err)
	}
	if _, err := recv(t, c); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Errorf("close: %v", err)
	}
	s.hubMu.Lock()
	started := len(s.live)
	s.hubMu.Unlock()
	if started != 0 {
		t.Errorf("a room goroutine started for a refused room: %d", started)
	}
}

// When the server stops its rooms, a connected daemon is told to come back
// later rather than silently missing events.
func TestHubClosesWhenTheRoomStops(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t)
	room, _ := s.rooms.Create(ctx, user(1), "stopping")
	c := helloRooms(t, testServer(t, s), map[string]uint64{room.ID: 0})
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	if _, err := recv(t, c); websocket.CloseStatus(err) != websocket.StatusTryAgainLater {
		t.Errorf("close = %v, want %v", err, websocket.StatusTryAgainLater)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
}
