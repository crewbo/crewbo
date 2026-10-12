package roomstate

import (
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

func TestApplyIsIdempotentAndShiftsTime(t *testing.T) {
	server := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	// A daemon whose clock runs 90 s behind the server.
	skew := -90 * time.Second
	local := func(ms int64) time.Time { return time.UnixMilli(ms).Add(skew) }
	now := server.Add(skew)
	c := &crewbov1.Claim{Id: "c1", RoomId: "r", Pattern: "src/a.go", SessionId: "dev/terminal", MemberId: "RM1338", TaskId: "12", LeaseExpiresUnixMs: server.Add(time.Hour).UnixMilli()}
	events := []*crewbov1.Event{
		{Body: &crewbov1.Event_ClaimsGranted{ClaimsGranted: &crewbov1.ClaimsGranted{Claims: []*crewbov1.Claim{c}}}},
		{Body: &crewbov1.Event_ClaimsRenewed{ClaimsRenewed: &crewbov1.ClaimsRenewed{SessionId: "dev/terminal", LeaseExpiresUnixMs: server.Add(2 * time.Hour).UnixMilli()}}},
		{Body: &crewbov1.Event_ClaimsRenewed{ClaimsRenewed: &crewbov1.ClaimsRenewed{SessionId: "dev/terminal", LeaseExpiresUnixMs: server.Add(time.Minute).UnixMilli()}}},
	}
	set := claims.NewSet()
	for range 2 { // replaying everything twice changes nothing
		for _, ev := range events {
			Apply(set, ev, local, now)
		}
	}
	got, ok := set.Get("c1")
	want := claims.Claim{ID: "c1", Pattern: "src/a.go", Holder: claims.Holder{SessionID: "dev/terminal", MemberID: "RM1338"}, TaskID: "12", LeaseExpires: server.Add(2 * time.Hour).Add(skew)}
	gotLease, wantLease := got.LeaseExpires, want.LeaseExpires
	got.LeaseExpires, want.LeaseExpires = time.Time{}, time.Time{}
	if !ok || got != want || !gotLease.Equal(wantLease) {
		t.Fatalf("claim = %+v (lease %v), want %+v (lease %v)", got, gotLease, want, wantLease)
	}
	if back := ToProto(FromProto(c, ServerClock), "r"); back.GetLeaseExpiresUnixMs() != c.GetLeaseExpiresUnixMs() || back.GetSessionId() != c.GetSessionId() || back.GetMemberId() != "RM1338" {
		t.Errorf("round trip = %v", back)
	}
	for _, ev := range []*crewbov1.Event{
		{Body: &crewbov1.Event_ClaimsReleased{ClaimsReleased: &crewbov1.ClaimsReleased{ClaimIds: []string{"c1"}}}},
		{Body: &crewbov1.Event_ClaimsExpired{ClaimsExpired: &crewbov1.ClaimsExpired{ClaimIds: []string{"c1", "unknown"}}}},
	} {
		s := claims.NewSet()
		Apply(s, events[0], local, now)
		Apply(s, ev, local, now)
		Apply(s, ev, local, now)
		if _, ok := s.Get("c1"); ok {
			t.Errorf("%T left the claim", ev.GetBody())
		}
	}
}
