package server

import (
	"slices"
	"testing"

	"github.com/coder/websocket"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// nextPresence returns the next Presence on c, skipping everything else.
func nextPresence(t *testing.T, c *websocket.Conn) []string {
	t.Helper()
	for {
		env, err := recvAny(t, c)
		if err != nil {
			t.Fatalf("waiting for presence: %v", err)
		}
		if p := env.GetPresence(); p != nil {
			return p.GetOnlineMemberIds()
		}
	}
}

// M7-01: each connection of a room hears who is connected, when that
// changes; a member with two connections stays online until both close.
func TestPresence(t *testing.T) {
	d := newDispatchRoom(t)
	lead := d.cr.connect(t, testToken, d.leadID)
	if got := nextPresence(t, lead); !slices.Equal(got, []string{"tester"}) {
		t.Fatalf("alone: %v", got)
	}
	mate := d.cr.connect(t, d.cr.tokenB, d.mateID)
	if got := nextPresence(t, mate); len(got) != 2 {
		t.Fatalf("the mate's first presence: %v", got)
	}
	both := nextPresence(t, lead)
	if len(both) != 2 || !slices.Contains(both, "tester") {
		t.Fatalf("after the mate joined: %v", both)
	}
	mate2 := d.cr.connect(t, d.cr.tokenB, d.mateID)
	if got := nextPresence(t, lead); !slices.Equal(got, both) {
		t.Fatalf("a second connection of the mate: %v", got)
	}
	_ = mate.Close(websocket.StatusNormalClosure, "")
	if got := nextPresence(t, lead); !slices.Equal(got, both) {
		t.Fatalf("one of the mate's two connections closed: %v", got)
	}
	_ = mate2.Close(websocket.StatusNormalClosure, "")
	if got := nextPresence(t, lead); !slices.Equal(got, []string{"tester"}) {
		t.Fatalf("after the mate left: %v", got)
	}
}

// M7-01: a member's agent activity reaches the room with the member set
// by the server, and gets no answer.
func TestActivityIsRelayed(t *testing.T) {
	d := newDispatchRoom(t)
	lead := d.cr.connect(t, testToken, d.leadID)
	mate := d.cr.connect(t, d.cr.tokenB, d.mateID)
	sendEnv(t, lead, &crewbov1.Envelope{Id: "a0", Body: &crewbov1.Envelope_Activity{Activity: &crewbov1.Activity{
		RoomId: "r_other", SessionId: "k/s", Agent: "codex",
	}}})
	sendEnv(t, lead, &crewbov1.Envelope{Id: "a1", Body: &crewbov1.Envelope_Activity{Activity: &crewbov1.Activity{
		RoomId: d.cr.room, SessionId: "k/s", Agent: "codex", Path: "src/auth.go", MemberId: "someone-else",
	}}})
	for {
		env, err := recvAny(t, mate)
		if err != nil {
			t.Fatalf("waiting for activity: %v", err)
		}
		if a := env.GetActivity(); a != nil {
			if a.GetRoomId() != d.cr.room || a.GetMemberId() != "tester" || a.GetAgent() != "codex" || a.GetPath() != "src/auth.go" {
				t.Fatalf("relayed %v", a)
			}
			break
		}
	}
	// No answer to either: the next reply the lead gets is to this claim.
	r := request(t, lead, claimReq("c1", d.cr.room, "k/s", []string{"x.go"}, nil))
	if id := r.GetAck().GetRequestId() + r.GetError().GetRequestId(); id != "c1" {
		t.Fatalf("after activity, a reply to %q: %v", id, r)
	}
}
