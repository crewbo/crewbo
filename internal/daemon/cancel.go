package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/crewbo/crewbo/internal/dispatch"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// cancelTask answers crewbo task cancel (M5-09; data-model.md: a running
// or checkpointed task can be cancelled by the lead or the teammate). The
// assignee's daemon stops the agent and releases the task's claims; the
// worktree and branch stay, so no work is lost. The lead who sent a task
// cancels it in the room, and the assignee's daemon follows.
func (d *Daemon) cancelTask(ctx context.Context, req Request) Response {
	fail := func(format string, a ...any) Response {
		return Response{ID: req.ID, Error: fmt.Sprintf(format, a...)}
	}
	if d.sync == nil {
		return fail("task_cancel: the daemon is not ready")
	}
	var room, state string
	var signed []byte
	err := d.sync.db.QueryRowContext(ctx, `SELECT room_id, state, signed FROM tasks WHERE task_id = ?`, req.Task).Scan(&room, &state, &signed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fail("no task %s", req.Task)
	case err != nil:
		return fail("task_cancel: %v", err)
	}
	refused := func(state string) Response {
		return fail("task %s is %s; only a running or checkpointed task can be cancelled", req.Task, state)
	}
	note := ""
	if state == taskOther {
		// A task this member sent: cancel it as its author.
		sts, err := d.taskStatuses(ctx, room, req.Task)
		if err != nil {
			return fail("task_cancel: %v", err)
		}
		last := dispatch.Fold(sentTo(signed), taskAuthor(signed), sts)
		if !dispatch.CanMove(last.State, dispatch.StateCancelled) {
			return refused(last.State)
		}
		note = sentTo(signed) + "'s daemon stops its agent and releases its claims."
	} else {
		if !d.stopTask(ctx, req.Task, req.Text) {
			return refused(state)
		}
		note = "Its work stays in " + d.taskWorktree(ctx, req.Task) + " on its branch."
	}
	reply, err := d.sendStatus(ctx, room, req.Task, crewbov1.TaskState_TASK_STATE_CANCELLED, truncate(req.Text, maxReason), replyWait)
	switch {
	case errors.Is(err, errNotSent):
		note += " The room hears when the sync server answers."
	case err != nil:
		return fail("cancelled here, but telling the room failed: %v", err)
	case reply.GetError() != nil:
		return fail("the sync server said: %s", reply.GetError().GetMessage())
	}
	return Response{ID: req.ID, TaskID: req.Task, Note: note}
}

// stopTask moves this member's task to cancelled if the state machine
// allows it, stops its agent and releases its claims. A dispatched run's
// supervisor releases its claims when the agent exits; an own task's
// claims belong to its session.
func (d *Daemon) stopTask(ctx context.Context, id, detail string) bool {
	if !d.setTaskState(ctx, id, dispatch.StateCancelled, detail) {
		return false
	}
	d.runsMu.Lock()
	stop := d.stops[id]
	d.runsMu.Unlock()
	if stop != nil {
		stop()
	}
	if r, _, session, err := d.loadOwnTask(ctx, id); err == nil {
		d.releaseOwnClaims(ctx, r, session)
	}
	d.cfg.Log.Info("task cancelled", "task_id", id)
	return true
}
