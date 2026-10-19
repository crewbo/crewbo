// Package doctor runs `crewbo doctor` (M6-02): it checks a member's setup
// and says how to fix each problem. Every outside call goes through Deps,
// so each check is tested without a real daemon, server or agent.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks/install"
	"github.com/crewbo/crewbo/internal/server"
)

// Result is one check's outcome.
type Result struct {
	Name   string
	Level  Level
	Detail string // what was found
	Fix    string // what to do; empty when nothing is needed
}

// Level is how a result counts.
type Level int

// Levels: OK passes, Warn deserves a look, Fail breaks Crewbo for this
// member and makes `crewbo doctor` exit non-zero, Skip did not apply.
const (
	OK Level = iota
	Warn
	Fail
	Skip
)

// Agents are the agents doctor checks hooks for, as in `crewbo hook <agent>`.
var Agents = []string{install.ClaudeCode, install.Codex, install.Antigravity}

// Deps is everything the checks read.
type Deps struct {
	// Status asks the running daemon for its status; an error means no
	// daemon answered.
	Status func(ctx context.Context) (daemon.Status, error)
	// Session and Bindings read the saved sign-in and room bindings.
	Session  func() (auth.Session, error)
	Bindings func() ([]auth.Binding, error)
	// ServerInfo fetches GET /v1/info from a server address.
	ServerInfo func(ctx context.Context, base string) (server.Info, error)
	// Repo is the repository root of the current directory, "" outside one.
	Repo string
	// GitConfig reads one git config value for Repo ("" when unset).
	GitConfig func(ctx context.Context, repo, key string) string
	// Binary is the running crewbo binary's path.
	Binary string
	// AgentPresent reports whether an agent seems installed; HookConfig
	// reads its hook config file (nil when absent).
	AgentPresent func(agent string) bool
	HookConfig   func(agent string) []byte
	// LoginShell returns the account's login shell; RunShell runs
	// `<shell> -c true` and returns what it printed on stdout.
	LoginShell func() (string, bool)
	RunShell   func(ctx context.Context, shell string) (string, error)
	Now        func() time.Time
}

// Run runs every check in order.
func Run(ctx context.Context, d Deps) []Result {
	var out []Result
	add := func(r Result) { out = append(out, r) }

	st, derr := d.Status(ctx)
	if derr != nil {
		add(Result{Name: "daemon", Level: Fail, Detail: "not running", Fix: "start it with `crewbo daemon` and keep it running"})
	} else {
		add(Result{Name: "daemon", Level: OK, Detail: fmt.Sprintf("running (version %s, pid %d)", st.Version, st.PID)})
	}

	sess, serr := d.Session()
	switch {
	case errors.Is(serr, auth.ErrNoSession):
		add(Result{Name: "sign-in", Level: Fail, Detail: "not signed in", Fix: "`crewbo login --server <address of your team's sync server>`"})
	case serr != nil:
		add(Result{Name: "sign-in", Level: Fail, Detail: serr.Error(), Fix: "`crewbo login --server <address>` again"})
	case sess.ExpiresUnixMs != 0 && d.Now().UnixMilli() >= sess.ExpiresUnixMs:
		add(Result{Name: "sign-in", Level: Fail, Detail: fmt.Sprintf("the session for %s expired on %s", sess.Server, time.UnixMilli(sess.ExpiresUnixMs).Local().Format("2 Jan 15:04")), Fix: "`crewbo login --server " + sess.Server + "`"})
	default:
		add(Result{Name: "sign-in", Level: OK, Detail: fmt.Sprintf("%s at %s", sess.GitHubLogin, sess.Server)})
	}

	if serr == nil {
		add(serverCheck(ctx, d, sess))
	}
	if derr == nil && serr == nil {
		if strings.HasPrefix(st.Sync, "connected") {
			add(Result{Name: "sync", Level: OK, Detail: st.Sync})
		} else {
			add(Result{Name: "sync", Level: Fail, Detail: st.Sync, Fix: "see the checks above; the daemon retries on its own once the server is reachable"})
		}
	}

	add(roomCheck(d, serr == nil))
	add(gitIdentity(ctx, d))

	if strings.ContainsAny(d.Binary, " &") {
		add(Result{Name: "binary path", Level: Fail, Detail: d.Binary, Fix: "move crewbo to a folder without spaces (Codex and Antigravity run hooks as shell commands), then `crewbo hooks install` again"})
	} else {
		add(Result{Name: "binary path", Level: OK, Detail: d.Binary})
	}

	hooked := false
	for _, a := range Agents {
		hooked = hooked || install.Installed(a, d.HookConfig(a))
	}
	shellAgents := false
	for _, a := range Agents {
		add(hookCheck(d, a, st, derr == nil, hooked))
		if a != install.ClaudeCode && d.AgentPresent(a) {
			shellAgents = true
		}
	}
	add(shellCheck(ctx, d, shellAgents))
	return out
}

func serverCheck(ctx context.Context, d Deps, sess auth.Session) Result {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := d.ServerInfo(ctx, sess.Server)
	switch {
	case err != nil:
		return Result{Name: "server", Level: Fail, Detail: fmt.Sprintf("%s does not answer: %v", sess.Server, err), Fix: "check the address with whoever runs the server; if its tunnel restarted, `crewbo login --server <new address>`"}
	case sess.ServerID != "" && info.ServerID != sess.ServerID:
		return Result{Name: "server", Level: Fail, Detail: "a different sync server answers at " + sess.Server, Fix: "`crewbo login --server <your team's current address>`"}
	case info.ProtocolVersion != server.ProtocolVersion:
		return Result{Name: "server", Level: Fail, Detail: fmt.Sprintf("the server speaks protocol %d, this crewbo %d", info.ProtocolVersion, server.ProtocolVersion), Fix: "use the same crewbo version as the server"}
	}
	return Result{Name: "server", Level: OK, Detail: sess.Server + " answers"}
}

func roomCheck(d Deps, signedIn bool) Result {
	if d.Repo == "" {
		return Result{Name: "room", Level: Skip, Detail: "not inside a git repository; run doctor in your clone to check its room"}
	}
	bs, err := d.Bindings()
	if err != nil {
		return Result{Name: "room", Level: Fail, Detail: err.Error(), Fix: "`crewbo room join <code>` again"}
	}
	if b, ok := auth.FindBinding(bs, d.Repo); ok {
		return Result{Name: "room", Level: OK, Detail: fmt.Sprintf("%s is in room %s", d.Repo, b.Name)}
	}
	if !signedIn {
		return Result{Name: "room", Level: Fail, Detail: d.Repo + " is in no room", Fix: "sign in first, then `crewbo room join <code>` (or `crewbo room create`)"}
	}
	return Result{Name: "room", Level: Fail, Detail: d.Repo + " is in no room", Fix: "`crewbo room join <code from your lead>`, or `crewbo room create` if you are the lead"}
}

func gitIdentity(ctx context.Context, d Deps) Result {
	if d.Repo == "" {
		return Result{Name: "git identity", Level: Skip, Detail: "not inside a git repository"}
	}
	var missing []string
	for _, k := range []string{"user.name", "user.email"} {
		if d.GitConfig(ctx, d.Repo, k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Result{Name: "git identity", Level: Fail, Detail: strings.Join(missing, " and ") + " not set (checkpoints and merges commit as you)", Fix: "`git config --global user.name \"Your Name\"` and `git config --global user.email you@example.com`"}
	}
	return Result{Name: "git identity", Level: OK, Detail: d.GitConfig(ctx, d.Repo, "user.name") + " <" + d.GitConfig(ctx, d.Repo, "user.email") + ">"}
}

// hookCheck reports whether an agent's hooks are installed and whether the
// daemon has heard from them (decision log 2026-10-11: hooks configured but
// never firing are found from the absence of any hook event). Missing hooks
// fail only when no agent has them (hooked false): a member who uses one
// agent with Crewbo may have others installed for other work.
func hookCheck(d Deps, agent string, st daemon.Status, daemonUp, hooked bool) Result {
	name := "hooks: " + agent
	cfg := d.HookConfig(agent)
	switch {
	case !d.AgentPresent(agent) && cfg == nil:
		return Result{Name: name, Level: Skip, Detail: "agent not found"}
	case !install.Installed(agent, cfg) && hooked:
		return Result{Name: name, Level: Warn, Detail: "installed, without Crewbo hooks", Fix: "if you use " + agent + " in this repository: `crewbo hooks install --agent " + agent + "`"}
	case !install.Installed(agent, cfg):
		return Result{Name: name, Level: Fail, Detail: "not installed", Fix: "`crewbo hooks install --agent " + agent + "`"}
	case !daemonUp:
		return Result{Name: name, Level: OK, Detail: "installed"}
	}
	if seen, ok := st.Agents[agent]; ok {
		return Result{Name: name, Level: OK, Detail: "installed; last event " + seen.Local().Format("15:04")}
	}
	fix := "start " + agent + ", send it a prompt, then run `crewbo doctor` again"
	if agent == install.Codex {
		fix = "open Codex, type /hooks and trust the crewbo hooks, send a prompt, then run `crewbo doctor` again"
	}
	return Result{Name: name, Level: Warn, Detail: "installed, but no event since the daemon started at " + st.Started.Local().Format("15:04"), Fix: fix}
}

// shellCheck runs the login shell the way Codex runs hooks: anything it
// prints before the hook's JSON corrupts it (U-25).
func shellCheck(ctx context.Context, d Deps, needed bool) Result {
	if !needed {
		return Result{Name: "login shell", Level: Skip, Detail: "no agent that runs hooks through the shell"}
	}
	sh, ok := d.LoginShell()
	if !ok {
		return Result{Name: "login shell", Level: Skip, Detail: "no login shell on this system"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := d.RunShell(ctx, sh)
	switch {
	case err != nil:
		return Result{Name: "login shell", Level: Warn, Detail: fmt.Sprintf("%s -c true failed: %v", sh, err), Fix: "check that your login shell starts"}
	case strings.TrimSpace(out) != "":
		return Result{Name: "login shell", Level: Fail, Detail: fmt.Sprintf("%s prints %q when it starts, which breaks hook replies", sh, firstLine(out)), Fix: "remove that output from your shell startup files (for zsh: ~/.zshenv), or send it to stderr"}
	}
	return Result{Name: "login shell", Level: OK, Detail: sh + " prints nothing"}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:60] + "..."
	}
	return s
}

// Failed reports whether any result failed.
func Failed(rs []Result) bool {
	for _, r := range rs {
		if r.Level == Fail {
			return true
		}
	}
	return false
}
