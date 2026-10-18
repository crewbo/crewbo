package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
)

func TestFormatReport(t *testing.T) {
	day := time.Date(2026, 11, 24, 10, 0, 0, 0, time.Local)
	s := formatReport(daemon.Report{
		Version: "v0.1.0", OS: "linux/amd64",
		Agents: []daemon.AgentReport{{Agent: "codex", First: day, Last: day.Add(48 * time.Hour), Sessions: 4}},
		Rooms:  []daemon.RoomReport{{Members: 3, Merged: 2, Conflicts: 1, Handoffs: 5, Approved: 3, Overrides: 1}, {Members: -1}},
		Claims: 2,
		Tasks:  map[string]map[string]int{"own": {"running": 1, "merged": 2}, "sent": {"sent": 3}},
	})
	for _, want := range []string{
		"version: v0.1.0", "os: linux/amd64",
		"codex: 4 session(s), first 2026-11-24, last 2026-11-26",
		"rooms: 2", "room 1: members 3, tasks merged 2, conflict warnings 1, handoff requests 5 (handed off 3), lead overrides 1",
		"room 2: members unknown (server not reachable)",
		"claims active now: 2", "own: merged 2, running 1", "sent: sent 3",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if empty := formatReport(daemon.Report{}); strings.Count(empty, "none") != 2 {
		t.Errorf("empty report:\n%s", empty)
	}
}
