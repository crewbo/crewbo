package daemon

import (
	"context"
	"database/sql"
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

// ownCheckpointEvery is how often at most an own task is checkpointed
// automatically when its agent finishes a reply (Ronel, 2026-10-14; GitHub
// asks for at most 6 pushes a minute per repository).
const ownCheckpointEvery = 5 * time.Minute

// errNothingNew means an own task's worktree has no changes since its last
// checkpoint.
var errNothingNew = errors.New("nothing new since the last checkpoint")

// loadOwnTask returns an own task's binding and body, its state and
// session.
func (d *Daemon) loadOwnTask(ctx context.Context, id string) (r *taskRun, state, session string, err error) {
	var room string
	var signed []byte
	err = d.sync.db.QueryRowContext(ctx, `SELECT room_id, signed, state, session FROM tasks WHERE task_id = ? AND own = 1`, id).Scan(&room, &signed, &state, &session)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", fmt.Errorf("no task %s that you started", id)
	}
	if err != nil {
		return nil, "", "", err
	}
	st, body := &crewbov1.SignedTask{}, &crewbov1.TaskBody{}
	if err := proto.Unmarshal(signed, st); err != nil {
		return nil, "", "", err
	}
	if err := proto.Unmarshal(st.GetBody(), body); err != nil {
		return nil, "", "", err
	}
	b, ok := d.sync.roomBinding(room)
	if !ok {
		return nil, "", "", errors.New("no repository on this machine is in the task's room")
	}
	return &taskRun{b: b, body: body, agent: ""}, state, session, nil
}

// checkpointOwn commits what is new in an own task's worktree, pushes it
// and marks the task checkpointed, then checks it against the room's
// other checkpoints. A task checkpointed before goes back to running
// first (data-model.md: the agent resumed). Own checkpoints run one at a
// time.
func (d *Daemon) checkpointOwn(ctx context.Context, id string) (string, error) {
	d.ownMu.Lock()
	defer d.ownMu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkpointTimeout)
	defer cancel()
	r, state, _, err := d.loadOwnTask(ctx, id)
	if err != nil {
		return "", err
	}
	if state != taskRunning && state != dispatch.StateCheckpointed {
		return "", fmt.Errorf("task %s is %s", id, state)
	}
	g := gitx.New("")
	wt := filepath.Join(d.cfg.Paths.Data, "worktrees", id)
	commit, err := g.CommitAll(ctx, wt, "Checkpoint of Crewbo task "+id)
	if err != nil {
		return "", err
	}
	if commit == "" {
		return "", errNothingNew
	}
	if state == dispatch.StateCheckpointed {
		d.ownResumed(ctx, r)
	}
	ref, err := d.pushCheckpoint(ctx, g, r.b.Repo, id, commit)
	if err != nil {
		return "", fmt.Errorf("committed %s, %w", short(commit), err)
	}
	d.lastOwnCheckpoint[id] = d.cfg.Now()
	d.finishRunAt(ctx, r, runEnd{state: dispatch.StateCheckpointed, detail: ref + " at " + short(commit), ref: ref, commit: commit})
	d.checkConflicts(ctx, r, commit)
	return ref + " at " + short(commit), nil
}

// ownResumed moves a checkpointed own task back to running and tells the
// room.
func (d *Daemon) ownResumed(ctx context.Context, r *taskRun) {
	id := r.body.GetTaskId()
	if !d.setTaskState(ctx, id, taskRunning, "") {
		return
	}
	if _, err := d.sendStatus(ctx, r.b.RoomID, id, crewbov1.TaskState_TASK_STATE_RUNNING, "", 0); err != nil && !errors.Is(err, errNotSent) {
		d.cfg.Log.Warn("task status not queued", "task_id", id, "err", err)
	}
}

// ownTaskEvent reacts to a hook event from an own task's worktree: a new
// prompt after a checkpoint means the agent resumed; a finished reply
// checkpoints the task, at most every ownCheckpointEvery. It runs in the
// background, owned by the daemon's run group.
func (d *Daemon) ownTaskEvent(ctx context.Context, event, session string) {
	var id, state string
	if d.sync.db.QueryRowContext(ctx, `SELECT task_id, state FROM tasks WHERE own = 1 AND session = ?`, session).Scan(&id, &state) != nil {
		return // not an own task (a dispatched run checkpoints when it exits)
	}
	switch event {
	case "prompt":
		if state == dispatch.StateCheckpointed {
			if r, _, _, err := d.loadOwnTask(ctx, id); err == nil {
				d.ownResumed(ctx, r)
			}
		}
	case "stop":
		d.ownMu.Lock()
		last, due := d.lastOwnCheckpoint[id], true
		if !last.IsZero() && d.cfg.Now().Sub(last) < d.ownEvery {
			due = false
		}
		d.ownMu.Unlock()
		if !due {
			return
		}
		if _, err := d.checkpointOwn(ctx, id); err != nil && !errors.Is(err, errNothingNew) {
			d.cfg.Log.Warn("automatic checkpoint", "task_id", id, "err", err)
		}
	}
}

// checkpointOwnTask answers crewbo task checkpoint.
func (d *Daemon) checkpointOwnTask(ctx context.Context, req Request) Response {
	if d.sync == nil {
		return Response{ID: req.ID, Error: "task_checkpoint: the daemon is not ready"}
	}
	where, err := d.checkpointOwn(ctx, req.Task)
	switch {
	case errors.Is(err, errNothingNew):
		return Response{ID: req.ID, TaskID: req.Task, Note: "Nothing new since the last checkpoint."}
	case err != nil:
		return Response{ID: req.ID, Error: err.Error()}
	}
	return Response{ID: req.ID, TaskID: req.Task, Note: "Checkpointed as " + where + "."}
}

// releaseOwnClaims releases the claims an own task's session holds and
// forgets its worktree, once the task is merged.
func (d *Daemon) releaseOwnClaims(ctx context.Context, r *taskRun, session string) {
	_, key, _, ok := d.sync.binding(r.b.Repo)
	if !ok {
		return
	}
	reply := make(chan Response, 1)
	select {
	case d.calls <- call{req: Request{Method: MethodClaimList, Repo: r.b.Repo}, reply: reply}:
	case <-ctx.Done():
		return
	}
	var ids []string
	for _, c := range (<-reply).Claims {
		if c.Holder.SessionID == key+"/"+session {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) > 0 {
		d.sharedClaim(ctx, Request{Method: MethodClaimRelease, Repo: r.b.Repo, IDs: ids})
	}
	d.setRun(filepath.Join(d.cfg.Paths.Data, "worktrees", r.body.GetTaskId()), "")
}
