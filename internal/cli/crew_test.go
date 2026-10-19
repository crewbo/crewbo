package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
)

func TestFormatCrew(t *testing.T) {
	at := time.Date(2026, 10, 20, 21, 40, 0, 0, time.Local)
	got := formatCrew([]daemon.CrewMember{
		{Login: "RM1338", You: true, Lead: true, Online: true, Agents: []daemon.CrewAgent{{Agent: "claude-code", Path: "src/auth.go"}}, Claims: []string{"src/auth/**"}},
		{Login: "gavriel953", Online: true, Agents: []daemon.CrewAgent{{Agent: "codex", Idle: true}}},
		{Login: "riya", Online: true},
		{Login: "dev", OfflineSince: at},
		{Login: "maya"},
	})
	for _, want := range []string{
		"RM1338 (you)  Writing src/auth.go  Claude Code  src/auth/**",
		"gavriel953    Idle                 Codex",
		"riya          Online",
		"dev           Offline since 21:40",
		"maya          Offline",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}
