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
	"github.com/crewbo/crewbo/internal/roomstate"
)

// replyWait is how long `crewbo claim add` waits for the sync server to
// confirm a claim before reporting it as queued.
const replyWait = 5 * time.Second

// onOwner runs f on the owner goroutine and waits for it.
func (d *Daemon) onOwner(ctx context.Context, f func()) error {
	done := make(chan struct{})
	select {
	case d.funcs <- func() { f(); close(done) }:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-done
	return nil
}

// applyEvent records a room event: a dispatched task is verified and
// stored here, on the syncer goroutine, because that needs the network;
// anything else goes to the bound repository's claims on the owner.
func (d *Daemon) applyEvent(ctx context.Context, b auth.Binding, ev *crewbov1.Event, clock roomstate.Clock) error {
	if st := ev.GetTaskDispatched().GetTask(); st != nil {
		return d.receiveTask(ctx, b, st)
	}
	return d.onOwner(ctx, func() {
		roomstate.Apply(d.set(b.Repo), ev, clock, d.cfg.Now())
	})
}

// claimRefused drops claims the server turned down after the member
// stopped waiting, and counts them for `crewbo status`.
func (d *Daemon) claimRefused(ctx context.Context, b auth.Binding, ids []string, reason string) {
	d.cfg.Log.Warn("sync: the server refused a claim made earlier; it no longer applies", "room", b.Name, "reason", reason)
	_ = d.onOwner(ctx, func() {
		set := d.set(b.Repo)
		for _, id := range ids {
			set.Release(id)
		}
		d.refused += len(ids)
	})
}

// shared reports whether req is a claim change for a repository in a room,
// which goes through the sync server instead of staying local.
func (d *Daemon) shared(req Request) bool {
	if d.sync == nil || (req.Method != MethodClaimAdd && req.Method != MethodClaimRelease) {
		return false
	}
	_, _, _, ok := d.sync.binding(req.Repo)
	return ok
}

// sharedClaim handles claim add and release for a repository in a room. It
// runs on the connection goroutine, so waiting for the server never holds
// up the owner, and so hook decisions are never delayed.
func (d *Daemon) sharedClaim(ctx context.Context, req Request) Response {
	b, key, login, _ := d.sync.binding(req.Repo)
	call := func(r Request) Response {
		reply := make(chan Response, 1)
		select {
		case d.calls <- call{req: r, reply: reply}:
		case <-ctx.Done():
			return Response{ID: req.ID, Error: ctx.Err().Error()}
		}
		return <-reply
	}

	if req.Method == MethodClaimRelease {
		// Only this device's claims; others are released by their holders.
		req.OwnerPrefix = key + "/"
		resp := call(req)
		if resp.Error == "" && len(resp.ReleasedIDs) > 0 {
			env := &crewbov1.Envelope{Id: newEnvelopeID(), Body: &crewbov1.Envelope_ClaimRelease{ClaimRelease: &crewbov1.ClaimRelease{RoomId: b.RoomID, ClaimIds: resp.ReleasedIDs}}}
			reply, err := d.sync.submit(ctx, env, replyWait)
			switch {
			case errors.Is(err, errNotSent):
				resp.Note = fmt.Sprintf("Released here; room %s will hear when the sync server answers.", b.Name)
			case err != nil:
				resp.Note = fmt.Sprintf("Released here, but sharing it with room %s failed: %v", b.Name, err)
			case reply.GetError() != nil:
				resp.Note = fmt.Sprintf("Released here; the sync server said: %s", reply.GetError().GetMessage())
			default:
				resp.Note = fmt.Sprintf("Shared with room %s.", b.Name)
			}
		}
		return resp
	}

	// Grant locally first, so this machine's other agents are blocked at
	// once, with the session qualified by this device's key.
	req.Session = key + "/" + req.Session
	req.Member = login // the server records the GitHub login; match it here
	resp := call(req)
	if resp.Error != "" {
		return resp
	}
	cr := &crewbov1.ClaimRequest{RoomId: b.RoomID, SessionId: req.Session, TaskId: req.Task, LeaseSeconds: int64(time.Until(resp.Claims[0].LeaseExpires).Round(time.Second).Seconds())}
	for _, c := range resp.Claims {
		cr.Patterns = append(cr.Patterns, c.Pattern)
		cr.ClaimIds = append(cr.ClaimIds, c.ID)
	}
	reply, err := d.sync.submit(ctx, &crewbov1.Envelope{Id: newEnvelopeID(), Body: &crewbov1.Envelope_ClaimRequest{ClaimRequest: cr}}, replyWait)
	switch {
	case errors.Is(err, errNotSent):
		resp.Note = fmt.Sprintf("Claimed here; room %s will get it when the sync server answers.", b.Name)
		return resp
	case err != nil:
		resp.Note = fmt.Sprintf("Claimed here, but sharing it with room %s failed: %v", b.Name, err)
		return resp
	case reply.GetError() != nil:
		// Refused by the server: undo the local grant.
		call(Request{Method: MethodClaimRelease, Repo: req.Repo, IDs: cr.GetClaimIds()})
		out := Response{ID: req.ID, Error: reply.GetError().GetMessage()}
		if bc := reply.GetError().GetBlockingClaims(); len(bc) > 0 {
			blocked := roomstate.FromProto(bc[0], roomstate.ServerClock)
			out.BlockedBy, out.Path = &blocked, cr.GetPatterns()[0]
		}
		return out
	}
	resp.Note = fmt.Sprintf("Shared with room %s.", b.Name)
	return resp
}

func newEnvelopeID() string {
	return "e-" + strings.TrimPrefix(newClaimID(), "c-") + strings.TrimPrefix(newClaimID(), "c-")
}

// ownedBy reports whether a claim belongs to a session under prefix; an
// empty prefix owns everything (repositories not in a room).
func ownedBy(c claims.Claim, prefix string) bool {
	return prefix == "" || strings.HasPrefix(c.Holder.SessionID, prefix)
}
