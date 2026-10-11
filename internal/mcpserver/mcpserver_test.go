package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/platform"
)

// session starts a daemon (unless claimsByRepo is nil) and an MCP server for
// repo, and returns a connected client session.
func session(t *testing.T, repo string, claimsByRepo map[string]*claims.Set) *mcp.ClientSession {
	t.Helper()
	root, err := os.MkdirTemp("", "cbm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	paths := platform.Paths{Data: filepath.Join(root, "d"), Config: filepath.Join(root, "c"), Runtime: filepath.Join(root, "r")}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if claimsByRepo != nil {
		done := make(chan struct{})
		go func() { defer close(done); _ = daemon.New(daemon.Config{Paths: paths, Claims: claimsByRepo}).Run(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
		deadline := time.Now().Add(5 * time.Second)
		for {
			dctx, dcancel := context.WithTimeout(ctx, time.Second)
			c, err := daemon.Dial(dctx, paths)
			dcancel()
			if err == nil {
				_ = c.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("daemon did not start")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(Config{Paths: paths, Cwd: repo, Version: "test"}).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call[T any](t *testing.T, cs *mcp.ClientSession, name string, args any) (T, *mcp.CallToolResult) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out, res
}

func repoWithClaims(t *testing.T) (string, map[string]*claims.Set) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	set := claims.NewSet()
	for _, c := range []claims.Claim{
		{ID: "c1", Pattern: "src/auth.go", Holder: claims.Holder{SessionID: "s1", MemberID: "priya"}, TaskID: "12", LeaseExpires: now.Add(time.Hour)},
		{ID: "c2", Pattern: "web/**", Holder: claims.Holder{SessionID: daemon.TerminalSession, MemberID: "ronel"}, LeaseExpires: now.Add(time.Hour)},
		{ID: "c3", Pattern: "lib/x.go", Holder: claims.Holder{SessionID: "s9"}, LeaseExpires: now.Add(time.Hour)},
	} {
		if err := set.Add(c, now); err != nil {
			t.Fatal(err)
		}
	}
	return repo, map[string]*claims.Set{repo: set}
}

func TestTools(t *testing.T) {
	repo, cl := repoWithClaims(t)
	cs := session(t, repo, cl)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 2 {
		t.Fatalf("tools = %+v, %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
	}

	claimsOut, _ := call[ClaimsOut](t, cs, "crewbo_claims", map[string]any{})
	if len(claimsOut.Claims) != 3 {
		t.Fatalf("claims = %+v", claimsOut)
	}
	held := map[string]string{}
	for _, c := range claimsOut.Claims {
		held[c.Pattern] = c.HeldBy
	}
	if held["src/auth.go"] != "priya's agent" || held["web/**"] != "ronel (from a terminal)" || held["lib/x.go"] != "agent session s9" {
		t.Errorf("held_by = %v", held)
	}

	who, _ := call[WhoHoldsOut](t, cs, "crewbo_who_holds", map[string]any{"paths": []string{"SRC/Auth.go", "web/app/index.ts", "README.md", filepath.Join(repo, "lib", "x.go"), "/elsewhere/a.go"}})
	want := []bool{true, true, false, true, false}
	if len(who.Results) != len(want) {
		t.Fatalf("results = %+v", who.Results)
	}
	for i, r := range who.Results {
		if r.Claimed != want[i] {
			t.Errorf("%s claimed = %v, want %v", r.Path, r.Claimed, want[i])
		}
	}
	if who.Results[0].Claim.Task != "12" {
		t.Errorf("task = %+v", who.Results[0].Claim)
	}
}

func TestToolErrors(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	cs := session(t, repo, nil) // no daemon
	_, res := call[ClaimsOut](t, cs, "crewbo_claims", map[string]any{})
	if !res.IsError || !strings.Contains(text(res), "daemon is not running") {
		t.Errorf("no daemon: %+v", res)
	}
	cs = session(t, t.TempDir(), nil) // not a repository
	_, res = call[WhoHoldsOut](t, cs, "crewbo_who_holds", map[string]any{"paths": []string{"a"}})
	if !res.IsError || !strings.Contains(text(res), "not working inside a git repository") {
		t.Errorf("outside repo: %+v", res)
	}
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
