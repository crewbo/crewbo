package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/gitx"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// mergeAttempts bounds how often a merge re-checks after main moved.
const mergeAttempts = 3

// errConflict is a merge refused because of a conflict.
var errConflict = errors.New("merge refused")

// mergeTask merges a checkpointed task of this member into the remote's
// default branch (ADR 0014, ADR 0015): it checks the checkpoint against the
// current main and against every other task's latest checkpoint with git
// merge-tree, refuses a conflict with main, and a conflict with another
// task unless the member named that task in req.Despite, then writes the merge commit with
// commit-tree (no working tree is touched) and pushes it. A push refused
// because main moved is re-checked and retried. It runs on the connection
// goroutine.
func (d *Daemon) mergeTask(ctx context.Context, req Request) Response {
	fail := func(format string, a ...any) Response {
		return Response{ID: req.ID, Error: fmt.Sprintf(format, a...)}
	}
	if d.sync == nil {
		return fail("task_merge: the daemon is not ready")
	}
	var room, state string
	err := d.sync.db.QueryRowContext(ctx, `SELECT room_id, state FROM tasks WHERE task_id = ?`, req.Task).Scan(&room, &state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fail("no task %s", req.Task)
	case err != nil:
		return fail("task_merge: %v", err)
	case state == taskOther:
		return fail("you sent task %s; its assignee merges it", req.Task)
	case state != dispatch.StateCheckpointed:
		return fail("task %s is %s; only a checkpointed task can be merged", req.Task, state)
	}
	b, ok := d.sync.roomBinding(room)
	if !ok {
		return fail("no repository on this machine is in the task's room")
	}
	g := gitx.New("")
	commit, err := g.Head(ctx, filepath.Join(d.cfg.Paths.Data, "worktrees", req.Task))
	if err != nil {
		return fail("read the task's checkpoint: %v", err)
	}
	remote, err := g.PushRemote(ctx, b.Repo)
	if err != nil {
		return fail("%v", err)
	}
	branch, err := g.DefaultBranch(ctx, b.Repo, remote)
	if err != nil {
		return fail("find the main branch on %s: %v", remote, err)
	}
	if err := d.checkOthers(ctx, g, b.Repo, remote, room, req.Task, commit, req.Despite); err != nil {
		return fail("%v", err)
	}
	var merged string
	for attempt := 1; ; attempt++ {
		merged, err = d.mergeOnce(ctx, g, b.Repo, remote, branch, req.Task, commit)
		if err == nil || errors.Is(err, errConflict) || attempt == mergeAttempts {
			break
		}
		d.cfg.Log.Info("merge push refused; checking the moved branch again", "task_id", req.Task, "attempt", attempt, "err", err)
	}
	if err != nil {
		return fail("%v", err)
	}
	short := strings.TrimPrefix(branch, "refs/heads/")
	d.setTaskState(ctx, req.Task, dispatch.StateMerging, "")
	d.setTaskState(ctx, req.Task, dispatch.StateMerged, branch+" at "+merged[:12])
	for _, st := range []*crewbov1.TaskStatus{
		{RoomId: room, TaskId: req.Task, State: crewbov1.TaskState_TASK_STATE_MERGING},
		{RoomId: room, TaskId: req.Task, State: crewbov1.TaskState_TASK_STATE_MERGED, Detail: branch + " at " + merged[:12], Ref: branch, Commit: merged},
	} {
		if _, err := d.sync.submit(ctx, &crewbov1.Envelope{Id: newEnvelopeID(), Body: &crewbov1.Envelope_TaskStatus{TaskStatus: st}}, 0); err != nil && !errors.Is(err, errNotSent) {
			d.cfg.Log.Warn("task status not queued", "task_id", req.Task, "err", err)
		}
	}
	d.cfg.Log.Info("task merged", "task_id", req.Task, "branch", branch, "commit", merged[:12])
	return Response{ID: req.ID, TaskID: req.Task, Note: fmt.Sprintf("Merged into %s as %s and pushed to %s.", short, merged[:12], remote)}
}

// checkOthers refuses a merge that would conflict with another task's
// latest checkpoint, unless the member confirmed that task in despite
// (ADR 0015): the first to merge goes ahead, and the other task then
// conflicts with main until its owner updates it.
func (d *Daemon) checkOthers(ctx context.Context, g *gitx.Git, repo, remote, room, taskID, commit string, despite []string) error {
	others, err := d.otherCheckpoints(ctx, room, taskID)
	if err != nil {
		return err
	}
	var refused []string
	for _, o := range others {
		if err := g.Fetch(ctx, repo, remote, o.ref); err != nil {
			return fmt.Errorf("fetch task %s's checkpoint: %w", o.task, err)
		}
		res, err := g.MergeTree(ctx, repo, commit, o.commit)
		if err != nil {
			return fmt.Errorf("check against task %s: %w", o.task, err)
		}
		if !res.Clean && !slices.Contains(despite, o.task) {
			refused = append(refused, fmt.Sprintf("task %s on %s", o.task, strings.Join(res.Conflicts, ", ")))
			despite = append(despite, o.task) // for the hint below
		}
	}
	if len(refused) == 0 {
		return nil
	}
	var flags strings.Builder
	for _, o := range despite[len(despite)-len(refused):] {
		flags.WriteString(" --despite " + o)
	}
	return fmt.Errorf("%w: task %s conflicts with %s. Talk to them, then merge anyway with: crewbo task merge %s%s (they will have to update their task)",
		errConflict, taskID, strings.Join(refused, "; "), taskID, flags.String())
}

// mergeOnce merges commit into the remote branch as it stands now and
// pushes the result; it returns the merge commit.
func (d *Daemon) mergeOnce(ctx context.Context, g *gitx.Git, repo, remote, branch, taskID, commit string) (string, error) {
	main, err := g.RemoteRef(ctx, repo, remote, branch)
	if err != nil {
		return "", err
	}
	if err := g.Fetch(ctx, repo, remote, branch); err != nil {
		return "", err
	}
	res, err := g.MergeTree(ctx, repo, main, commit)
	if err != nil {
		return "", err
	}
	if !res.Clean {
		return "", fmt.Errorf("%w: task %s conflicts with %s on %s; update the task and checkpoint it again", errConflict, taskID, strings.TrimPrefix(branch, "refs/heads/"), strings.Join(res.Conflicts, ", "))
	}
	merged, err := g.CommitTree(ctx, repo, res.Tree, "Merge Crewbo task "+taskID, main, commit)
	if err != nil {
		return "", err
	}
	if err := g.Push(ctx, repo, remote, merged, branch); err != nil {
		return "", fmt.Errorf("push to %s: %w", remote, err)
	}
	return merged, nil
}
