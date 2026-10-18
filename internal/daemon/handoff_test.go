package daemon

import (
	"strings"
	"testing"
)

func (m *machine) handoffs() []Handoff {
	m.t.Helper()
	resp, err := m.call(Request{Method: MethodClaimRequests, Repo: m.repo})
	if err != nil {
		m.t.Fatal(err)
	}
	return resp.Handoffs
}

// latest returns the newest ladder step this machine has recorded.
func (m *machine) latest() Handoff {
	if hs := m.handoffs(); len(hs) > 0 {
		return hs[0]
	}
	return Handoff{}
}

// The whole escalation ladder across two machines (M5-03): a request is
// declined, asked again and approved, which frees the file; then the lead
// overrides a claim a teammate keeps.
func TestEscalationLadder(t *testing.T) {
	f := newCrossFixture(t)
	a, b := f.a, f.b // a is the room lead

	held, err := b.claim("src/a.go")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "A to see B's claim", func() bool { ok, _ := a.agentMay("src/a.go"); return !ok })
	if _, err := b.call(Request{Method: MethodClaimRequest, Repo: b.repo, Paths: []string{"src/a.go"}}); err == nil || !strings.Contains(err.Error(), "nobody else holds") {
		t.Errorf("B asks for their own claim: %v", err)
	}

	// A asks; B sees the open request with the path and reason, and declines.
	ask := func() Handoff {
		t.Helper()
		resp, err := a.call(Request{Method: MethodClaimRequest, Repo: a.repo, Paths: []string{"src/a.go"}, Text: "need the login fix"})
		if err != nil || len(resp.Handoffs) != 1 || !strings.Contains(resp.Note, "Shared with room crewbo") {
			t.Fatalf("A asks: %+v %v", resp, err)
		}
		h := resp.Handoffs[0]
		if h.Holder != "gavriel953" || h.Path != "src/a.go" || h.ClaimID != held.Claims[0].ID || h.RequestID == "" {
			t.Fatalf("A's request: %+v", h)
		}
		eventually(t, "B to list the request", func() bool { return b.latest().RequestID == h.RequestID })
		return h
	}
	first := ask()
	if got := b.latest(); !got.Open || got.Actor != "RM1338" || got.Holder != "gavriel953" || got.Path != "src/a.go" || got.Reason != "need the login fix" {
		t.Errorf("B lists %+v", got)
	}
	if _, err := a.call(Request{Method: MethodClaimHandoff, Repo: a.repo, IDs: []string{first.RequestID}, Approve: true}); err == nil || !strings.Contains(err.Error(), "only gavriel953") {
		t.Errorf("A answers their own request: %v", err)
	}
	if _, err := b.call(Request{Method: MethodClaimHandoff, Repo: b.repo, IDs: []string{first.RequestID}, Text: "halfway through"}); err != nil {
		t.Fatalf("B declines: %v", err)
	}
	eventually(t, "A to see the decline", func() bool {
		h := a.latest()
		return h.Answer == "declined" && h.AnswerReason == "halfway through" && !h.Open
	})
	if ok, _ := a.agentMay("src/a.go"); ok {
		t.Error("a declined request released the claim")
	}

	// Asked again and approved: the claim goes and A can take the file.
	second := ask()
	if second.RequestID == first.RequestID {
		t.Error("a second request reuses the first one's id")
	}
	if _, err := b.call(Request{Method: MethodClaimHandoff, Repo: b.repo, IDs: []string{second.RequestID}, Approve: true}); err != nil {
		t.Fatalf("B approves: %v", err)
	}
	eventually(t, "A to see the claim released", func() bool { ok, _ := a.agentMay("src/a.go"); return ok })
	if h := a.latest(); h.Answer != "approved" || h.Open {
		t.Errorf("A lists %+v", h)
	}
	if resp, err := a.claim("src/a.go"); err != nil || resp.Claims[0].Holder.MemberID != "RM1338" {
		t.Fatalf("A claims after the handoff: %+v %v", resp, err)
	}
	if _, err := b.call(Request{Method: MethodClaimHandoff, Repo: b.repo, IDs: []string{second.RequestID}, Approve: true}); err == nil {
		t.Error("an answered request was answered again")
	}

	// A teammate cannot override; the lead can, and must give a reason.
	kept, err := b.claim("src/b.go")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "A to see B's second claim", func() bool { ok, _ := a.agentMay("src/b.go"); return !ok })
	if _, err := b.call(Request{Method: MethodClaimOverride, Repo: b.repo, Paths: []string{"src/b.go"}, Text: "mine"}); err == nil || !strings.Contains(err.Error(), "only the room lead") {
		t.Errorf("a teammate overrides: %v", err)
	}
	if _, err := a.call(Request{Method: MethodClaimOverride, Repo: a.repo, Paths: []string{"src/b.go"}}); err == nil || !strings.Contains(err.Error(), "needs a reason") {
		t.Errorf("an override without a reason: %v", err)
	}
	pending, err := a.call(Request{Method: MethodClaimRequest, Repo: a.repo, Paths: []string{"src/b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := a.call(Request{Method: MethodClaimOverride, Repo: a.repo, Paths: []string{"src/b.go"}, Text: "demo in ten minutes"})
	if err != nil || resp.Handoffs[0].ClaimID != kept.Claims[0].ID || resp.Handoffs[0].Holder != "gavriel953" {
		t.Fatalf("the lead overrides: %+v %v", resp, err)
	}
	eventually(t, "B to lose the claim and list the override", func() bool {
		h := b.latest()
		return h.Kind == "override" && h.Path == "src/b.go" && h.Holder == "gavriel953" && h.Actor == "RM1338" && h.Reason == "demo in ten minutes"
	})
	// The open request closed with its claim, unanswered.
	if got := b.handoffs(); got[1].RequestID != pending.Handoffs[0].RequestID || got[1].Open || got[1].Answer != "" {
		t.Errorf("the request on an overridden claim: %+v", got[1])
	}
	if _, err := a.call(Request{Method: MethodClaimOverride, Repo: a.repo, Paths: []string{"src/none.go"}, Text: "x"}); err == nil || !strings.Contains(err.Error(), "nobody holds") {
		t.Errorf("an override of a free path: %v", err)
	}

	// A restart replays the room without recording any step twice.
	want := len(b.handoffs())
	if want != 4 {
		t.Errorf("B lists %d steps, want 4", want)
	}
	b.run.cancel()
	<-b.run.done
	b.start()
	eventually(t, "B to replay the room up to A's claim", func() bool { _, c := b.agentMay("src/a.go"); return c.Holder.MemberID == "RM1338" })
	if got := b.handoffs(); len(got) != want || got[0].Kind != "override" || got[2].Answer != "approved" || got[3].Answer != "declined" {
		t.Errorf("after a restart B lists %+v", got)
	}
}

func TestHandoffNeedsARoom(t *testing.T) {
	f := newCrossFixture(t)
	for _, m := range []string{MethodClaimRequest, MethodClaimRequests, MethodClaimHandoff, MethodClaimOverride} {
		if _, err := f.a.call(Request{Method: m, Repo: t.TempDir(), Paths: []string{"a"}, IDs: []string{"r"}}); err == nil || !strings.Contains(err.Error(), "not in a room") {
			t.Errorf("%s outside a room: %v", m, err)
		}
	}
}
