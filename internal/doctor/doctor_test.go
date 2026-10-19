package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks/install"
	"github.com/crewbo/crewbo/internal/server"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// healthy is a member whose setup passes every check.
func healthy() Deps {
	claude, _, _ := install.Install(install.ClaudeCode, nil, "/opt/crewbo")
	codex, _, _ := install.Install(install.Codex, nil, "/opt/crewbo")
	return Deps{
		Status: func(context.Context) (daemon.Status, error) {
			return daemon.Status{
				Version: "v0.1.0", PID: 7, Started: now.Add(-time.Hour), Sync: "connected to https://s as riya, 1 room(s)",
				Agents: map[string]time.Time{install.ClaudeCode: now, install.Codex: now},
			}, nil
		},
		Session: func() (auth.Session, error) {
			return auth.Session{Server: "https://s", GitHubLogin: "riya", ServerID: "srv_1", ExpiresUnixMs: now.Add(time.Hour).UnixMilli()}, nil
		},
		Bindings: func() ([]auth.Binding, error) {
			return []auth.Binding{{Repo: "/w/app", Server: "https://s", RoomID: "r1", Name: "app"}}, nil
		},
		ServerInfo: func(context.Context, string) (server.Info, error) {
			return server.Info{ServerID: "srv_1", ProtocolVersion: server.ProtocolVersion}, nil
		},
		Repo: "/w/app",
		GitConfig: func(_ context.Context, _, key string) string {
			return map[string]string{"user.name": "Riya", "user.email": "r@example.com"}[key]
		},
		Binary: "/opt/crewbo",
		AgentPresent: func(agent string) bool {
			return agent != install.Antigravity
		},
		HookConfig: func(agent string) []byte {
			return map[string][]byte{install.ClaudeCode: claude, install.Codex: codex}[agent]
		},
		LoginShell: func() (string, bool) { return "/bin/zsh", true },
		RunShell:   func(context.Context, string) (string, error) { return "", nil },
		Now:        func() time.Time { return now },
	}
}

func byName(rs []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range rs {
		m[r.Name] = r
	}
	return m
}

func TestHealthy(t *testing.T) {
	rs := Run(context.Background(), healthy())
	if Failed(rs) {
		t.Fatalf("a healthy setup failed: %+v", rs)
	}
	got := byName(rs)
	for _, name := range []string{"daemon", "sign-in", "server", "sync", "room", "git identity", "binary path", "hooks: claude-code", "hooks: codex", "login shell"} {
		if r, ok := got[name]; !ok || r.Level != OK || r.Fix != "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if got["hooks: antigravity"].Level != Skip {
		t.Errorf("an absent agent: %+v", got["hooks: antigravity"])
	}
}

func TestProblems(t *testing.T) {
	for name, tc := range map[string]struct {
		breakIt func(*Deps)
		check   string
		level   Level
		want    string // in Detail or Fix
	}{
		"no daemon": {func(d *Deps) {
			d.Status = func(context.Context) (daemon.Status, error) { return daemon.Status{}, errors.New("no") }
		}, "daemon", Fail, "crewbo daemon"},
		"not signed in": {func(d *Deps) {
			d.Session = func() (auth.Session, error) { return auth.Session{}, auth.ErrNoSession }
		}, "sign-in", Fail, "crewbo login --server"},
		"damaged session": {func(d *Deps) {
			d.Session = func() (auth.Session, error) { return auth.Session{}, errors.New("session.json is damaged") }
		}, "sign-in", Fail, "damaged"},
		"expired session": {func(d *Deps) {
			s, _ := d.Session()
			s.ExpiresUnixMs = now.Add(-time.Minute).UnixMilli()
			d.Session = func() (auth.Session, error) { return s, nil }
		}, "sign-in", Fail, "expired"},
		"server down": {func(d *Deps) {
			d.ServerInfo = func(context.Context, string) (server.Info, error) { return server.Info{}, errors.New("timeout") }
		}, "server", Fail, "does not answer"},
		"another server at the address": {func(d *Deps) {
			d.ServerInfo = func(context.Context, string) (server.Info, error) {
				return server.Info{ServerID: "srv_2", ProtocolVersion: server.ProtocolVersion}, nil
			}
		}, "server", Fail, "a different sync server"},
		"other protocol": {func(d *Deps) {
			d.ServerInfo = func(context.Context, string) (server.Info, error) {
				return server.Info{ServerID: "srv_1", ProtocolVersion: server.ProtocolVersion + 1}, nil
			}
		}, "server", Fail, "protocol"},
		"offline": {func(d *Deps) {
			d.Status = func(context.Context) (daemon.Status, error) {
				return daemon.Status{Sync: "offline, retrying: refused"}, nil
			}
		}, "sync", Fail, "offline"},
		"no room": {func(d *Deps) {
			d.Bindings = func() ([]auth.Binding, error) { return nil, nil }
		}, "room", Fail, "crewbo room join"},
		"no git email": {func(d *Deps) {
			d.GitConfig = func(_ context.Context, _, key string) string { return map[string]string{"user.name": "Riya"}[key] }
		}, "git identity", Fail, "user.email not set"},
		"spaced binary": {func(d *Deps) { d.Binary = "/Users/r/My Tools/crewbo" }, "binary path", Fail, "without spaces"},
		"hooks missing": {func(d *Deps) {
			d.HookConfig = func(string) []byte { return nil }
		}, "hooks: codex", Fail, "crewbo hooks install --agent codex"},
		"one agent without hooks": {func(d *Deps) {
			hc := d.HookConfig
			d.HookConfig = func(agent string) []byte {
				if agent == install.Codex {
					return nil
				}
				return hc(agent)
			}
		}, "hooks: codex", Warn, "if you use codex"},
		"hooks never fired": {func(d *Deps) {
			d.Status = func(context.Context) (daemon.Status, error) {
				return daemon.Status{Started: now, Sync: "connected", Agents: map[string]time.Time{install.ClaudeCode: now}}, nil
			}
		}, "hooks: codex", Warn, "/hooks"},
		"noisy shell": {func(d *Deps) {
			d.RunShell = func(context.Context, string) (string, error) { return "Welcome back!\nnvm loaded\n", nil }
		}, "login shell", Fail, `"Welcome back!"`},
		"broken shell": {func(d *Deps) {
			d.RunShell = func(context.Context, string) (string, error) { return "", errors.New("exit 1") }
		}, "login shell", Warn, "failed"},
	} {
		d := healthy()
		tc.breakIt(&d)
		rs := Run(context.Background(), d)
		r := byName(rs)[tc.check]
		if r.Level != tc.level || !strings.Contains(r.Detail+" "+r.Fix, tc.want) {
			t.Errorf("%s: %s = %+v", name, tc.check, r)
		}
		if tc.level == Fail && !Failed(rs) {
			t.Errorf("%s: Failed() is false", name)
		}
		if (tc.level == Fail || tc.level == Warn) && r.Fix == "" {
			t.Errorf("%s: no fix", name)
		}
	}
}

func TestSkips(t *testing.T) {
	d := healthy()
	d.Repo = ""
	d.LoginShell = func() (string, bool) { return "", false }
	got := byName(Run(context.Background(), d))
	for _, name := range []string{"room", "git identity", "login shell"} {
		if got[name].Level != Skip {
			t.Errorf("%s: %+v", name, got[name])
		}
	}
	// No agent that runs hooks through a shell: the shell is not checked.
	d = healthy()
	d.AgentPresent = func(a string) bool { return a == install.ClaudeCode }
	d.RunShell = func(context.Context, string) (string, error) { return "noise", nil }
	if r := byName(Run(context.Background(), d))["login shell"]; r.Level != Skip {
		t.Errorf("shell checked for Claude Code only: %+v", r)
	}
	// Without a daemon, installed hooks are fine and sync is not checked.
	d = healthy()
	d.Status = func(context.Context) (daemon.Status, error) { return daemon.Status{}, errors.New("no") }
	got = byName(Run(context.Background(), d))
	if _, ok := got["sync"]; ok || got["hooks: codex"].Level != OK {
		t.Errorf("without a daemon: %+v", got)
	}
}
