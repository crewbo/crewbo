package gitx

import (
	"context"
	"strings"
)

// WorktreeAdd creates a worktree at path on a new branch started from base,
// for one dispatched task (ADR 0005).
func (g *Git) WorktreeAdd(ctx context.Context, repo, path, branch, base string) error {
	if err := checkRefs(branch, base); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, "worktree", "add", "-b", branch, "--", path, base)
	return err
}

// WorktreeRemove removes the worktree at path. With force, uncommitted
// changes in it are discarded.
func (g *Git) WorktreeRemove(ctx context.Context, repo, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := g.run(ctx, repo, append(args, "--", path)...)
	return err
}

// WorktreeFor returns the worktree where branch (for example feat/login)
// is checked out, from `git worktree list --porcelain -z`: records of
// "worktree <path>" then "branch refs/heads/<name>", each line ending in
// NUL.
func (g *Git) WorktreeFor(ctx context.Context, repo, branch string) (string, bool, error) {
	if err := checkRefs(branch); err != nil {
		return "", false, err
	}
	out, err := g.run(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", false, err
	}
	var current string
	for _, line := range strings.Split(string(out), "\x00") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			current = path
		} else if line == "branch refs/heads/"+branch {
			return current, true, nil
		}
	}
	return "", false, nil
}

// WorktreeAddBranch checks out an existing branch in a new worktree at
// path.
func (g *Git) WorktreeAddBranch(ctx context.Context, repo, path, branch string) error {
	if err := checkRefs(branch); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, "worktree", "add", "--", path, branch)
	return err
}
