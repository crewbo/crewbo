package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/gitx"
	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// startOwnTask starts a task the member gives themselves (ADR 0014,
// M5-07): no lead and no approval, otherwise like a dispatched task. It
// creates the task's worktree on branch crewbo/<id> at the repository's
// current commit and claims its owned paths for the task's session; any
// agent the member opens in the worktree is that session. The task is
// recorded only on this machine, signed with the member's own key, and
// the room hears RUNNING. It runs on the connection goroutine.
func (d *Daemon) startOwnTask(ctx context.Context, req Request) Response {
	fail := func(format string, a ...any) Response {
		return Response{ID: req.ID, Error: fmt.Sprintf(format, a...)}
	}
	if d.sync == nil {
		return fail("task_start: the daemon is not ready")
	}
	b, deviceKey, login, ok := d.sync.binding(req.Repo)
	if !ok {
		return fail("this repository is not in a room on the server you are signed in to; run crewbo room create or join")
	}
	key, err := keys.Load(d.keyring(), d.cfg.Paths.Data, keys.Timeout)
	if err != nil {
		return fail("load this device's key: %v", err)
	}
	if key.ID() != deviceKey {
		return fail("this device's key is not the one registered at sign-in; run crewbo login again")
	}
	g := gitx.New("")
	head, err := g.Head(ctx, req.Repo)
	if err != nil {
		return fail("read the repository's current commit: %v", err)
	}
	id, err := dispatch.NewTaskID()
	if err != nil {
		return fail("task id: %v", err)
	}
	now := d.cfg.Now()
	body := &crewbov1.TaskBody{
		TaskId: id, RoomId: b.RoomID, AuthorKeyId: key.ID(), AuthorMemberId: login, AssigneeMemberId: login,
		Text: req.Text, OwnedPaths: req.Patterns, BaseCommit: head,
		CreatedUnixMs: now.UnixMilli(), ExpiresUnixMs: now.Add(MaxTaskTTL).UnixMilli(),
	}
	if err := dispatch.WellFormed(body); err != nil {
		return fail("%v", err)
	}
	st, err := dispatch.Sign(key, body)
	if err != nil {
		return fail("sign the task: %v", err)
	}
	// Claim first: a refused claim then leaves no worktree or branch behind.
	// ponytail: the claims last MaxCLILease (8 h), as terminal claims do,
	// since no run renews them; renew on checkpoints if tasks outlive that.
	session := uuid.NewString()
	var claimIDs []string
	if len(body.GetOwnedPaths()) > 0 {
		resp := d.sharedClaim(ctx, Request{
			Method: MethodClaimAdd, Session: session, Repo: b.Repo, Patterns: body.GetOwnedPaths(),
			Task: id, LeaseSeconds: int64(MaxCLILease / time.Second),
		})
		if resp.Error != "" {
			return fail("claim the task's paths: %s", resp.Error)
		}
		for _, c := range resp.Claims {
			claimIDs = append(claimIDs, c.ID)
		}
	}
	wt := filepath.Join(d.cfg.Paths.Data, "worktrees", id)
	if err := g.WorktreeAdd(ctx, req.Repo, wt, "crewbo/"+id, head); err != nil {
		if len(claimIDs) > 0 {
			d.sharedClaim(ctx, Request{Method: MethodClaimRelease, Repo: b.Repo, IDs: claimIDs})
		}
		return fail("create the worktree: %v", err)
	}
	signed, err := proto.Marshal(st)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := d.sync.db.ExecContext(ctx, `INSERT INTO tasks (digest, task_id, room_id, signed, state, received_unix_ms, expires_unix_ms, own, session) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		taskDigest(st), id, b.RoomID, signed, taskRunning, now.UnixMilli(), body.GetExpiresUnixMs(), session); err != nil {
		return fail("record the task: %v", err)
	}
	d.setRun(wt, session)
	if _, err := d.sendStatus(ctx, b.RoomID, id, crewbov1.TaskState_TASK_STATE_RUNNING, "", 0); err != nil && !errors.Is(err, errNotSent) {
		d.cfg.Log.Warn("task status not queued", "task_id", id, "err", err)
	}
	d.cfg.Log.Info("own task started", "task_id", id, "worktree", wt)
	return Response{ID: req.ID, TaskID: id, Note: fmt.Sprintf(
		"Work in %s (branch crewbo/%s) with any agent; its claims are yours there. Crewbo checkpoints your work when the agent finishes a reply, at most every 5 minutes.", wt, id)}
}

// restoreOwnTasks maps the worktree of every own task still in progress
// back to its session after a restart, so the member's agent there still
// holds the task's claims.
func (d *Daemon) restoreOwnTasks(ctx context.Context) {
	rows, err := d.sync.db.QueryContext(ctx, `SELECT task_id, session FROM tasks WHERE own = 1 AND session != '' AND state IN (?, ?)`, taskRunning, dispatch.StateCheckpointed)
	if err != nil {
		d.cfg.Log.Warn("restore own tasks", "err", err)
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, session string
		if rows.Scan(&id, &session) == nil {
			d.setRun(filepath.Join(d.cfg.Paths.Data, "worktrees", id), session)
		}
	}
}
