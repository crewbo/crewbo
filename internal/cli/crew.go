package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
)

// runCrew prints the room of the current repository (M7-02): each member,
// what their agents are doing, and the files they hold.
func runCrew(ctx context.Context, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	root, ok := hooks.RepoRoot(cwd)
	if !ok {
		return write(stderr, ExitError, "crewbo: not inside a git repository\n")
	}
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, p)
	if err != nil {
		return write(stderr, ExitError, "crewbo: the daemon is not running; start it with `crewbo daemon`\n")
	}
	defer func() { _ = c.Close() }()
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodCrew, Repo: root})
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return write(stdout, ExitOK, "%s", formatCrew(resp.Crew))
}

// formatCrew renders the crew as a table: member, status, agent, claims.
func formatCrew(crew []daemon.CrewMember) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "MEMBER\tSTATUS\tAGENT\tCLAIMS")
	for _, m := range crew {
		name := m.Login
		switch {
		case m.You:
			name += " (you)"
		case m.Lead:
			name += " (lead)"
		}
		status, agent := "Online", ""
		switch {
		case !m.Online && m.OfflineSince.IsZero():
			status = "Offline"
		case !m.Online:
			status = "Offline since " + m.OfflineSince.Local().Format("15:04")
		case len(m.Agents) > 0:
			a := m.Agents[0]
			agent = agentName(a.Agent)
			if agent == "" {
				agent = a.Agent
			}
			switch {
			case a.Idle:
				status = "Idle"
			case a.Path != "":
				status = "Writing " + a.Path
			default:
				status = "Working"
			}
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, visible(status), agent, visible(strings.Join(m.Claims, ", ")))
	}
	_ = w.Flush()
	return b.String()
}
