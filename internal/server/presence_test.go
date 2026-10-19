package server

import (
	"slices"
	"testing"

	"github.com/coder/websocket"
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
