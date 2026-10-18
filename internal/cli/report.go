package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
)

// runReport prints the opt-in dogfood report (M6-03): counts and dates the
// member pastes into the survey themselves. Nothing is sent.
func runReport(ctx context.Context, stdout, stderr io.Writer) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, p)
	if err != nil {
		return write(stderr, ExitError, "crewbo: the daemon is not running; start it with `crewbo daemon`\n")
	}
	defer func() { _ = c.Close() }()
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodReport})
	if err != nil || resp.Report == nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return write(stdout, ExitOK, "%s", formatReport(*resp.Report))
}

// formatReport renders a report as plain text to paste into a form.
func formatReport(r daemon.Report) string {
	var b strings.Builder
	day := func(t time.Time) string { return t.Local().Format("2006-01-02") }
	fmt.Fprintf(&b, "Crewbo report (counts only: no file paths, task text or names; nothing was sent)\n")
	fmt.Fprintf(&b, "version: %s\nos: %s\n", r.Version, r.OS)
	fmt.Fprintf(&b, "agents seen on this computer:\n")
	if len(r.Agents) == 0 {
		fmt.Fprintf(&b, "  none\n")
	}
	for _, a := range r.Agents {
		fmt.Fprintf(&b, "  %s: %d session(s), first %s, last %s\n", a.Agent, a.Sessions, day(a.First), day(a.Last))
	}
	fmt.Fprintf(&b, "rooms: %d\n", len(r.Rooms))
	for i, rr := range r.Rooms {
		members := fmt.Sprint(rr.Members)
		if rr.Members < 0 {
			members = "unknown (server not reachable)"
		}
		fmt.Fprintf(&b, "  room %d: members %s, tasks merged %d, conflict warnings %d, handoff requests %d (handed off %d), lead overrides %d\n",
			i+1, members, rr.Merged, rr.Conflicts, rr.Handoffs, rr.Approved, rr.Overrides)
	}
	fmt.Fprintf(&b, "claims active now: %d\n", r.Claims)
	fmt.Fprintf(&b, "your tasks:\n")
	if len(r.Tasks) == 0 {
		fmt.Fprintf(&b, "  none\n")
	}
	for _, kind := range []string{"own", "received", "sent"} {
		states := r.Tasks[kind]
		if len(states) == 0 {
			continue
		}
		names := make([]string, 0, len(states))
		for s := range states {
			names = append(names, s)
		}
		sort.Strings(names)
		parts := make([]string, len(names))
		for i, s := range names {
			parts[i] = fmt.Sprintf("%s %d", s, states[s])
		}
		fmt.Fprintf(&b, "  %s: %s\n", kind, strings.Join(parts, ", "))
	}
	return b.String()
}
