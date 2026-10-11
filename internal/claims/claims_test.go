package claims

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func claim(id, pattern, session string) Claim { return claimAt(id, pattern, session, t0) }

// claimAt returns a claim whose lease starts at start.
func claimAt(id, pattern, session string, start time.Time) Claim {
	return Claim{ID: id, Pattern: pattern, Holder: Holder{SessionID: session, MemberID: "m-" + session}, LeaseExpires: start.Add(DefaultLease)}
}

func TestAdd(t *testing.T) {
	s := NewSet()
	if err := s.Add(claim("c1", "src/auth.go", "alice"), t0); err != nil {
		t.Fatal(err)
	}
	// Same session may overlap its own claims.
	if err := s.Add(claim("c2", "src/*.go", "alice"), t0); err != nil {
		t.Fatalf("same-session overlap: %v", err)
	}
	// Another session conflicts and learns who holds the path.
	err := s.Add(claim("c3", "src/**", "bob"), t0)
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Existing.ID != "c1" {
		t.Fatalf("err = %v, want ConflictError with c1", err)
	}
	if !strings.Contains(ce.Error(), "alice") {
		t.Errorf("error %q does not name the holder session", ce.Error())
	}
	if err := s.Add(claim("c4", "lib/x.go", "bob"), t0); err != nil {
		t.Fatalf("non-overlapping: %v", err)
	}
	if err := s.Add(claim("c1", "other.go", "alice"), t0); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("duplicate id err = %v", err)
	}
	if err := s.Add(claim("c5", "../x", "bob"), t0); !errors.Is(err, ErrBadPattern) {
		t.Errorf("bad pattern err = %v", err)
	}
	if err := s.Add(claim("c6", "y.go", ""), t0); !errors.Is(err, ErrNoSession) {
		t.Errorf("no session err = %v", err)
	}
	if got := len(s.Active(t0)); got != 3 {
		t.Errorf("active = %d, want 3", got)
	}
}

func TestLeaseLifecycle(t *testing.T) {
	s := NewSet()
	if err := s.Add(claim("c1", "a.go", "alice"), t0); err != nil {
		t.Fatal(err)
	}
	// Renewal before expiry pushes the lease out.
	t1 := t0.Add(4 * time.Minute)
	if n := s.Renew("alice", t1, DefaultLease); n != 1 {
		t.Fatalf("renewed %d, want 1", n)
	}
	t2 := t0.Add(8 * time.Minute) // past the original expiry, inside the renewed one
	if d := s.Decide("bob", []string{"a.go"}, t2); d.Allow {
		t.Fatal("renewed claim should still block bob")
	}
	// Without renewal the claim stops blocking exactly at expiry.
	t3 := t1.Add(DefaultLease)
	if d := s.Decide("bob", []string{"a.go"}, t3); !d.Allow {
		t.Fatal("expired claim should not block")
	}
	if n := s.Renew("alice", t3, DefaultLease); n != 0 {
		t.Errorf("expired claim was renewed (%d)", n)
	}
	// Another session may claim the path once the lease has run out.
	if err := s.Add(claim("c2", "a.go", "bob"), t3); !errors.Is(err, ErrExpired) {
		t.Errorf("already-expired lease err = %v, want ErrExpired", err)
	}
	if err := s.Add(claimAt("c2", "a.go", "bob", t3), t3); err != nil {
		t.Fatalf("claim after expiry: %v", err)
	}
	expired := s.Expire(t3)
	if len(expired) != 1 || expired[0].ID != "c1" {
		t.Errorf("expired = %+v, want c1", expired)
	}
}

func TestRelease(t *testing.T) {
	s := NewSet()
	for _, c := range []Claim{claim("c1", "a.go", "alice"), claim("c2", "b.go", "alice"), claim("c3", "c.go", "bob")} {
		if err := s.Add(c, t0); err != nil {
			t.Fatal(err)
		}
	}
	if !s.Release("c3") || s.Release("c3") {
		t.Error("Release should report true once, then false")
	}
	got := s.ReleaseSession("alice")
	if len(got) != 2 || got[0].ID != "c1" || got[1].ID != "c2" {
		t.Errorf("ReleaseSession = %+v", got)
	}
	if len(s.Active(t0)) != 0 {
		t.Error("set should be empty")
	}
}

func TestDecide(t *testing.T) {
	s := NewSet()
	if err := s.Add(claim("c1", "src/auth.go", "alice"), t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(claim("c2", "web/**", "carol"), t0); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		session   string
		paths     []string
		allow     bool
		blockedBy string
	}{
		{"holder edits own file", "alice", []string{"src/auth.go"}, true, ""},
		{"free file", "bob", []string{"src/limits.go"}, true, ""},
		{"claimed file", "bob", []string{"src/limits.go", "src/auth.go"}, false, "c1"},
		{"case-only difference", "bob", []string{"SRC/Auth.go"}, false, "c1"},
		{"inside claimed glob", "bob", []string{"web/app/index.ts"}, false, "c2"},
		{"no paths", "bob", nil, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := s.Decide(tt.session, tt.paths, t0)
			if d.Allow != tt.allow || d.BlockedBy.ID != tt.blockedBy {
				t.Errorf("Decide = %+v, want allow=%v blockedBy=%q", d, tt.allow, tt.blockedBy)
			}
		})
	}
}
