package gitx

import "context"

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
