// Package gitx runs the system git binary for the operations Crewbo needs:
// version checks, worktrees and conflict detection with merge-tree.
// It never uses go-git (ADR 0005).
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// MinVersion is the oldest git Crewbo supports: 2.38 added
// `git merge-tree --write-tree` (Documentation/RelNotes/2.38.0).
var MinVersion = Version{Major: 2, Minor: 38}

// Errors returned by Git methods.
var (
	ErrGitNotFound = errors.New("gitx: git executable not found")
	ErrGitTooOld   = errors.New("gitx: git is older than 2.38")
	ErrBadVersion  = errors.New("gitx: cannot parse git version")
	// ErrBadRef rejects ref or branch names git would read as options.
	ErrBadRef = errors.New("gitx: ref must not be empty or start with '-'")
)

// checkRefs guards against git option injection: refs can come from other
// machines (task branches in room events), and a value such as
// "--upload-pack=..." must never reach git as an argument.
func checkRefs(refs ...string) error {
	for _, r := range refs {
		if r == "" || strings.HasPrefix(r, "-") {
			return fmt.Errorf("%w: %q", ErrBadRef, r)
		}
	}
	return nil
}

// Git runs one git executable.
type Git struct {
	path string
}

// New returns a Git that runs the executable at path, or "git" from PATH
// when path is empty.
func New(path string) *Git {
	if path == "" {
		path = "git"
	}
	return &Git{path: path}
}

// run executes git with args in dir and returns stdout. A non-zero exit
// returns *ExitError carrying the code and stderr.
func (g *Git) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	// core.longpaths is read by Git for Windows and ignored elsewhere, so it
	// is passed on every OS instead of branching on the OS here.
	full := append([]string{"-c", "core.longpaths=true"}, args...)
	cmd := exec.CommandContext(ctx, g.path, full...) //nolint:gosec // argv without a shell; refs pass checkRefs, paths follow "--"
	cmd.Dir = dir
	// Never block on a credential prompt; Crewbo runs git unattended.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrGitNotFound, g.path)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), &ExitError{Args: args, Code: exitErr.ExitCode(), Stderr: strings.TrimSpace(stderr.String())}
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// ExitError is a git command that exited non-zero.
type ExitError struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("git %s: exit %d: %s", strings.Join(e.Args, " "), e.Code, e.Stderr)
}

// Version is a git release number.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v is older than w.
func (v Version) Less(w Version) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}
	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}
	return v.Patch < w.Patch
}

var versionRE = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion reads `git --version` output, such as "git version 2.55.0",
// "git version 2.51.0.windows.1" or "git version 2.54.0 (Apple Git-157)".
func ParseVersion(out string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(out))
	if m == nil {
		return Version{}, fmt.Errorf("%w: %q", ErrBadVersion, out)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}
	return v, nil
}

// Version returns the installed git version.
func (g *Git) Version(ctx context.Context) (Version, error) {
	out, err := g.run(ctx, "", "--version")
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(string(out))
}

// CheckVersion returns the installed version, or ErrGitTooOld when it is
// older than MinVersion (failure mode F18).
func (g *Git) CheckVersion(ctx context.Context) (Version, error) {
	v, err := g.Version(ctx)
	if err != nil {
		return v, err
	}
	if v.Less(MinVersion) {
		return v, fmt.Errorf("%w: found %s", ErrGitTooOld, v)
	}
	return v, nil
}
