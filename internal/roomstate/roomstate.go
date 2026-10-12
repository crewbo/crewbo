// Package roomstate turns a room's events into claim state. The sync
// server and every daemon apply the same events with the same rules, so
// they agree on who holds what (ADR 0003: claim state is a projection of
// the event log).
package roomstate

import (
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// Clock converts a server time (Unix milliseconds) to the local clock. The
// server uses time.UnixMilli; a daemon shifts by its measured offset.
type Clock func(serverUnixMs int64) time.Time

// ServerClock is the identity Clock used on the server.
func ServerClock(ms int64) time.Time { return time.UnixMilli(ms) }

// FromProto converts a wire claim, mapping its lease end with clock.
func FromProto(c *crewbov1.Claim, clock Clock) claims.Claim {
	return claims.Claim{
		ID:           c.GetId(),
		Pattern:      c.GetPattern(),
		Holder:       claims.Holder{SessionID: c.GetSessionId(), MemberID: c.GetMemberId()},
		TaskID:       c.GetTaskId(),
		LeaseExpires: clock(c.GetLeaseExpiresUnixMs()),
	}
}

// ToProto converts a claim held in server time to the wire form.
func ToProto(c claims.Claim, roomID string) *crewbov1.Claim {
	return &crewbov1.Claim{
		Id: c.ID, RoomId: roomID, Pattern: c.Pattern,
		SessionId: c.Holder.SessionID, MemberId: c.Holder.MemberID, TaskId: c.TaskID,
		LeaseExpiresUnixMs: c.LeaseExpires.UnixMilli(),
	}
}

// Apply changes set as ev records. It is idempotent, so replaying an event
// twice leaves the same state. Grants are recorded as given: the server
// checked overlaps before it committed them.
func Apply(set *claims.Set, ev *crewbov1.Event, clock Clock, now time.Time) {
	switch b := ev.GetBody().(type) {
	case *crewbov1.Event_ClaimsGranted:
		for _, c := range b.ClaimsGranted.GetClaims() {
			set.Put(FromProto(c, clock))
		}
	case *crewbov1.Event_ClaimsReleased:
		for _, id := range b.ClaimsReleased.GetClaimIds() {
			set.Release(id)
		}
	case *crewbov1.Event_ClaimsExpired:
		for _, id := range b.ClaimsExpired.GetClaimIds() {
			set.Release(id)
		}
	case *crewbov1.Event_ClaimsRenewed:
		set.Extend(b.ClaimsRenewed.GetSessionId(), clock(b.ClaimsRenewed.GetLeaseExpiresUnixMs()), now)
	}
}
