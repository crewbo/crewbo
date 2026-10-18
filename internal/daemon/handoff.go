package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/claims"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// The escalation ladder (M5-03, docs/architecture/flows.md flow 4): a
// member blocked by a claim asks its holder to hand it over; the holder
// approves (the server releases the claim) or declines; the room lead can
// release it with a reason. The server enforces who may do what; the
// daemon records every step for `crewbo claim requests`.

// recordHandoff stores a HandoffRequested, HandoffAnswered or
// ClaimOverridden event. The claim's path and holder come from this
// machine's view of the room, which still has the claim: the release an
// approval or override causes is the next event.
func (d *Daemon) recordHandoff(ctx context.Context, b auth.Binding, ev *crewbov1.Event) error {
	at := ev.GetServerTimeUnixMs()
	if a := ev.GetHandoffAnswered(); a != nil {
		answer := "declined"
		if a.GetApprove() {
			answer = "approved"
		}
		_, err := d.sync.db.ExecContext(ctx, `UPDATE handoff SET answer = ?, answer_reason = ? WHERE room_id = ? AND request_id = ? AND kind = 'request'`,
			answer, a.GetReason(), b.RoomID, a.GetRequestId())
		return err
	}
	kind, requestID, claimID, reason := "request", "", "", ""
	if h := ev.GetHandoffRequested(); h != nil {
		requestID, claimID, reason = h.GetRequestId(), h.GetClaimId(), h.GetReason()
	} else {
		o := ev.GetClaimOverridden()
		kind, claimID, reason = "override", o.GetClaimId(), o.GetReason()
	}
	var c claims.Claim
	if err := d.onOwner(ctx, func() { c, _ = d.set(b.Repo).Get(claimID) }); err != nil {
		return err
	}
	_, err := d.sync.db.ExecContext(ctx, `INSERT INTO handoff (room_id, seq, kind, request_id, claim_id, path, holder, actor, reason, at_unix_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		b.RoomID, ev.GetSeq(), kind, requestID, claimID, c.Pattern, c.Holder.MemberID, ev.GetActorMemberId(), reason, at)
	return err
}

// errNoRoom is a handoff command in a repository that is not in a room:
// its claims are this machine's own, so nobody else holds them.
var errNoRoom = errors.New("this repository is not in a room; claims here are local, so release them with `crewbo claim release`")

// handoff runs claim_request, claim_requests, claim_handoff and
// claim_override on the connection goroutine.
func (d *Daemon) handoff(ctx context.Context, req Request) Response {
	fail := func(format string, a ...any) Response {
		return Response{ID: req.ID, Error: fmt.Sprintf(format, a...)}
	}
	if d.sync == nil {
		return fail("%s: the daemon is not ready", req.Method)
	}
	b, _, login, ok := d.sync.binding(req.Repo)
	if !ok {
		return fail("%v", errNoRoom)
	}
	if req.Method == MethodClaimRequests {
		hs, err := d.listHandoffs(ctx, b)
		if err != nil {
			return fail("claim_requests: %v", err)
		}
		return Response{ID: req.ID, Handoffs: hs}
	}

	var env *crewbov1.Envelope
	var sent Handoff
	switch req.Method {
	case MethodClaimHandoff:
		if len(req.IDs) != 1 {
			return fail("claim_handoff: one request id is required")
		}
		env = &crewbov1.Envelope{Body: &crewbov1.Envelope_HandoffAnswer{HandoffAnswer: &crewbov1.HandoffAnswer{
			RoomId: b.RoomID, RequestId: req.IDs[0], Approve: req.Approve, Reason: req.Text,
		}}}
	case MethodClaimRequest, MethodClaimOverride:
		if len(req.Paths) != 1 {
			return fail("%s: one path is required", req.Method)
		}
		c, found, err := d.claimOn(ctx, req.Repo, req.Paths[0], login, req.Method == MethodClaimOverride)
		switch {
		case err != nil:
			return fail("%s: %v", req.Method, err)
		case !found && req.Method == MethodClaimOverride:
			return fail("nobody holds a claim on %s", req.Paths[0])
		case !found:
			return fail("nobody else holds a claim on %s", req.Paths[0])
		}
		sent = Handoff{ClaimID: c.ID, Path: c.Pattern, Holder: c.Holder.MemberID, Actor: login, Reason: req.Text, At: d.cfg.Now()}
		if req.Method == MethodClaimOverride {
			sent.Kind = "override"
			env = &crewbov1.Envelope{Body: &crewbov1.Envelope_LeadOverride{LeadOverride: &crewbov1.LeadOverride{
				RoomId: b.RoomID, ClaimId: c.ID, Reason: req.Text,
			}}}
		} else {
			sent.Kind, sent.RequestID, sent.Open = "request", "r-"+strings.TrimPrefix(newClaimID(), "c-"), true
			env = &crewbov1.Envelope{Body: &crewbov1.Envelope_HandoffRequest{HandoffRequest: &crewbov1.HandoffRequest{
				RoomId: b.RoomID, RequestId: sent.RequestID, ClaimId: c.ID, Reason: req.Text,
			}}}
		}
	default:
		return fail("unknown method %q", req.Method)
	}
	env.Id = newEnvelopeID()
	reply, err := d.sync.submit(ctx, env, replyWait)
	resp := Response{ID: req.ID}
	if sent.ClaimID != "" {
		resp.Handoffs = []Handoff{sent}
	}
	switch {
	case errors.Is(err, errNotSent):
		resp.Note = fmt.Sprintf("Queued; room %s will get it when the sync server answers.", b.Name)
	case err != nil:
		return fail("sending it to room %s failed: %v", b.Name, err)
	case reply.GetError() != nil:
		return fail("%s", reply.GetError().GetMessage())
	default:
		resp.Note = fmt.Sprintf("Shared with room %s.", b.Name)
	}
	return resp
}

// claimOn returns the active claim covering pattern held by another
// member than login, or, for a lead override, by anyone.
func (d *Daemon) claimOn(ctx context.Context, repo, pattern, login string, anyone bool) (claims.Claim, bool, error) {
	var out claims.Claim
	var found bool
	err := d.onOwner(ctx, func() {
		for _, c := range d.set(repo).Active(d.cfg.Now()) {
			if claims.Overlaps(pattern, c.Pattern) && (anyone || c.Holder.MemberID != login) {
				out, found = c, true
				return
			}
		}
	})
	return out, found, err
}

// maxHandoffs is how many ladder steps `crewbo claim requests` shows.
const maxHandoffs = 50

// listHandoffs returns the room's latest ladder steps, newest first. A
// request is open until answered or until its claim is gone.
func (d *Daemon) listHandoffs(ctx context.Context, b auth.Binding) ([]Handoff, error) {
	rows, err := d.sync.db.QueryContext(ctx, `SELECT kind, request_id, claim_id, path, holder, actor, reason, answer, answer_reason, at_unix_ms
		FROM handoff WHERE room_id = ? ORDER BY seq DESC LIMIT ?`, b.RoomID, maxHandoffs)
	if err != nil {
		return nil, err
	}
	var out []Handoff
	for rows.Next() {
		var h Handoff
		var at int64
		if err := rows.Scan(&h.Kind, &h.RequestID, &h.ClaimID, &h.Path, &h.Holder, &h.Actor, &h.Reason, &h.Answer, &h.AnswerReason, &at); err != nil {
			_ = rows.Close()
			return nil, err
		}
		h.At = time.UnixMilli(at)
		out = append(out, h)
	}
	// Close before the next query: the store has one connection.
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	err = d.onOwner(ctx, func() {
		set := d.set(b.Repo)
		for i, h := range out {
			_, held := set.Get(h.ClaimID)
			out[i].Open = h.Kind == "request" && h.Answer == "" && held
		}
	})
	return out, err
}
