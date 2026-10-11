package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
)

func TestClaimAddListReleasePerRepo(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	clk := newClock(t0)
	agent := claims.NewSet()
	if err := agent.Add(claims.Claim{ID: "a1", Pattern: "web/**", Holder: claims.Holder{SessionID: "codex-1", MemberID: "priya"}, LeaseExpires: t0.Add(time.Hour)}, t0); err != nil {
		t.Fatal(err)
	}
	p := testPaths(t)
	start(t, Config{Paths: p, Now: clk.Now, Claims: map[string]*claims.Set{"/repo/A": agent}})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()
	add := func(repo string, lease int64, patterns ...string) (Response, error) {
		return c.Call(ctx, Request{Method: MethodClaimAdd, Session: TerminalSession, Member: "ronel", Repo: repo, Patterns: patterns, LeaseSeconds: lease})
	}

	resp, err := add("/repo/A", 0, "src/auth.go", "docs/*.md")
	if err != nil || len(resp.Claims) != 2 || !resp.Claims[0].LeaseExpires.Equal(t0.Add(DefaultCLILease)) {
		t.Fatalf("add = %+v, %v", resp, err)
	}
	// All or nothing: web/index.ts conflicts with priya's agent, so lib.go is
	// rolled back too.
	resp, err = add("/repo/A", 0, "lib.go", "web/index.ts")
	if err == nil || resp.BlockedBy == nil || resp.BlockedBy.Holder.MemberID != "priya" || resp.Path != "web/index.ts" {
		t.Fatalf("conflicting add = %+v, %v", resp, err)
	}
	list, _ := c.Call(ctx, Request{Method: MethodClaimList, Repo: "/repo/A"})
	if len(list.Claims) != 3 {
		t.Fatalf("list = %+v, want 3 claims (a1 plus 2); lib.go must not be held", list.Claims)
	}
	// Another repository is independent: the same path is free there.
	if resp, err := add("/repo/B", 0, "src/auth.go"); err != nil || len(resp.Claims) != 1 {
		t.Fatalf("other repo add = %+v, %v", resp, err)
	}
	// An agent in repo A is blocked by the terminal claim, also on the case
	// spelling of the repo path.
	d, _ := c.Call(ctx, Request{Method: MethodDecide, Session: "claude-1", Repo: "/REPO/a", Paths: []string{"src/auth.go"}})
	if d.Allow || d.BlockedBy.Holder.SessionID != TerminalSession {
		t.Fatalf("decide = %+v", d)
	}
	// Leases are capped at MaxCLILease.
	resp, _ = add("/repo/C", int64((24 * time.Hour).Seconds()), "x.go")
	if !resp.Claims[0].LeaseExpires.Equal(t0.Add(MaxCLILease)) {
		t.Errorf("lease = %v, want capped at %v", resp.Claims[0].LeaseExpires, t0.Add(MaxCLILease))
	}
	id := list.Claims[1].ID
	if r, err := c.Call(ctx, Request{Method: MethodClaimRelease, Repo: "/repo/A", IDs: []string{id}}); err != nil || r.Released != 1 {
		t.Fatalf("release = %+v, %v", r, err)
	}
	if _, err := c.Call(ctx, Request{Method: MethodClaimRelease, Repo: "/repo/A", IDs: []string{id}}); err == nil {
		t.Error("releasing an unknown id succeeded")
	}
	if _, err := add("/repo/A", 0); err == nil {
		t.Error("add without patterns succeeded")
	}
	st, _ := c.Call(ctx, Request{Method: MethodStatus})
	if st.Status.ActiveClaims != 4 { // a1, one of A's two, B, C
		t.Errorf("active claims = %d, want 4", st.Status.ActiveClaims)
	}
}

func TestRepoKey(t *testing.T) {
	if RepoKey("") != "" || RepoKey("/A/b/../C") != "/a/c" {
		t.Errorf("RepoKey basics: %q %q", RepoKey(""), RepoKey("/A/b/../C"))
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if RepoKey(link) != RepoKey(target) {
		t.Errorf("symlinked path %q and target %q map to different keys", RepoKey(link), RepoKey(target))
	}
	if !strings.HasSuffix(RepoKey(target), "/real") {
		t.Errorf("RepoKey(%q) = %q", target, RepoKey(target))
	}
}
