package daemon

import (
	"slices"
	"testing"
)

// seen returns what m's daemon knows of a session's agent in any room.
func (m *machine) seen(session string) (agentSeen, bool) {
	c := m.run.d.sync.crew
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, room := range c.agents {
		for id, a := range room {
			if len(id) > len(session) && id[len(id)-len(session)-1:] == "/"+session {
				return a, true
			}
		}
	}
	return agentSeen{}, false
}

func (m *machine) online() []string {
	c := m.run.d.sync.crew
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, members := range c.online {
		return members
	}
	return nil
}

// M7-01: the lead's daemon learns who is connected and what the
// teammate's agent is doing, from the teammate's hook events.
func TestCrewPresenceAndActivity(t *testing.T) {
	f := newCrossFixture(t)
	eventually(t, "both members online", func() bool {
		return slices.Equal(f.a.online(), []string{"RM1338", "gavriel953"})
	})
	if _, err := f.b.call(Request{Method: MethodDecide, Session: "s1", Agent: "codex", Repo: f.b.repo, Paths: []string{"src/parse.go"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the edit", func() bool {
		a, ok := f.a.seen("s1")
		return ok && a.Member == "gavriel953" && a.Agent == "codex" && a.Path == "src/parse.go" && !a.Idle
	})
	if _, err := f.b.call(Request{Method: MethodEvent, Session: "s1", Agent: "codex", Repo: f.b.repo, Event: "stop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "idle after the reply", func() bool { a, _ := f.a.seen("s1"); return a.Idle && a.Path == "" })
	// A terminal session shares nothing.
	if _, err := f.b.call(Request{Method: MethodDecide, Session: "term", Repo: f.b.repo, Paths: []string{"x.go"}}); err != nil {
		t.Fatal(err)
	}
	// Activity sent after it arrives after it, so once this is seen the
	// terminal session would have been too.
	if _, err := f.b.call(Request{Method: MethodDecide, Session: "s1", Agent: "codex", Repo: f.b.repo, Paths: []string{"src/lex.go"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the next edit", func() bool { a, _ := f.a.seen("s1"); return a.Path == "src/lex.go" })
	if _, ok := f.a.seen("term"); ok {
		t.Error("a session without an agent was shared")
	}
}
