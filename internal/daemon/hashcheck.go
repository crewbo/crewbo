package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
)

// Limits for the post-shell hash check.
const (
	maxWatchedFiles = 200
	maxHashedBytes  = 10 << 20
	maxSnapshots    = 256
)

// snapshot holds content hashes of other sessions' claimed files, taken
// before one shell command runs.
type snapshot struct {
	repo    string
	command string
	hashes  map[string]string // repo-relative pattern -> hash ("" when absent)
	claims  map[string]claims.Claim
}

// takeSnapshot fingerprints the literal files claimed by sessions other than
// the caller, so a write the shell parser missed is still noticed afterwards.
//
// ponytail: literal claims only; glob claims (web/**) are skipped because
// expanding them means walking the repository on every command. Upgrade:
// expand globs with `git ls-files` once per room event, not per command.
func (d *Daemon) takeSnapshot(req Request, now time.Time) {
	if req.ToolUse == "" || req.Repo == "" {
		return
	}
	s := &snapshot{repo: req.Repo, command: req.Command, hashes: map[string]string{}, claims: map[string]claims.Claim{}}
	for _, c := range d.set(req.Repo).Active(now) {
		if c.Holder.SessionID == req.Session || claims.IsGlob(c.Pattern) || len(s.hashes) >= maxWatchedFiles {
			continue
		}
		s.hashes[c.Pattern] = hashFile(filepath.Join(req.Repo, filepath.FromSlash(c.Pattern)))
		s.claims[c.Pattern] = c
	}
	if len(s.hashes) == 0 {
		return
	}
	if len(d.snapshots) >= maxSnapshots {
		for k := range d.snapshots { // drop one; abandoned tool calls must not grow memory
			delete(d.snapshots, k)
			break
		}
	}
	d.snapshots[req.Session+"\x00"+req.ToolUse] = s
}

// checkSnapshot compares the snapshot taken before the session's shell
// command with the files now, and records every claimed file that changed.
func (d *Daemon) checkSnapshot(req Request, now time.Time) []Violation {
	key := req.Session + "\x00" + req.ToolUse
	s, ok := d.snapshots[key]
	if !ok {
		return nil
	}
	delete(d.snapshots, key)
	var out []Violation
	for p, before := range s.hashes {
		if hashFile(filepath.Join(s.repo, filepath.FromSlash(p))) == before {
			continue
		}
		v := Violation{Path: p, Claim: s.claims[p], Session: req.Session, Command: s.command, DetectedAt: now}
		out = append(out, v)
		d.violations = append(d.violations, v)
		d.cfg.Log.Warn("claimed file changed by another session", "path", p, "holder", v.Claim.Holder.SessionID, "session", req.Session)
	}
	return out
}

// hashFile returns a SHA-256 of the file's content, "" when it does not
// exist, or a marker when it cannot be read or is too large to hash, so a
// change between those states still counts as a change.
func hashFile(path string) string {
	f, err := os.Open(path) //nolint:gosec // path is a claimed file inside the repository the agent reported
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		return "unreadable"
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxHashedBytes+1))
	if err != nil {
		return "unreadable"
	}
	if n > maxHashedBytes {
		fi, _ := f.Stat()
		return "large:" + fi.ModTime().UTC().Format(time.RFC3339Nano)
	}
	return hex.EncodeToString(h.Sum(nil))
}
