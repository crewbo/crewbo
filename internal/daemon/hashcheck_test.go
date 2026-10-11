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

func TestPostShellHashCheck(t *testing.T) {
	repo := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("src/auth.go", "package auth\n")
	write("src/gone.go", "package gone\n")
	write("mine.go", "package mine\n")
	now := time.Now()
	set := claims.NewSet()
	for _, c := range []claims.Claim{
		{ID: "c1", Pattern: "src/auth.go", Holder: claims.Holder{SessionID: "alice", MemberID: "priya"}, LeaseExpires: now.Add(time.Hour)},
		{ID: "c2", Pattern: "src/gone.go", Holder: claims.Holder{SessionID: "alice"}, LeaseExpires: now.Add(time.Hour)},
		{ID: "c3", Pattern: "web/**", Holder: claims.Holder{SessionID: "alice"}, LeaseExpires: now.Add(time.Hour)},
		{ID: "c4", Pattern: "mine.go", Holder: claims.Holder{SessionID: "bob"}, LeaseExpires: now.Add(time.Hour)},
	} {
		if err := set.Add(c, now); err != nil {
			t.Fatal(err)
		}
	}
	p := testPaths(t)
	start(t, Config{Paths: p, Claims: map[string]*claims.Set{repo: set}})
	c, err := dial(t, p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx := context.Background()
	shell := func(toolUse, cmd string) {
		t.Helper()
		resp, err := c.Call(ctx, Request{Method: MethodDecide, Session: "bob", Repo: repo, Command: cmd, ToolUse: toolUse})
		if err != nil || !resp.Allow {
			t.Fatalf("decide %q = %+v, %v", cmd, resp, err)
		}
	}
	post := func(toolUse string) []Violation {
		t.Helper()
		resp, err := c.Call(ctx, Request{Method: MethodEvent, Event: "post_edit", Session: "bob", Repo: repo, ToolUse: toolUse})
		if err != nil {
			t.Fatal(err)
		}
		return resp.Violations
	}

	// A formatter run rewrites alice's file, deletes another, and touches
	// bob's own claim and a file under alice's glob claim.
	shell("t1", "npm run format")
	write("src/auth.go", "package auth // reformatted\n")
	if err := os.Remove(filepath.Join(repo, "src", "gone.go")); err != nil {
		t.Fatal(err)
	}
	write("mine.go", "package mine // mine to change\n")
	write("web/app.ts", "changed")
	got := map[string]bool{}
	for _, v := range post("t1") {
		got[v.Path] = true
		if v.Command != "npm run format" || v.Session != "bob" {
			t.Errorf("violation details: %+v", v)
		}
	}
	if len(got) != 2 || !got["src/auth.go"] || !got["src/gone.go"] {
		t.Errorf("violations = %v, want src/auth.go and src/gone.go only", got)
	}
	// The snapshot is consumed: a second post_edit reports nothing.
	if vs := post("t1"); len(vs) != 0 {
		t.Errorf("snapshot reused: %+v", vs)
	}
	// A command that changes nothing claimed reports nothing.
	shell("t2", "go test ./...")
	if vs := post("t2"); len(vs) != 0 {
		t.Errorf("unchanged files reported: %+v", vs)
	}
	// Without a tool use id there is nothing to pair.
	shell("", "make")
	write("src/auth.go", "package auth // again\n")
	if vs := post(""); len(vs) != 0 {
		t.Errorf("unpaired post_edit reported: %+v", vs)
	}
	st, _ := c.Call(ctx, Request{Method: MethodStatus})
	if st.Status.Violations != 2 {
		t.Errorf("status violations = %d, want 2", st.Status.Violations)
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	if hashFile(filepath.Join(dir, "missing")) != "" {
		t.Error("missing file should hash to empty")
	}
	small := filepath.Join(dir, "small")
	if err := os.WriteFile(small, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h := hashFile(small); len(h) != 64 {
		t.Errorf("hash = %q", h)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, maxHashedBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if h := hashFile(big); !strings.HasPrefix(h, "large:") {
		t.Errorf("large file hash = %q", h)
	}
}
