package gitx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		out  string
		want Version
	}{
		{"git version 2.55.0\n", Version{2, 55, 0}},
		{"git version 2.51.0.windows.1", Version{2, 51, 0}},       // Git for Windows, Phase 0
		{"git version 2.54.0 (Apple Git-157)", Version{2, 54, 0}}, // macOS, Phase 0
		{"git version 2.38", Version{2, 38, 0}},
	}
	for _, tt := range tests {
		got, err := ParseVersion(tt.out)
		if err != nil || got != tt.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", tt.out, got, err, tt.want)
		}
	}
	if _, err := ParseVersion("hub version 2.14"); !errors.Is(err, ErrBadVersion) {
		t.Errorf("unrelated output: err = %v", err)
	}
}

func TestVersionLess(t *testing.T) {
	if !(Version{2, 37, 9}).Less(MinVersion) || (Version{2, 38, 0}).Less(MinVersion) || !(Version{1, 99, 0}).Less(MinVersion) || (Version{3, 0, 0}).Less(MinVersion) {
		t.Error("Less ordering is wrong around 2.38")
	}
	if (Version{2, 38, 1}).Less(Version{2, 38, 1}) {
		t.Error("equal versions reported as less")
	}
}

func TestGitNotFound(t *testing.T) {
	_, err := New("crewbo-no-such-git-binary").Version(context.Background())
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("err = %v, want ErrGitNotFound", err)
	}
}

// repo creates a git repository with one commit and returns its path.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := New("").CheckVersion(context.Background()); err != nil {
		t.Skipf("system git unusable for these tests: %v", err)
	}
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "shared.txt", "line1\nline2\nline3\n")
	write(t, dir, "café notes.txt", "a\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "core.autocrlf=false"}, args...)
	cmd := exec.Command("git", full...) //nolint:gosec // test helper with fixed arguments
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// branch creates a branch from main with one commit writing files.
func branch(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	gitT(t, dir, "switch", "-q", "-c", name, "main")
	for f, c := range files {
		write(t, dir, f, c)
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-q", "-m", name)
	gitT(t, dir, "switch", "-q", "main")
}

func TestCheckVersionSystemGit(t *testing.T) {
	repo(t) // skips when git is missing or too old
	v, err := New("").CheckVersion(context.Background())
	if err != nil || v.Less(MinVersion) {
		t.Fatalf("CheckVersion = %v, %v", v, err)
	}
}

func TestMergeTree(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	branch(t, dir, "alice", map[string]string{"shared.txt": "line1\nalice\nline3\n", "café notes.txt": "alice\n"})
	branch(t, dir, "bob", map[string]string{"shared.txt": "line1\nbob\nline3\n", "café notes.txt": "bob\n"})
	branch(t, dir, "carol", map[string]string{"new.txt": "carol\n"})
	g := New("")

	got, err := g.MergeTree(ctx, dir, "alice", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if got.Clean || got.Tree == "" {
		t.Errorf("conflicting merge reported %+v", got)
	}
	want := map[string]bool{"shared.txt": true, "café notes.txt": true}
	if len(got.Conflicts) != len(want) {
		t.Fatalf("conflicts = %q, want %v", got.Conflicts, want)
	}
	for _, c := range got.Conflicts {
		if !want[c] {
			t.Errorf("unexpected conflict path %q (name must not be quoted)", c)
		}
	}

	got, err = g.MergeTree(ctx, dir, "alice", "carol")
	if err != nil || !got.Clean || got.Tree == "" || len(got.Conflicts) != 0 {
		t.Errorf("clean merge = %+v, %v", got, err)
	}

	// git exits 1 here too (observed on 2.55.0), so this must still be an
	// error, not an empty "conflicted" result.
	_, err = g.MergeTree(ctx, dir, "alice", "no-such-branch")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Stderr == "" {
		t.Errorf("unknown ref: err = %v, want an ExitError with stderr", err)
	}
}

func TestRejectsOptionLikeRefs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := New("")
	for _, ref := range []string{"--upload-pack=touch pwned", "-b", ""} {
		if _, err := g.MergeTree(ctx, dir, "main", ref); !errors.Is(err, ErrBadRef) {
			t.Errorf("MergeTree(%q): err = %v, want ErrBadRef", ref, err)
		}
		if err := g.WorktreeAdd(ctx, dir, filepath.Join(dir, "wt"), ref, "main"); !errors.Is(err, ErrBadRef) {
			t.Errorf("WorktreeAdd branch %q: err = %v, want ErrBadRef", ref, err)
		}
	}
}

func TestWorktree(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	g := New("")
	wt := filepath.Join(t.TempDir(), "task 12") // space in the path on purpose
	if err := g.WorktreeAdd(ctx, dir, wt, "crewbo/12-auth", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, "shared.txt")); err != nil {
		t.Fatalf("worktree not checked out: %v", err)
	}
	if err := g.WorktreeAdd(ctx, dir, filepath.Join(t.TempDir(), "again"), "crewbo/12-auth", "main"); err == nil {
		t.Error("adding a second worktree on an existing branch succeeded")
	}
	write(t, wt, "shared.txt", "dirty\n")
	if err := g.WorktreeRemove(ctx, dir, wt, false); err == nil {
		t.Error("removing a dirty worktree without force succeeded")
	}
	if err := g.WorktreeRemove(ctx, dir, wt, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree directory still exists: %v", err)
	}
}

func TestHead(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	h, err := New("").Head(ctx, dir)
	if err != nil || len(h) < 40 || strings.TrimLeft(h, "0123456789abcdef") != "" {
		t.Fatalf("Head = %q, %v", h, err)
	}
	branch(t, dir, "feature", map[string]string{"x.txt": "x\n"})
	gitT(t, dir, "switch", "-q", "feature")
	if h2, err := New("").Head(ctx, dir); err != nil || h2 == h {
		t.Errorf("Head after switching branch = %q, %v (was %q)", h2, err, h)
	}
	empty := t.TempDir()
	gitT(t, empty, "init", "-q")
	if _, err := New("").Head(ctx, empty); err == nil {
		t.Error("Head of a repository with no commits succeeded")
	}
}

func TestHasCommit(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	g := New("")
	h, err := g.Head(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	for commit, want := range map[string]bool{h: true, h[:12]: true, "HEAD": true, strings.Repeat("0", 40): false, "no-such-branch": false} {
		if got, err := g.HasCommit(ctx, dir, commit); err != nil || got != want {
			t.Errorf("HasCommit(%q) = %v, %v; want %v", commit, got, err, want)
		}
	}
	if _, err := g.HasCommit(ctx, dir, "--upload-pack=x"); !errors.Is(err, ErrBadRef) {
		t.Errorf("option-like commit: %v", err)
	}
}

func TestCheckpointCommitAndPush(t *testing.T) {
	// Only the repository's own config counts here, not the machine's.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := repo(t)
	ctx := context.Background()
	g := New("")

	// Nothing to commit needs no identity.
	if commit, err := g.CommitAll(ctx, dir, "checkpoint"); err != nil || commit != "" {
		t.Fatalf("nothing to commit, no identity: %q, %v", commit, err)
	}
	write(t, dir, "new.txt", "agent work\n")
	if _, err := g.CommitAll(ctx, dir, "checkpoint"); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("commit without an identity: %v", err)
	}
	gitT(t, dir, "config", "user.name", "Member")
	gitT(t, dir, "config", "user.email", "member@example.com")
	commit, err := g.CommitAll(ctx, dir, "checkpoint")
	if err != nil || commit == "" {
		t.Fatalf("CommitAll = %q, %v", commit, err)
	}
	if again, err := g.CommitAll(ctx, dir, "checkpoint"); err != nil || again != "" {
		t.Errorf("nothing to commit: %q, %v", again, err)
	}

	if _, err := g.PushRemote(ctx, dir); !errors.Is(err, ErrNoRemote) {
		t.Errorf("no remote: %v", err)
	}
	bare := t.TempDir()
	gitT(t, bare, "init", "-q", "--bare")
	gitT(t, dir, "remote", "add", "upstream", bare)
	if r, err := g.PushRemote(ctx, dir); err != nil || r != "upstream" {
		t.Errorf("the only remote: %q, %v", r, err)
	}
	gitT(t, dir, "remote", "add", "other", bare)
	if _, err := g.PushRemote(ctx, dir); !errors.Is(err, ErrNoRemote) {
		t.Errorf("several remotes without origin: %v", err)
	}
	gitT(t, dir, "remote", "add", "origin", bare)
	remote, err := g.PushRemote(ctx, dir)
	if err != nil || remote != "origin" {
		t.Fatalf("origin among several: %q, %v", remote, err)
	}
	if err := g.Push(ctx, dir, remote, commit, "refs/crewbo/member/task-1"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", bare, "rev-parse", "refs/crewbo/member/task-1").Output() //nolint:gosec // test
	if err != nil || strings.TrimSpace(string(out)) != commit {
		t.Errorf("ref on the remote = %q, %v; want %s", out, err, commit)
	}
	// Another clone fetches the checkpoint and can then merge-tree against it.
	other := t.TempDir()
	gitT(t, other, "clone", "-q", "--no-local", bare, ".") // a real transport, not a copy of the object store
	if ok, _ := g.HasCommit(ctx, other, commit); ok {
		t.Fatal("the clone already has the checkpoint")
	}
	if err := g.Fetch(ctx, other, "origin", "refs/crewbo/member/task-1"); err != nil {
		t.Fatal(err)
	}
	if ok, err := g.HasCommit(ctx, other, commit); !ok || err != nil {
		t.Errorf("after fetch: %v %v", ok, err)
	}
	if err := g.Fetch(ctx, other, "origin", "--upload-pack=x"); !errors.Is(err, ErrBadRef) {
		t.Errorf("option-like ref: %v", err)
	}
	if err := g.Push(ctx, dir, "--upload-pack=x", commit, "refs/crewbo/m/t"); !errors.Is(err, ErrBadRef) {
		t.Errorf("option-like remote: %v", err)
	}
}

// The pieces of a merge that touches no working tree: the remote's default
// branch and where it points, merge-tree, commit-tree with two parents.
func TestMergeWithoutAWorkingTree(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := repo(t)
	ctx := context.Background()
	g := New("")
	gitT(t, dir, "config", "user.name", "Member")
	gitT(t, dir, "config", "user.email", "member@example.com")
	bare := t.TempDir()
	gitT(t, bare, "init", "-q", "--bare", "-b", "main")
	gitT(t, dir, "remote", "add", "origin", bare)
	gitT(t, dir, "push", "-q", "origin", "main")
	branch, err := g.DefaultBranch(ctx, dir, "origin")
	if err != nil || branch != "refs/heads/main" {
		t.Fatalf("DefaultBranch = %q, %v", branch, err)
	}
	main, err := g.RemoteRef(ctx, dir, "origin", branch)
	if err != nil || main != gitOut(t, dir, "rev-parse", "main") {
		t.Fatalf("RemoteRef = %q, %v", main, err)
	}
	if _, err := g.RemoteRef(ctx, dir, "origin", "refs/heads/nope"); !errors.Is(err, ErrNoRef) {
		t.Errorf("missing ref: %v", err)
	}
	branchTo(t, dir, "feature", "new.txt", "feature work\n")
	feature := gitOut(t, dir, "rev-parse", "feature")
	res, err := g.MergeTree(ctx, dir, main, feature)
	if err != nil || !res.Clean {
		t.Fatalf("merge-tree: %+v %v", res, err)
	}
	merged, err := g.CommitTree(ctx, dir, res.Tree, "Merge feature", main, feature)
	if err != nil {
		t.Fatal(err)
	}
	if parents := gitOut(t, dir, "rev-list", "--parents", "-n", "1", merged); parents != merged+" "+main+" "+feature {
		t.Errorf("parents %q", parents)
	}
	if author := gitOut(t, dir, "log", "-1", "--format=%an", merged); author != "Member" {
		t.Errorf("author %q", author)
	}
	if err := g.Push(ctx, dir, "origin", merged, branch); err != nil {
		t.Fatal(err)
	}
	if got, _ := g.RemoteRef(ctx, dir, "origin", branch); got != merged {
		t.Errorf("main on the remote = %s, want %s", got, merged)
	}
	// A push based on a stale main is refused by the remote.
	branchTo(t, dir, "stale", "other.txt", "x\n")
	stale, _ := g.CommitTree(ctx, dir, gitOut(t, dir, "rev-parse", "stale^{tree}"), "stale", main)
	if err := g.Push(ctx, dir, "origin", stale, branch); err == nil {
		t.Error("a push that does not include the current main was accepted")
	}
	if _, err := g.CommitTree(ctx, dir, res.Tree, "x", "--upload-pack=y"); !errors.Is(err, ErrBadRef) {
		t.Errorf("option-like parent: %v", err)
	}
}

// branchTo commits one file on a new branch from main, leaving main checked out.
func branchTo(t *testing.T, dir, name, file, content string) {
	t.Helper()
	gitT(t, dir, "switch", "-q", "-c", name, "main")
	write(t, dir, file, content)
	gitT(t, dir, "add", file)
	gitT(t, dir, "commit", "-q", "-m", name)
	gitT(t, dir, "switch", "-q", "main")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeForABranch(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	g := New("")
	gitT(t, dir, "branch", "feat/login")
	if _, ok, err := g.WorktreeFor(ctx, dir, "feat/login"); ok || err != nil {
		t.Errorf("a branch checked out nowhere: %v %v", ok, err)
	}
	if path, ok, err := g.WorktreeFor(ctx, dir, "main"); !ok || err != nil || evalT(t, path) != evalT(t, dir) {
		t.Errorf("main: %q %v %v", path, ok, err)
	}
	wt := filepath.Join(t.TempDir(), "with space")
	if err := g.WorktreeAddBranch(ctx, dir, wt, "feat/login"); err != nil {
		t.Fatal(err)
	}
	if path, ok, err := g.WorktreeFor(ctx, dir, "feat/login"); !ok || err != nil || evalT(t, path) != evalT(t, wt) {
		t.Errorf("feat/login: %q %v %v", path, ok, err)
	}
	if err := g.WorktreeAddBranch(ctx, dir, filepath.Join(t.TempDir(), "again"), "feat/login"); err == nil {
		t.Error("a branch was checked out twice")
	}
	if _, _, err := g.WorktreeFor(ctx, dir, "--upload-pack=x"); !errors.Is(err, ErrBadRef) {
		t.Errorf("option-like branch: %v", err)
	}
}

func evalT(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
