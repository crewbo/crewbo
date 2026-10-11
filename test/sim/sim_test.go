// Package sim drives internal/claims with seeded random sessions and checks
// it against an independent model after every step (issue #14).
//
// Fixed seeds run in CI. To explore more, pass a seed:
//
//	go test ./test/sim -run Sim -seed 12345 -steps 20000
//
// A failure prints the seed and step, so any run can be replayed exactly.
package sim

import (
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
)

var (
	seedFlag  = flag.Int64("seed", 0, "run a single seed instead of the fixed CI seeds")
	stepsFlag = flag.Int("steps", 3000, "steps per seed")
)

// patterns is the pool sessions claim from: literals, globs, nested paths and
// case variants, so overlaps and case folding are exercised constantly.
var patterns = []string{
	"src/auth.go", "src/limits.go", "src/*.go", "src/**", "SRC/Auth.go",
	"web/app.ts", "web/**", "docs/*.md", "docs/a.md", "go.mod", "*.go", "lib/x/y.go",
}

var sessions = []string{"alice", "bob", "carol", "dev"}

// modelClaim is the model's view of one claim.
type modelClaim struct {
	pattern, session string
	expires          time.Time
}

type world struct {
	t       *testing.T
	seed    int64
	rng     *rand.Rand
	set     *claims.Set
	model   map[string]modelClaim
	offline map[string]time.Time // session -> offline until (stops renewing)
	now     time.Time
	nextID  int
	trace   []string
}

func newWorld(t *testing.T, seed int64) *world {
	return &world{
		t:       t,
		seed:    seed,
		rng:     rand.New(rand.NewSource(seed)), //nolint:gosec // seeded on purpose: runs must replay exactly
		set:     claims.NewSet(),
		model:   map[string]modelClaim{},
		offline: map[string]time.Time{},
		now:     time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
	}
}

func (w *world) fail(step int, format string, a ...any) {
	w.t.Helper()
	w.t.Fatalf("seed %d step %d: %s", w.seed, step, fmt.Sprintf(format, a...))
}

func (w *world) pick(xs []string) string { return xs[w.rng.Intn(len(xs))] }

func (w *world) online(s string) bool { return !w.now.Before(w.offline[s]) }

// step performs one random action and records it in the trace.
func (w *world) step(i int) {
	// Time moves 0 to 90 s per step, so leases (5 min) expire regularly.
	w.now = w.now.Add(time.Duration(w.rng.Intn(91)) * time.Second)
	s := w.pick(sessions)
	switch op := w.rng.Intn(100); {
	case op < 35: // claim
		w.nextID++
		id := fmt.Sprintf("c%04d", w.nextID)
		c := claims.Claim{ID: id, Pattern: w.pick(patterns), Holder: claims.Holder{SessionID: s}, LeaseExpires: w.now.Add(claims.DefaultLease)}
		err := w.set.Add(c, w.now)
		conflict := w.modelConflict(c.Pattern, s)
		if (err == nil) == conflict {
			w.fail(i, "Add %s %q by %s: err=%v, model conflict=%v", id, c.Pattern, s, err, conflict)
		}
		if err == nil {
			w.model[id] = modelClaim{c.Pattern, s, c.LeaseExpires}
		}
		w.trace = append(w.trace, fmt.Sprintf("add %s %s %s %v", id, s, c.Pattern, err == nil))
	case op < 60: // hook event: renew if online
		if !w.online(s) {
			w.trace = append(w.trace, "offline "+s)
			return
		}
		n := w.set.Renew(s, w.now, claims.DefaultLease)
		want := 0
		for id, m := range w.model {
			if m.session == s && w.now.Before(m.expires) {
				m.expires = w.now.Add(claims.DefaultLease)
				w.model[id] = m
				want++
			}
		}
		if n != want {
			w.fail(i, "Renew %s renewed %d, model %d", s, n, want)
		}
		w.trace = append(w.trace, fmt.Sprintf("renew %s %d", s, n))
	case op < 70: // release one claim of the session
		for _, id := range w.sortedIDs() {
			if w.model[id].session == s {
				if !w.set.Release(id) {
					w.fail(i, "Release %s returned false", id)
				}
				delete(w.model, id)
				w.trace = append(w.trace, "release "+id)
				return
			}
		}
	case op < 75: // session ends
		got := w.set.ReleaseSession(s)
		for _, c := range got {
			delete(w.model, c.ID)
		}
		for id, m := range w.model {
			if m.session == s {
				w.fail(i, "ReleaseSession %s left %s", s, id)
			}
		}
		w.trace = append(w.trace, fmt.Sprintf("end %s %d", s, len(got)))
	case op < 82: // disconnect or sleep: no renewals for 1 to 10 minutes
		w.offline[s] = w.now.Add(time.Duration(1+w.rng.Intn(10)) * time.Minute)
		w.trace = append(w.trace, "disconnect "+s)
	case op < 90: // periodic expiry sweep
		for _, c := range w.set.Expire(w.now) {
			m, ok := w.model[c.ID]
			if !ok || w.now.Before(m.expires) {
				w.fail(i, "Expire removed %s which the model holds active until %v", c.ID, m.expires)
			}
			delete(w.model, c.ID)
		}
		w.trace = append(w.trace, "expire")
	default: // an edit through the hook
		p := w.pick(patterns)
		d := w.set.Decide(s, []string{p}, w.now)
		if d.Allow == w.modelConflict(p, s) {
			w.fail(i, "Decide %s %q allow=%v, model conflict=%v", s, p, d.Allow, !d.Allow)
		}
		w.trace = append(w.trace, fmt.Sprintf("edit %s %s %v", s, p, d.Allow))
	}
}

// modelConflict reports whether pattern overlaps an active model claim of
// another session, using only the model and claims.Overlaps.
func (w *world) modelConflict(pattern, session string) bool {
	for _, m := range w.model {
		if m.session != session && w.now.Before(m.expires) && claims.Overlaps(pattern, m.pattern) {
			return true
		}
	}
	return false
}

func (w *world) sortedIDs() []string {
	ids := make([]string, 0, len(w.model))
	for id := range w.model {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// check verifies the invariants after a step.
func (w *world) check(i int) {
	active := w.set.Active(w.now)
	// The active set equals the model's unexpired, unreleased claims.
	want := 0
	for _, m := range w.model {
		if w.now.Before(m.expires) {
			want++
		}
	}
	if len(active) != want {
		w.fail(i, "Active has %d claims, model %d", len(active), want)
	}
	for _, c := range active {
		m, ok := w.model[c.ID]
		if !ok {
			w.fail(i, "released claim %s is active again", c.ID)
		}
		if !c.LeaseExpires.Equal(m.expires) {
			w.fail(i, "claim %s expires %v, model %v", c.ID, c.LeaseExpires, m.expires)
		}
	}
	// No two sessions hold overlapping active claims.
	for a := range active {
		for b := a + 1; b < len(active); b++ {
			x, y := active[a], active[b]
			if x.Holder.SessionID != y.Holder.SessionID && claims.Overlaps(x.Pattern, y.Pattern) {
				w.fail(i, "active claims overlap across sessions: %s %q (%s) and %s %q (%s)",
					x.ID, x.Pattern, x.Holder.SessionID, y.ID, y.Pattern, y.Holder.SessionID)
			}
		}
	}
}

func run(t *testing.T, seed int64, steps int) uint64 {
	w := newWorld(t, seed)
	for i := range steps {
		w.step(i)
		w.check(i)
	}
	h := fnv.New64a()
	counts := map[string]int{}
	for _, line := range w.trace {
		_, _ = h.Write([]byte(line + "\n")) // hash.Hash.Write never returns an error
		counts[outcome(line)]++
	}
	t.Logf("seed %d: %v", seed, counts)
	return h.Sum64()
}

// outcome classifies a trace line for the per-seed summary.
func outcome(line string) string {
	f := strings.Fields(line)
	ok := f[len(f)-1] == "true"
	switch {
	case f[0] == "add" && ok:
		return "add_ok"
	case f[0] == "add":
		return "add_conflict"
	case f[0] == "edit" && ok:
		return "edit_allowed"
	case f[0] == "edit":
		return "edit_denied"
	}
	return f[0]
}

func TestSimClaims(t *testing.T) {
	seeds := []int64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597}
	if *seedFlag != 0 {
		seeds = []int64{*seedFlag}
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { run(t, seed, *stepsFlag) })
	}
}

func TestSimDeterministic(t *testing.T) {
	if a, b := run(t, 42, 2000), run(t, 42, 2000); a != b {
		t.Fatalf("same seed produced different traces: %x vs %x", a, b)
	}
	if a, b := run(t, 42, 2000), run(t, 43, 2000); a == b {
		t.Fatal("different seeds produced identical traces")
	}
}
