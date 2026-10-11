package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

// MergeResult is the outcome of a merge computed without touching any
// working tree.
type MergeResult struct {
	Clean bool
	// Tree is the OID of the merged top-level tree (with conflict markers
	// in conflicted files).
	Tree string
	// Conflicts lists conflicted paths once each, in git's order.
	Conflicts []string
}

// MergeTree computes the merge of two commits with
// `git merge-tree --write-tree -z --name-only --no-messages`. The docs say
// exit 0 is clean, 1 is conflicted and anything else an error
// (https://git-scm.com/docs/git-merge-tree#_exit_status), but git 2.55.0 also
// exits 1 for an unknown ref, with nothing on stdout. So exit 1 counts as a
// conflict only when a tree OID was printed. With -z every field ends in NUL,
// so file names are never quoted or split.
func (g *Git) MergeTree(ctx context.Context, repo, ours, theirs string) (MergeResult, error) {
	if err := checkRefs(ours, theirs); err != nil {
		return MergeResult{}, err
	}
	out, err := g.run(ctx, repo, "merge-tree", "--write-tree", "-z", "--name-only", "--no-messages", ours, theirs)
	var exitErr *ExitError
	switch {
	case err == nil:
		r, perr := parseMergeTree(out)
		r.Clean = true
		return r, perr
	case errors.As(err, &exitErr) && exitErr.Code == 1 && len(out) > 0:
		return parseMergeTree(out)
	default:
		return MergeResult{}, err
	}
}

func parseMergeTree(out []byte) (MergeResult, error) {
	fields := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	if len(fields) == 0 || len(fields[0]) == 0 {
		return MergeResult{}, fmt.Errorf("gitx: empty merge-tree output")
	}
	r := MergeResult{Tree: string(fields[0])}
	seen := map[string]bool{}
	for _, f := range fields[1:] {
		// A file appears once per conflicted stage; keep it once.
		if name := string(f); name != "" && !seen[name] {
			seen[name] = true
			r.Conflicts = append(r.Conflicts, name)
		}
	}
	return r, nil
}
