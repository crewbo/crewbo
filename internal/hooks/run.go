package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/platform"
)

// MaxPayload caps how much of stdin the hook reads.
const MaxPayload = 1 << 20

// DefaultTimeout bounds the daemon round trip on the hook path. Phase 0
// measured hook spawn at 2 to 3 ms (Linux) and 14 to 23 ms (Windows); the
// local API round trip is about 25 us (#31).
const DefaultTimeout = 300 * time.Millisecond

// warnPrefix starts every fail-open message the agent sees.
const warnPrefix = "Crewbo could not check claims for this action, so it was allowed: "

// Deps are the hook's collaborators.
type Deps struct {
	Paths   platform.Paths
	Timeout time.Duration // DefaultTimeout when zero
	Log     *slog.Logger  // discard when nil
	// ShellTargets returns the files a shell command writes (M2-04). When
	// nil, shell commands are recorded as events but not checked.
	ShellTargets func(command string, shell Shell, cwd string) []string
}

// Run handles one hook call and returns the exit code for the process.
func Run(ctx context.Context, a Adapter, event Kind, in io.Reader, out io.Writer, d Deps) int {
	if d.Timeout == 0 {
		d.Timeout = DefaultTimeout
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	res := decide(ctx, a, event, in, d)
	if res.Warning != "" {
		d.Log.Warn("hook failed open", "agent", a.Name(), "event", event, "reason", res.Warning)
		res.Warning = warnPrefix + res.Warning
	}
	b, code := a.Encode(event, res)
	if len(b) > 0 {
		if _, err := out.Write(b); err != nil {
			d.Log.Warn("write hook response", "err", err)
		}
	}
	return code
}

func decide(ctx context.Context, a Adapter, kind Kind, in io.Reader, d Deps) Result {
	payload, err := io.ReadAll(io.LimitReader(in, MaxPayload+1))
	if err != nil {
		return Result{Warning: "could not read the hook input"}
	}
	if len(payload) > MaxPayload {
		return Result{Warning: "the hook input was larger than 1 MiB"}
	}
	// A UTF-8 byte order mark made the Phase 0 probe parse nothing.
	payload = bytes.TrimPrefix(payload, []byte("\xef\xbb\xbf"))
	ev, err := a.Decode(kind, payload)
	if err != nil {
		return Result{Warning: fmt.Sprintf("could not read the %s hook input (%v)", a.Name(), err)}
	}
	root, ok := RepoRoot(ev.Cwd)
	if !ok {
		return Result{} // not in a git repository: no claims apply
	}

	var paths []string
	targets := ev.Paths
	if ev.Kind == PreShell && d.ShellTargets != nil {
		targets = d.ShellTargets(ev.Command, ev.Shell, ev.Cwd)
	}
	for _, p := range targets {
		if !filepath.IsAbs(p) && !isWindowsAbs(p) {
			p = filepath.Join(ev.Cwd, p)
		}
		if rel, err := claims.Normalize(root, p); err == nil {
			paths = append(paths, rel)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	c, err := daemon.Dial(ctx, d.Paths)
	if err != nil {
		if errors.Is(err, daemon.ErrNotRunning) {
			return Result{Warning: "the Crewbo daemon is not running (start it with `crewbo daemon`)"}
		}
		return Result{Warning: "could not reach the Crewbo daemon"}
	}
	defer func() { _ = c.Close() }()

	req := daemon.Request{Session: ev.Session, Agent: ev.Agent, Repo: root}
	if (ev.Kind == PreEdit || ev.Kind == PreShell) && len(paths) > 0 {
		req.Method, req.Paths = daemon.MethodDecide, paths
	} else {
		req.Method, req.Event = daemon.MethodEvent, string(ev.Kind)
	}
	resp, err := c.Call(ctx, req)
	if err != nil {
		return Result{Warning: "the Crewbo daemon did not answer in time"}
	}
	if req.Method == daemon.MethodDecide && !resp.Allow {
		return Result{Deny: true, Reason: denyReason(resp)}
	}
	return Result{}
}

// denyReason names the file and the holder. It only suggests actions that
// exist today; handoff requests arrive in M5.
func denyReason(resp daemon.Response) string {
	holder := "another agent session"
	until := ""
	if c := resp.BlockedBy; c != nil {
		switch {
		case c.Holder.SessionID == daemon.TerminalSession && c.Holder.MemberID != "":
			holder = c.Holder.MemberID + " (from a terminal)"
		case c.Holder.MemberID != "":
			holder = c.Holder.MemberID + "'s agent"
		case c.Holder.SessionID != "":
			holder = "agent session " + c.Holder.SessionID
		}
		if c.TaskID != "" {
			holder += " for task " + c.TaskID
		}
		if !c.LeaseExpires.IsZero() {
			until = " until at least " + c.LeaseExpires.Local().Format("15:04")
		}
	}
	return fmt.Sprintf("%s is claimed by %s%s (Crewbo). Work on other files, or ask them to release it.", resp.Path, holder, until)
}

// RepoRoot walks up from dir to the nearest directory containing .git (a
// directory, or a file in worktrees and submodules). It does not run git, so
// the hook stays fast.
func RepoRoot(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// isWindowsAbs reports "C:\..." or "C:/..." paths, which filepath.IsAbs does
// not recognise on Unix; agents on Windows send them and tests replay them.
func isWindowsAbs(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}
