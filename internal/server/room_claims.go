package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/roomstate"
)

// Claim request limits (protocol.md size limits; lease bounds match the
// daemon's terminal claims).
const (
	maxPatterns  = 200
	maxClaimID   = 64
	maxLease     = 8 * time.Hour
	minLease     = time.Minute
	renewalLease = claims.DefaultLease
	maxTaskID    = 64
	maxDetail    = 512
	maxRef       = 255
	maxCommit    = 64 // SHA-256 object ids in hex
	maxPath      = 4096
)

// reportable reports whether a member may send state in a TaskStatus:
// every state but the ones the log already implies (dispatched) or none.
func reportable(state crewbov1.TaskState) bool {
	_, known := crewbov1.TaskState_name[int32(state)]
	return known && state != crewbov1.TaskState_TASK_STATE_UNSPECIFIED && state != crewbov1.TaskState_TASK_STATE_DISPATCHED
}

// Caller is who sends a request: the member from the session and the
// device key presented in Hello.
type Caller struct {
	Member    string // GitHub login
	DeviceKey string // empty when the daemon presented none
}

// ownsSession reports whether a session id belongs to the caller's device.
// Daemons prefix every session id with their device key id, so terminal
// sessions on two machines never look like one, and nobody can act for a
// session on someone else's device.
func (c Caller) ownsSession(session string) bool {
	return c.DeviceKey != "" && strings.HasPrefix(session, c.DeviceKey+"/") && len(session) > len(c.DeviceKey)+1
}

// Handle answers one claim request with an Ack or an Error. It runs on the
// room goroutine, so requests are decided one at a time against the
// current claims.
func (r *Room) Handle(ctx context.Context, caller Caller, env *crewbov1.Envelope) *crewbov1.Envelope {
	var reply *crewbov1.Envelope
	if err := r.do(ctx.Done(), func() { reply = r.handle(ctx, caller, env) }); err != nil {
		return errorEnvelope(env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNSPECIFIED, "the room is not available; try again")
	}
	return reply
}

func ack(id string, seq uint64) *crewbov1.Envelope {
	return &crewbov1.Envelope{Body: &crewbov1.Envelope_Ack{Ack: &crewbov1.Ack{RequestId: id, Seq: seq}}}
}

func (r *Room) handle(ctx context.Context, caller Caller, env *crewbov1.Envelope) *crewbov1.Envelope {
	id := env.GetId()
	invalid := func(format string, a ...any) *crewbov1.Envelope {
		return errorEnvelope(id, crewbov1.ErrorCode_ERROR_CODE_INVALID, fmt.Sprintf(format, a...))
	}
	now := r.now()
	var ev *crewbov1.Event
	switch b := env.GetBody().(type) {
	case *crewbov1.Envelope_ClaimRequest:
		req := b.ClaimRequest
		switch {
		case !caller.ownsSession(req.GetSessionId()):
			return invalid("session %q does not belong to this device; sign in again with crewbo login", req.GetSessionId())
		case len(req.GetPatterns()) == 0 || len(req.GetPatterns()) > maxPatterns:
			return invalid("a request claims 1 to %d paths", maxPatterns)
		case len(req.GetClaimIds()) != len(req.GetPatterns()):
			return invalid("one claim id per path is required")
		}
		existing := 0
		for i, p := range req.GetPatterns() {
			cid := req.GetClaimIds()[i]
			if cid == "" || len(cid) > maxClaimID {
				return invalid("claim ids are 1 to %d bytes", maxClaimID)
			}
			if err := claims.ValidatePattern(p); err != nil {
				return invalid("%v", err)
			}
			if c, ok := r.claims.Get(cid); ok {
				if c.Holder.SessionID != req.GetSessionId() || c.Pattern != p {
					return invalid("claim id %s is already used", cid)
				}
				existing++
			}
		}
		if existing == len(req.GetPatterns()) {
			return ack(id, 0) // a resent request that was already granted
		}
		if existing != 0 {
			return invalid("some of these claim ids are already granted")
		}
		if dec := r.claims.Decide(req.GetSessionId(), req.GetPatterns(), now); !dec.Allow {
			e := errorEnvelope(id, crewbov1.ErrorCode_ERROR_CODE_CONFLICT,
				fmt.Sprintf("%s is claimed by %s", dec.Path, dec.BlockedBy.Holder.MemberID))
			e.GetError().BlockingClaims = []*crewbov1.Claim{roomstate.ToProto(dec.BlockedBy, r.id)}
			return e
		}
		lease := min(max(time.Duration(req.GetLeaseSeconds())*time.Second, minLease), maxLease)
		if req.GetLeaseSeconds() == 0 {
			lease = claims.DefaultLease
		}
		granted := &crewbov1.ClaimsGranted{}
		for i, p := range req.GetPatterns() {
			granted.Claims = append(granted.Claims, roomstate.ToProto(claims.Claim{
				ID: req.GetClaimIds()[i], Pattern: p, TaskID: req.GetTaskId(), LeaseExpires: now.Add(lease),
				Holder: claims.Holder{SessionID: req.GetSessionId(), MemberID: caller.Member},
			}, r.id))
		}
		ev = &crewbov1.Event{Body: &crewbov1.Event_ClaimsGranted{ClaimsGranted: granted}}

	case *crewbov1.Envelope_ClaimRelease:
		var ids []string
		for _, cid := range b.ClaimRelease.GetClaimIds() {
			c, ok := r.claims.Get(cid)
			if !ok {
				continue // already released or expired
			}
			if c.Holder.MemberID != caller.Member {
				return invalid("claim %s is held by %s; only they can release it", cid, c.Holder.MemberID)
			}
			ids = append(ids, cid)
		}
		if len(ids) == 0 {
			return ack(id, 0)
		}
		ev = &crewbov1.Event{Body: &crewbov1.Event_ClaimsReleased{ClaimsReleased: &crewbov1.ClaimsReleased{ClaimIds: ids}}}

	case *crewbov1.Envelope_ClaimRenew:
		session := b.ClaimRenew.GetSessionId()
		if !caller.ownsSession(session) {
			return invalid("session %q does not belong to this device", session)
		}
		until := now.Add(renewalLease)
		changes := false
		for _, c := range r.claims.Active(now) {
			if c.Holder.SessionID == session && c.LeaseExpires.Before(until) {
				changes = true
				break
			}
		}
		if !changes {
			return ack(id, 0) // nothing to extend: keep the log quiet
		}
		ev = &crewbov1.Event{Body: &crewbov1.Event_ClaimsRenewed{ClaimsRenewed: &crewbov1.ClaimsRenewed{SessionId: session, LeaseExpiresUnixMs: until.UnixMilli()}}}

	case *crewbov1.Envelope_DispatchTask:
		// Admitted by Server.checkDispatch (lead, device key, signature).
		// A resent request is recorded again; receiving daemons ignore a
		// task id they already have.
		ev = &crewbov1.Event{Body: &crewbov1.Event_TaskDispatched{TaskDispatched: &crewbov1.TaskDispatched{Task: b.DispatchTask.GetTask()}}}

	case *crewbov1.Envelope_TaskStatus:
		// Recorded with the sender as actor. The server does not decode
		// tasks, so it cannot tell the assignee; daemons trust a status
		// only from the task's assignee (or its author).
		st := b.TaskStatus
		switch {
		case st.GetTaskId() == "" || len(st.GetTaskId()) > maxTaskID:
			return invalid("task ids are 1 to %d bytes", maxTaskID)
		case !reportable(st.GetState()):
			return invalid("%v is not a state a member reports", st.GetState())
		case len(st.GetDetail()) > maxDetail:
			return invalid("the detail is at most %d bytes", maxDetail)
		case len(st.GetRef()) > maxRef || len(st.GetCommit()) > maxCommit:
			return invalid("a ref is at most %d bytes and a commit %d", maxRef, maxCommit)
		}
		ev = &crewbov1.Event{Body: &crewbov1.Event_TaskStatusChanged{TaskStatusChanged: &crewbov1.TaskStatusChanged{Status: st}}}

	case *crewbov1.Envelope_ConflictWarning:
		// Recorded with the sender as actor, like a task status: it holds
		// paths and commit ids only, never file contents.
		w := b.ConflictWarning
		switch {
		case w.GetTaskId() == "" || len(w.GetTaskId()) > maxTaskID || w.GetOtherTaskId() == "" || len(w.GetOtherTaskId()) > maxTaskID:
			return invalid("task ids are 1 to %d bytes", maxTaskID)
		case len(w.GetCommit()) > maxCommit || len(w.GetOtherCommit()) > maxCommit:
			return invalid("a commit is at most %d bytes", maxCommit)
		case len(w.GetPaths()) > maxPatterns:
			return invalid("a warning names at most %d paths", maxPatterns)
		}
		for _, p := range w.GetPaths() {
			if p == "" || len(p) > maxPath {
				return invalid("paths are 1 to %d bytes", maxPath)
			}
		}
		ev = &crewbov1.Event{Body: &crewbov1.Event_ConflictWarning{ConflictWarning: w}}

	default:
		return errorEnvelope(id, crewbov1.ErrorCode_ERROR_CODE_UNSUPPORTED, "not supported by this server")
	}
	ev.ActorMemberId = caller.Member
	out, err := r.commit(ctx, ev)
	if err != nil {
		return errorEnvelope(id, crewbov1.ErrorCode_ERROR_CODE_UNSPECIFIED, "could not record the change; try again")
	}
	return ack(id, out.GetSeq())
}

// expire removes lapsed leases and records them, so daemons drop them too.
func (r *Room) expire(ctx context.Context) {
	gone := r.claims.Expire(r.now())
	if len(gone) == 0 {
		return
	}
	ids := make([]string, len(gone))
	for i, c := range gone {
		ids[i] = c.ID
	}
	// A failed append leaves the claims removed here but in no event;
	// daemons still stop honouring them once their lease time passes.
	_, _ = r.commit(ctx, &crewbov1.Event{Body: &crewbov1.Event_ClaimsExpired{ClaimsExpired: &crewbov1.ClaimsExpired{ClaimIds: ids}}})
}
