package gitx

import (
	"context"
	"strings"
)

// Head returns the commit the repository's HEAD points at, as a full hex
// object id. It fails in a repository with no commits.
func (g *Git) Head(ctx context.Context, repo string) (string, error) {
	out, err := g.run(ctx, repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
