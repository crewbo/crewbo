package gitx

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoIdentity means git has no user.name or user.email for the
// repository, so a commit would fail or carry an invented author.
var ErrNoIdentity = errors.New("gitx: git user.name and user.email are not set")

// ErrNoRemote means the repository has no remote to push to, or several
// and none is called origin.
var ErrNoRemote = errors.New("gitx: no remote to push to")

// CommitAll stages every change in the worktree and commits it with
// message, as the member's own git identity. It returns the new commit, or
// "" when there was nothing to commit.
func (g *Git) CommitAll(ctx context.Context, worktree, message string) (string, error) {
	if err := g.identity(ctx, worktree); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, worktree, "add", "--all"); err != nil {
		return "", err
	}
	// git diff --cached --quiet exits 1 when something is staged.
	_, err := g.run(ctx, worktree, "diff", "--cached", "--quiet")
	var exit *ExitError
	switch {
	case err == nil:
		return "", nil
	case !errors.As(err, &exit) || exit.Code != 1:
		return "", err
	}
	if _, err := g.run(ctx, worktree, "commit", "--quiet", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	return g.Head(ctx, worktree)
}

// PushRemote returns the remote to push checkpoints to: the only remote,
// or origin when there are several.
func (g *Git) PushRemote(ctx context.Context, repo string) (string, error) {
	out, err := g.run(ctx, repo, "remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(string(out))
	switch {
	case len(remotes) == 1:
		return remotes[0], nil
	case slices.Contains(remotes, "origin"):
		return "origin", nil
	case len(remotes) == 0:
		return "", fmt.Errorf("%w: the repository has no remote", ErrNoRemote)
	}
	return "", fmt.Errorf("%w: several remotes (%s) and none is origin", ErrNoRemote, strings.Join(remotes, ", "))
}

// Push pushes commit to ref on remote (for example refs/crewbo/<member>/<task>).
func (g *Git) Push(ctx context.Context, repo, remote, commit, ref string) error {
	if err := checkRefs(remote, commit, ref); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, "push", "--quiet", "--no-verify", remote, commit+":"+ref)
	return err
}

// Fetch fetches ref from remote so its commits are available locally
// (for MergeTree); it updates no local branch.
func (g *Git) Fetch(ctx context.Context, repo, remote, ref string) error {
	if err := checkRefs(remote, ref); err != nil {
		return err
	}
	_, err := g.run(ctx, repo, "fetch", "--quiet", "--no-tags", remote, ref)
	return err
}

// identity refuses when git has no user.name or user.email for dir.
func (g *Git) identity(ctx context.Context, dir string) error {
	for _, key := range []string{"user.name", "user.email"} {
		out, err := g.run(ctx, dir, "config", "--get", key)
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return fmt.Errorf("%w (set them with git config --global user.name and user.email)", ErrNoIdentity)
		}
	}
	return nil
}

// ErrNoRef means the remote has no such ref.
var ErrNoRef = errors.New("gitx: the remote has no such ref")

// DefaultBranch returns the ref the remote's HEAD points at, for example
// refs/heads/main (`git ls-remote --symref <remote> HEAD` prints
// "ref: refs/heads/main<TAB>HEAD" first).
func (g *Git) DefaultBranch(ctx context.Context, repo, remote string) (string, error) {
	if err := checkRefs(remote); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, "ls-remote", "--symref", remote, "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if target, ok := strings.CutPrefix(line, "ref: "); ok {
			if ref, _, ok := strings.Cut(target, "\t"); ok && strings.HasPrefix(ref, "refs/heads/") {
				return ref, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %s has no default branch", ErrNoRef, remote)
}

// RemoteRef returns the commit ref points at on remote.
func (g *Git) RemoteRef(ctx context.Context, repo, remote, ref string) (string, error) {
	if err := checkRefs(remote, ref); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, "ls-remote", remote, ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if commit, name, ok := strings.Cut(line, "\t"); ok && name == ref {
			return commit, nil
		}
	}
	return "", fmt.Errorf("%w: %s on %s", ErrNoRef, ref, remote)
}

// CommitTree writes a commit of tree with parents and message, as the
// member's own git identity, without touching any working tree.
func (g *Git) CommitTree(ctx context.Context, repo, tree, message string, parents ...string) (string, error) {
	if err := g.identity(ctx, repo); err != nil {
		return "", err
	}
	if err := checkRefs(append([]string{tree}, parents...)...); err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	out, err := g.run(ctx, repo, append(args, "-m", message)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
