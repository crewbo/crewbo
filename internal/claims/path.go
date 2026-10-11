package claims

import (
	"errors"
	"path"
	"strings"
)

// Path errors returned by Normalize and ValidatePattern.
var (
	ErrEmptyPath   = errors.New("claims: empty path")
	ErrOutsideRepo = errors.New("claims: path is outside the repository")
	ErrBadPattern  = errors.New("claims: invalid pattern")
)

// Normalize converts a path reported by an agent into the canonical
// repository-relative form: forward slashes, no "." or ".." segments.
//
// root is the repository or worktree root and p the reported path; both may
// use POSIX or Windows syntax, and p may be absolute or relative to root.
// Observed forms (test/fixtures/hooks): absolute POSIX, absolute Windows with
// backslashes (Claude Code), relative apply_patch paths (Codex 0.154.0 on
// Windows, 0.162.1 on macOS). Paths from any OS must normalise identically on
// every OS, so this uses the path package, not path/filepath.
func Normalize(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", ErrEmptyPath
	}
	r := toSlash(root)
	q := toSlash(p)
	if !isAbs(q) {
		q = r + "/" + q
	}
	r, q = path.Clean(r), path.Clean(q)
	if r == q {
		return "", ErrOutsideRepo
	}
	prefix := strings.TrimSuffix(r, "/") + "/"
	// Drive letters and user directories differ in case between tools on
	// Windows, so the root prefix is matched case-insensitively.
	if len(q) < len(prefix) || !strings.EqualFold(q[:len(prefix)], prefix) {
		return "", ErrOutsideRepo
	}
	return q[len(prefix):], nil
}

// ValidatePattern checks that a claim pattern is repository-relative and
// canonical: forward slashes, no leading "/", no empty, "." or ".." segments.
func ValidatePattern(pattern string) error {
	if pattern == "" || strings.Contains(pattern, `\`) || isAbs(pattern) {
		return ErrBadPattern
	}
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ErrBadPattern
		}
	}
	return nil
}

func toSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// isAbs reports whether a slash path is absolute in POSIX ("/x") or
// Windows ("C:/x", "//server/share") syntax.
func isAbs(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && p[2] == '/' &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}
