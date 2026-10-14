package gitx

import (
	"context"
	"errors"
	"strings"
)

// Head returns the commit the repository's HEAD points at, as a full hex
// object id. It fails in a repository with no commits.
func (g *Git) Head(ctx context.Context, repo string) (string, error) {
	return g.Commit(ctx, repo, "HEAD")
}

// Commit returns the full object id of the commit rev names (a branch,
// tag or commit).
func (g *Git) Commit(ctx context.Context, repo, rev string) (string, error) {
	if err := checkRefs(rev); err != nil {
		return "", err
	}
	out, err := g.run(ctx, repo, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// HasCommit reports whether the repository has commit (any revision that
// names one), without fetching anything.
func (g *Git) HasCommit(ctx context.Context, repo, commit string) (bool, error) {
	if err := checkRefs(commit); err != nil {
		return false, err
	}
	// git cat-file -e exits non-zero when the object is missing or invalid.
	_, err := g.run(ctx, repo, "cat-file", "-e", commit+"^{commit}")
	var exit *ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return err == nil, err
}
