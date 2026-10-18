package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
)

const claimUsage = `Usage:
  crewbo claim add [--for 30m] [--task ID] <path or glob>...
  crewbo claim list
  crewbo claim release <id>...
  crewbo claim request [--reason TEXT] <path>
  crewbo claim requests
  crewbo claim handoff [--reason TEXT] <request id> approve|decline
  crewbo claim override --reason TEXT <path>

Paths and globs are relative to the current directory. Claims made here
block every agent session in this repository, including your own, until
they are released or expire (default 30m, at most 8h).

In a room, request asks the member holding a claim on <path> to hand it
over; they answer with handoff, and an approval releases the claim so you
can claim it. requests lists the room's requests and overrides. The room
lead can release anyone's claim with override.
`

// runClaim implements `crewbo claim` against the local daemon (M2-10). The
// sync server (M3) will share these claims with the room.
func runClaim(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return write(stderr, ExitUsage, "%s", claimUsage)
	}
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := daemon.Dial(ctx, p)
	if err != nil {
		return write(stderr, ExitError, "crewbo: the daemon is not running; start it with `crewbo daemon`\n")
	}
	defer func() { _ = c.Close() }()

	switch args[0] {
	case "add":
		return claimAdd(ctx, c, args[1:], root, cwd, stdout, stderr)
	case "list":
		resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimList, Repo: root})
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		if len(resp.Claims) == 0 {
			return write(stdout, ExitOK, "no active claims in this repository\n")
		}
		return printClaims(stdout, resp.Claims)
	case "release":
		if len(args) < 2 {
			return write(stderr, ExitUsage, "%s", claimUsage)
		}
		resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimRelease, Repo: root, IDs: args[1:]})
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		if resp.Note != "" {
			_ = write(stdout, ExitOK, "%s\n", resp.Note)
		}
		return write(stdout, ExitOK, "released %d claim(s)\n", resp.Released)
	case "request", "override":
		return claimRequest(ctx, c, args[0], args[1:], root, cwd, stdout, stderr)
	case "requests":
		resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimRequests, Repo: root})
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		if len(resp.Handoffs) == 0 {
			return write(stdout, ExitOK, "no handoff requests or overrides in this room\n")
		}
		return printHandoffs(stdout, resp.Handoffs)
	case "handoff":
		return claimHandoff(ctx, c, args[1:], root, stdout, stderr)
	default:
		return write(stderr, ExitUsage, "crewbo: unknown claim command %q\n\n%s", args[0], claimUsage)
	}
}

func claimAdd(ctx context.Context, c *daemon.Client, args []string, root, cwd string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claim add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lease := fs.Duration("for", daemon.DefaultCLILease, "how long to hold the claim (at most 8h)")
	task := fs.String("task", "", "task id the claim belongs to")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return ExitUsage
	}
	if len(paths) == 0 || *lease <= 0 {
		return write(stderr, ExitUsage, "%s", claimUsage)
	}
	var patterns []string
	for _, a := range paths {
		pat, err := toPattern(root, cwd, a)
		if err != nil {
			return write(stderr, ExitError, "crewbo: %s: %v\n", a, err)
		}
		patterns = append(patterns, pat)
	}
	resp, err := c.Call(ctx, daemon.Request{
		Method: daemon.MethodClaimAdd, Session: daemon.TerminalSession, Member: username(),
		Repo: root, Patterns: patterns, Task: *task, LeaseSeconds: int64(lease.Seconds()),
	})
	if err != nil {
		if b := resp.BlockedBy; b != nil {
			holder := b.Holder.MemberID
			if holder == "" {
				holder = "agent session " + b.Holder.SessionID
			}
			return write(stderr, ExitError, "crewbo: %s is already claimed by %s until %s; nothing was claimed\n",
				resp.Path, holder, b.LeaseExpires.Local().Format("15:04"))
		}
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	code := printClaims(stdout, resp.Claims)
	if resp.Note != "" {
		_ = write(stdout, ExitOK, "%s\n", resp.Note)
	}
	return code
}

// claimRequest sends a handoff request, or a lead override, for the claim
// on one path.
func claimRequest(ctx context.Context, c *daemon.Client, cmd string, args []string, root, cwd string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claim "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "why you need the file")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return ExitUsage
	}
	if len(paths) != 1 || (cmd == "override" && *reason == "") {
		return write(stderr, ExitUsage, "%s", claimUsage)
	}
	pat, err := toPattern(root, cwd, paths[0])
	if err != nil {
		return write(stderr, ExitError, "crewbo: %s: %v\n", paths[0], err)
	}
	method := daemon.MethodClaimRequest
	if cmd == "override" {
		method = daemon.MethodClaimOverride
	}
	resp, err := c.Call(ctx, daemon.Request{Method: method, Repo: root, Paths: []string{pat}, Text: *reason})
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	h := resp.Handoffs[0]
	if cmd == "override" {
		_ = write(stdout, ExitOK, "Released %s's claim on %s.\n", h.Holder, h.Path)
	} else {
		_ = write(stdout, ExitOK, "Asked %s to hand over %s (request %s). See the answer with `crewbo claim requests`.\n", h.Holder, h.Path, h.RequestID)
	}
	return write(stdout, ExitOK, "%s\n", resp.Note)
}

// claimHandoff answers a handoff request for a claim this member holds.
func claimHandoff(ctx context.Context, c *daemon.Client, args []string, root string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claim handoff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "why, shown to the member who asked")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return ExitUsage
	}
	if len(pos) != 2 || (pos[1] != "approve" && pos[1] != "decline") {
		return write(stderr, ExitUsage, "%s", claimUsage)
	}
	approve := pos[1] == "approve"
	resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodClaimHandoff, Repo: root, IDs: pos[:1], Approve: approve, Text: *reason})
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	if approve {
		_ = write(stdout, ExitOK, "Approved request %s; the claim is released.\n", pos[0])
	} else {
		_ = write(stdout, ExitOK, "Declined request %s; you keep the claim.\n", pos[0])
	}
	return write(stdout, ExitOK, "%s\n", resp.Note)
}

func printHandoffs(w io.Writer, hs []daemon.Handoff) int {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "REQUEST\tPATH\tHELD BY\tBY\tSTATE\tREASON")
	for _, h := range hs {
		id, state, reason := h.RequestID, h.Answer, h.Reason
		switch {
		case h.Kind == "override":
			id, state = "-", "overridden by lead"
		case h.Open:
			state = "open"
		case state == "":
			state = "closed" // the claim was released or expired
		}
		if h.AnswerReason != "" {
			reason += " / " + h.AnswerReason
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", cell(id), cell(h.Path), cell(h.Holder), cell(h.Actor), state, cell(reason))
	}
	if err := tw.Flush(); err != nil {
		return ExitError
	}
	return ExitOK
}

// cell shows text another member wrote on one table row, with every
// control character escaped.
func cell(s string) string {
	return strings.NewReplacer("\n", `\n`, "\t", `\t`).Replace(visible(s))
}

// toPattern turns a command-line path or glob, relative to cwd, into a
// canonical repository-relative pattern.
func toPattern(root, cwd, arg string) (string, error) {
	if !claims.IsGlob(arg) {
		return claims.Normalize(root, filepath.Join(cwd, arg))
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", claims.ErrOutsideRepo
	}
	pat := path.Clean(path.Join(filepath.ToSlash(rel), filepath.ToSlash(arg)))
	if err := claims.ValidatePattern(pat); err != nil {
		return "", errors.New("glob must stay inside the repository")
	}
	return pat, nil
}

func printClaims(w io.Writer, cs []claims.Claim) int {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tPATTERN\tHELD BY\tUNTIL")
	for _, c := range cs {
		holder := c.Holder.MemberID
		if daemon.IsTerminalSession(c.Holder.SessionID) {
			holder += " (terminal)"
		} else if holder == "" {
			holder = "agent " + c.Holder.SessionID
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Pattern, holder, c.LeaseExpires.Local().Format("15:04"))
	}
	if err := tw.Flush(); err != nil {
		return ExitError
	}
	return ExitOK
}

func username() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		// Windows reports DOMAIN\name.
		return u.Username[strings.LastIndex(u.Username, `\`)+1:]
	}
	return "you"
}

// parseInterspersed parses flags placed before, between or after the
// positional arguments (`claim add src/a.go --task 12`). The standard flag
// package stops at the first positional argument, which turned trailing
// flags into claims. Everything after "--" is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if len(args) > 0 && len(rest) < len(args) && args[len(args)-len(rest)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
