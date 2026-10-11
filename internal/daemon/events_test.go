package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
)

// clock is a settable time source shared with the daemon's owner goroutine.
type clock struct{ ns atomic.Int64 }

func newClock(t time.Time) *clock { c := &clock{}; c.ns.Store(t.UnixNano()); return c }
func (c *clock) Now() time.Time   { return time.Unix(0, c.ns.Load()) }
func (c *clock) set(t time.Time)  { c.ns.Store(t.UnixNano()) }

func TestHookEventsRenewLeases(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	clk := newClock(t0)
	set := claims.NewSet()
	if err := set.Add(claims.Claim{ID: "c1", Pattern: "src/auth.go", Holder: claims.Holder{SessionID: "alice"}, LeaseExpires: t0.Add(claims.DefaultLease)}, t0); err != nil {
		t.Fatal(err)
	}
	p := testPaths(t)
	start(t, Config{Paths: p, Claims: set, Now: clk.Now})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()
	decide := func(session string) bool {
		t.Helper()
		resp, err := c.Call(ctx, Request{Method: MethodDecide, Session: session, Agent: "codex", Paths: []string{"src/auth.go"}})
		if err != nil {
			t.Fatal(err)
		}
		return resp.Allow
	}

	// Alice's agent is active at minute 4: her lease moves to minute 9.
	clk.set(t0.Add(4 * time.Minute))
	if _, err := c.Call(ctx, Request{Method: MethodEvent, Session: "alice", Agent: "claude-code", Repo: "/r", Event: "prompt"}); err != nil {
		t.Fatal(err)
	}
	clk.set(t0.Add(7 * time.Minute)) // past the original expiry
	if decide("bob") {
		t.Fatal("bob allowed at minute 7; alice's renewed lease should still hold")
	}
	// Alice goes quiet: at minute 15 her lease has run out.
	clk.set(t0.Add(15 * time.Minute))
	if !decide("bob") {
		t.Fatal("bob denied at minute 15; alice's lease should have expired")
	}

	resp, err := c.Call(ctx, Request{Method: MethodStatus})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Sessions != 2 { // alice by event, bob by decide
		t.Errorf("sessions = %d, want 2", resp.Status.Sessions)
	}
}

func TestEventValidation(t *testing.T) {
	p := testPaths(t)
	start(t, Config{Paths: p})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	for _, req := range []Request{
		{Method: MethodEvent, Event: "prompt"},
		{Method: MethodEvent, Session: "s1"},
	} {
		if _, err := c.Call(context.Background(), req); err == nil {
			t.Errorf("event %+v accepted, want an error", req)
		}
	}
}
