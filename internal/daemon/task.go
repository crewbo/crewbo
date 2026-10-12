package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/gitx"
	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// Task lifetimes: how long a teammate has to approve before it expires.
const (
	DefaultTaskTTL = 24 * time.Hour
	MaxTaskTTL     = 7 * 24 * time.Hour
)

// createTask builds a task for a repository in a room, signs it with this
// device's key and sends it. It runs on the connection goroutine.
func (d *Daemon) createTask(ctx context.Context, req Request) Response {
	fail := func(format string, a ...any) Response {
		return Response{ID: req.ID, Error: fmt.Sprintf(format, a...)}
	}
	if d.sync == nil {
		return fail("task_create: the daemon is not ready")
	}
	b, deviceKey, login, ok := d.sync.binding(req.Repo)
	if !ok {
		return fail("this repository is not in a room on the server you are signed in to; run crewbo room create or join")
	}
	if req.Assignee == "" {
		return fail("task_create: assignee is required")
	}
	ttl := DefaultTaskTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if ttl > MaxTaskTTL {
		return fail("a task can stay open for at most %s", MaxTaskTTL)
	}
	key, err := keys.Load(d.keyring(), d.cfg.Paths.Data, keys.Timeout)
	if err != nil {
		return fail("load this device's key: %v", err)
	}
	if key.ID() != deviceKey {
		return fail("this device's key is not the one registered at sign-in; run crewbo login again")
	}
	head, err := gitx.New("").Head(ctx, req.Repo)
	if err != nil {
		return fail("read the repository's current commit: %v", err)
	}
	id, err := dispatch.NewTaskID()
	if err != nil {
		return fail("task id: %v", err)
	}
	now := d.cfg.Now()
	body := &crewbov1.TaskBody{
		TaskId: id, RoomId: b.RoomID, AuthorKeyId: key.ID(), AuthorMemberId: login,
		AssigneeMemberId: req.Assignee, Text: req.Text, OwnedPaths: req.Patterns, BaseCommit: head,
		CreatedUnixMs: now.UnixMilli(), ExpiresUnixMs: now.Add(ttl).UnixMilli(),
	}
	if err := dispatch.WellFormed(body); err != nil {
		return fail("%v", err)
	}
	st, err := dispatch.Sign(key, body)
	if err != nil {
		return fail("sign the task: %v", err)
	}
	env := &crewbov1.Envelope{Id: newEnvelopeID(), Body: &crewbov1.Envelope_DispatchTask{DispatchTask: &crewbov1.DispatchTask{RoomId: b.RoomID, Task: st}}}
	reply, err := d.sync.submit(ctx, env, replyWait)
	resp := Response{ID: req.ID, TaskID: id}
	switch {
	case errors.Is(err, errNotSent):
		resp.Note = fmt.Sprintf("Signed and queued; %s gets it when the sync server answers.", req.Assignee)
	case err != nil:
		return fail("send the task: %v", err)
	case reply.GetError() != nil:
		return fail("the sync server refused the task: %s", reply.GetError().GetMessage())
	default:
		resp.Note = fmt.Sprintf("Sent to %s in room %s.", req.Assignee, b.Name)
	}
	d.cfg.Log.Info("task sent", "task_id", id, "room", b.Name, "assignee", req.Assignee) // never the text
	return resp
}

// keyring is the keychain the daemon uses for this device's key.
func (d *Daemon) keyring() keys.Keyring {
	if d.cfg.Keyring != nil {
		return d.cfg.Keyring
	}
	return keys.OSKeyring{}
}
