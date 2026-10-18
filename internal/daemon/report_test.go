package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func (m *machine) report() Report {
	m.t.Helper()
	resp, err := m.call(Request{Method: MethodReport})
	if err != nil || resp.Report == nil {
		m.t.Fatalf("report: %+v %v", resp, err)
	}
	return *resp.Report
}

func TestReportCountsWithoutPrivateData(t *testing.T) {
	f := newCrossFixture(t)
	a, b := f.a, f.b
	for _, s := range []string{"s1", "s2"} {
		if _, err := b.call(Request{Method: MethodEvent, Session: s, Agent: "codex", Repo: b.repo, Event: "prompt"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.call(Request{Method: MethodEvent, Session: "s3", Agent: "claude-code", Repo: b.repo, Event: "prompt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.call(Request{Method: MethodTaskStart, Repo: b.repo, Text: "Secret feature text", Patterns: []string{"src/secret/**"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.claim("src/private.go"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "B to see A's claim", func() bool { ok, _ := b.agentMay("src/private.go"); return !ok })
	if _, err := b.call(Request{Method: MethodClaimRequest, Repo: b.repo, Paths: []string{"src/private.go"}, Text: "secret reason"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "B to record its request", func() bool { return len(b.handoffs()) == 1 })
	// The lead overrides a claim of its own: an override, not a request.
	if _, err := a.call(Request{Method: MethodClaimOverride, Repo: a.repo, Paths: []string{"src/private.go"}, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "B to record the override", func() bool { return len(b.handoffs()) == 2 })
	// A repeated event from one session is not a new session, and moves
	// the agent's last time.
	if _, err := b.call(Request{Method: MethodEvent, Session: "s1", Agent: "codex", Repo: b.repo, Event: "stop"}); err != nil {
		t.Fatal(err)
	}
	// A task stored for someone else (no task id here) is not this
	// member's.
	if _, err := b.run.d.sync.db.Exec(`INSERT INTO tasks (digest, room_id, signed, state, received_unix_ms) VALUES (x'01', 'r', x'00', 'other', 0)`); err != nil {
		t.Fatal(err)
	}

	r := b.report()
	agents := map[string]AgentReport{}
	for _, ag := range r.Agents {
		agents[ag.Agent] = ag
	}
	// Three codex sessions: s1, s2 and agentMay's "agent-1".
	if agents["codex"].Sessions != 3 || agents["claude-code"].Sessions != 1 || agents["codex"].First.IsZero() || agents["codex"].Last.Before(agents["codex"].First) {
		t.Errorf("agents = %+v", r.Agents)
	}
	if last := b.status().Agents["codex"]; !agents["codex"].Last.Equal(last) {
		t.Errorf("codex last %v, the daemon saw it at %v", agents["codex"].Last, last)
	}
	if len(r.Rooms) != 1 || r.Rooms[0].Members != 2 || r.Rooms[0].Handoffs != 1 || r.Rooms[0].Approved != 0 || r.Rooms[0].Overrides != 1 {
		t.Errorf("rooms = %+v", r.Rooms)
	}
	if r.Claims != 1 || r.Tasks["own"]["running"] != 1 || len(r.Tasks) != 1 || r.OS == "" {
		t.Errorf("claims %d, tasks %v, os %q", r.Claims, r.Tasks, r.OS)
	}
	raw, _ := json.Marshal(r)
	for _, private := range []string{"src/", "Secret feature", "secret reason", "RM1338", "gavriel953", b.repo} {
		if strings.Contains(string(raw), private) {
			t.Errorf("the report holds %q: %s", private, raw)
		}
	}

	// The agents seen outlive a daemon restart; a session seen again after
	// it counts again.
	b.restart()
	if _, err := b.call(Request{Method: MethodEvent, Session: "s1", Agent: "codex", Repo: b.repo, Event: "prompt"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the fourth codex session to be recorded", func() bool {
		for _, ag := range b.report().Agents {
			if ag.Agent == "codex" {
				return ag.Sessions == 4
			}
		}
		return false
	})
}
