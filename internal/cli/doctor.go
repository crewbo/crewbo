package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/doctor"
	"github.com/crewbo/crewbo/internal/gitx"
	"github.com/crewbo/crewbo/internal/hooks"
	"github.com/crewbo/crewbo/internal/hooks/install"
	"github.com/crewbo/crewbo/internal/platform"
	"github.com/crewbo/crewbo/internal/server"
)

// runDoctor checks this member's setup and prints one line per check, with
// a fix under each problem (M6-02). It exits 1 when a check fails.
func runDoctor(ctx context.Context, stdout, stderr io.Writer) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	env, err := platform.DefaultEnv()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	cwd, _ := os.Getwd()
	repo, _ := hooks.RepoRoot(cwd)
	bin, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(bin); err == nil {
		bin = r
	}
	httpc := &http.Client{Timeout: 5 * time.Second}
	git := gitx.New("")
	d := doctor.Deps{
		Status: func(ctx context.Context) (daemon.Status, error) {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			c, err := daemon.Dial(ctx, p)
			if err != nil {
				return daemon.Status{}, err
			}
			defer func() { _ = c.Close() }()
			resp, err := c.Call(ctx, daemon.Request{Method: daemon.MethodStatus})
			if err != nil || resp.Status == nil {
				return daemon.Status{}, fmt.Errorf("the daemon did not answer: %w", err)
			}
			return *resp.Status, nil
		},
		Session:  func() (auth.Session, error) { return auth.LoadSession(auth.SessionPath(p.Config)) },
		Bindings: func() ([]auth.Binding, error) { return auth.LoadBindings(auth.BindingsPath(p.Config)) },
		ServerInfo: func(ctx context.Context, base string) (server.Info, error) {
			var info server.Info
			err := callServer(ctx, httpc, base, "", http.MethodGet, "/v1/info", nil, &info)
			return info, err
		},
		Repo:      repo,
		GitConfig: git.Config,
		Binary:    bin,
		AgentPresent: func(agent string) bool {
			return agentPresent(agent, install.ConfigPath(agent, env))
		},
		HookConfig: func(agent string) []byte {
			b, err := os.ReadFile(install.ConfigPath(agent, env)) //nolint:gosec // the agent's own config file
			if err != nil {
				return nil
			}
			return b
		},
		LoginShell: platform.LoginShell,
		RunShell: func(ctx context.Context, sh string) (string, error) {
			cmd := exec.CommandContext(ctx, sh, "-c", "true") //nolint:gosec // the member's own login shell, as Codex runs it
			cmd.WaitDelay = time.Second
			out, err := cmd.Output()
			return string(out), err
		},
		Now: time.Now,
	}
	results := doctor.Run(ctx, d)
	return printDoctor(stdout, results)
}

func printDoctor(w io.Writer, rs []doctor.Result) int {
	label := map[doctor.Level]string{doctor.OK: "ok", doctor.Warn: "WARN", doctor.Fail: "FAIL", doctor.Skip: "skip"}
	failed := 0
	for _, r := range rs {
		_, _ = fmt.Fprintf(w, "%-5s %-20s %s\n", label[r.Level], r.Name, cell(r.Detail))
		if r.Fix != "" {
			_, _ = fmt.Fprintf(w, "%-26s fix: %s\n", "", r.Fix)
		}
		if r.Level == doctor.Fail {
			failed++
		}
	}
	if failed > 0 {
		return write(w, ExitError, "\n%d problem(s): fix the FAIL lines above, then run crewbo doctor again.\n", failed)
	}
	return write(w, ExitOK, "\nNo problems found.\n")
}
