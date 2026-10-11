// Package claims holds Crewbo's claim and lease logic: which agent session
// holds which repository paths, until when, and whether an edit is allowed.
//
// The package is pure: no I/O, no goroutines, no clock reads. Callers pass the
// current time, so the daemon, the sync server and the simulator in test/sim
// all drive the same code deterministically. A Set is not safe for concurrent
// use; the daemon's owner goroutine serialises access (docs/architecture/components.md).
package claims

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// DefaultLease is how long a claim stays active without renewal (ADR 0003).
const DefaultLease = 5 * time.Minute

// Holder identifies who holds a claim.
type Holder struct {
	SessionID string // agent session, the unit that renews leases
	MemberID  string
}

// Claim is an advisory lease on a repository-relative path or glob.
type Claim struct {
	ID           string
	Pattern      string // canonical form, see ValidatePattern
	Holder       Holder
	TaskID       string // optional
	LeaseExpires time.Time
}

// active reports whether the claim's lease is still running at now.
func (c Claim) active(now time.Time) bool { return now.Before(c.LeaseExpires) }

// Errors returned by Set methods.
var (
	ErrDuplicateID = errors.New("claims: duplicate claim id")
	ErrNoSession   = errors.New("claims: holder session id is required")
	ErrExpired     = errors.New("claims: lease already expired")
)

// ConflictError reports that a pattern overlaps an active claim held by
// another session.
type ConflictError struct {
	Pattern  string
	Existing Claim
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("claims: %q overlaps %q held by session %s", e.Pattern, e.Existing.Pattern, e.Existing.Holder.SessionID)
}

// Set is the collection of claims in one room.
type Set struct {
	claims map[string]Claim
}

// NewSet returns an empty Set.
func NewSet() *Set { return &Set{claims: map[string]Claim{}} }

// Add records c if its pattern is valid, its lease is active at now, and it
// does not overlap an active claim held by a different session. Overlaps with
// the same session are allowed.
func (s *Set) Add(c Claim, now time.Time) error {
	if c.Holder.SessionID == "" {
		return ErrNoSession
	}
	if !c.active(now) {
		return fmt.Errorf("claim %q: %w", c.ID, ErrExpired)
	}
	if err := ValidatePattern(c.Pattern); err != nil {
		return fmt.Errorf("claim %q: %w", c.Pattern, err)
	}
	if _, ok := s.claims[c.ID]; ok {
		return fmt.Errorf("claim %q: %w", c.ID, ErrDuplicateID)
	}
	if existing, ok := s.conflict(c.Pattern, c.Holder.SessionID, now); ok {
		return &ConflictError{Pattern: c.Pattern, Existing: existing}
	}
	s.claims[c.ID] = c
	return nil
}

// Renew extends every active claim of the session to now plus lease and
// returns how many were renewed. Expired claims are not revived.
func (s *Set) Renew(sessionID string, now time.Time, lease time.Duration) int {
	n := 0
	for id, c := range s.claims {
		if c.Holder.SessionID == sessionID && c.active(now) {
			c.LeaseExpires = now.Add(lease)
			s.claims[id] = c
			n++
		}
	}
	return n
}

// Release removes the claim with the given id and reports whether it existed.
func (s *Set) Release(id string) bool {
	_, ok := s.claims[id]
	delete(s.claims, id)
	return ok
}

// ReleaseSession removes every claim held by the session and returns them.
func (s *Set) ReleaseSession(sessionID string) []Claim {
	return s.removeWhere(func(c Claim) bool { return c.Holder.SessionID == sessionID })
}

// Expire removes claims whose lease ended at or before now and returns them.
func (s *Set) Expire(now time.Time) []Claim {
	return s.removeWhere(func(c Claim) bool { return !c.active(now) })
}

// Active returns the claims active at now, sorted by id.
func (s *Set) Active(now time.Time) []Claim {
	var out []Claim
	for _, c := range s.claims {
		if c.active(now) {
			out = append(out, c)
		}
	}
	sortByID(out)
	return out
}

// Decision is the result of Decide.
type Decision struct {
	Allow     bool
	Path      string // first blocked path when Allow is false
	BlockedBy Claim
}

// Decide reports whether the session may write all of the given
// repository-relative paths at now. It denies on the first path that
// overlaps an active claim held by another session. It does not renew leases.
func (s *Set) Decide(sessionID string, paths []string, now time.Time) Decision {
	for _, p := range paths {
		if c, ok := s.conflict(p, sessionID, now); ok {
			return Decision{Allow: false, Path: p, BlockedBy: c}
		}
	}
	return Decision{Allow: true}
}

// MentionedBy returns an active claim held by another session whose path
// appears literally in command, matching the full pattern or its file name
// without regard to case. It is the fallback for shell commands whose writes
// cannot be parsed (an interpreter, a script, `patch`): Phase 0 S3 measured
// no false positives when it is applied only to such commands. Glob claims
// are skipped, since a glob cannot be matched against free text.
func (s *Set) MentionedBy(sessionID, command string, now time.Time) (Claim, bool) {
	text := strings.ToLower(command)
	for _, c := range s.Active(now) {
		if c.Holder.SessionID == sessionID || IsGlob(c.Pattern) {
			continue
		}
		p := strings.ToLower(c.Pattern)
		if strings.Contains(text, p) || strings.Contains(text, path.Base(p)) {
			return c, true
		}
	}
	return Claim{}, false
}

// conflict returns the first active claim (by id, for determinism) that
// overlaps pattern and is held by a session other than sessionID.
func (s *Set) conflict(pattern, sessionID string, now time.Time) (Claim, bool) {
	for _, c := range s.Active(now) {
		if c.Holder.SessionID != sessionID && Overlaps(pattern, c.Pattern) {
			return c, true
		}
	}
	return Claim{}, false
}

func (s *Set) removeWhere(match func(Claim) bool) []Claim {
	var out []Claim
	for id, c := range s.claims {
		if match(c) {
			out = append(out, c)
			delete(s.claims, id)
		}
	}
	sortByID(out)
	return out
}

func sortByID(cs []Claim) { sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID }) }
