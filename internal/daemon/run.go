package daemon

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/gitx"
	"github.com/crewbo/crewbo/internal/platform"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// Task run states (store.Daemon migration 4). finished means the agent
// exited cleanly; M5-01 replaces it with a pushed checkpoint.
const (
	taskRunning  = "running"
	taskFinished = "finished"
	taskFailed   = "failed"
)

// claimRenewEvery keeps a running agent's claims alive on the server:
// leases last claims.DefaultLease (5 min) from each renewal.
const claimRenewEvery = 2 * time.Minute

// signInTimeout bounds `claude auth status`.
const signInTimeout = 20 * time.Second

// taskRun is an approved task about to run.
type taskRun struct {
	b          auth.Binding
	body       *crewbov1.TaskBody
	agent      string // dispatch.AgentClaudeCode or dispatch.AgentCodex
	keepAPIKey bool
}

// agentName is how messages name an agent.
func agentName(agent string) string {
	if agent == dispatch.AgentCodex {
		return "Codex"
	}
	return "Claude Code"
}

func (d *Daemon) agentBin(agent string) string {
	switch {
	case agent == dispatch.AgentCodex && d.cfg.CodexBin != "":
		return d.cfg.CodexBin
	case agent == dispatch.AgentCodex:
		return "codex"
	case d.cfg.ClaudeBin != "":
		return d.cfg.ClaudeBin
	}
	return "claude"
}

// runSession returns the agent session of the task running in the
// worktree repo, if any.
func (d *Daemon) runSession(repo string) (string, bool) {
	if repo == "" {
		return "", false
	}
	d.runsMu.Lock()
	defer d.runsMu.Unlock()
	s, ok := d.runs[pathKey(repo)]
	return s, ok
}

func (d *Daemon) setRun(worktree, session string) {
	d.runsMu.Lock()
	defer d.runsMu.Unlock()
	if session == "" {
		delete(d.runs, pathKey(worktree))
		return
	}
	d.runs[pathKey(worktree)] = session
}

// prepareRun checks what running a waiting task needs before it is
// approved. It returns nil and no error for a task that is not waiting.
func (d *Daemon) prepareRun(ctx context.Context, req Request) (*taskRun, error) {
	agent := req.Agent
	switch agent {
	case "", dispatch.AgentClaudeCode:
		agent = dispatch.AgentClaudeCode
	case dispatch.AgentCodex:
		if !platform.CodexDispatchSupported() {
			return nil, errors.New("running tasks with Codex is not supported on Windows yet (unknown U-20); approve with Claude Code instead")
		}
	default:
		return nil, fmt.Errorf("unknown agent %q; use %s or %s", agent, dispatch.AgentClaudeCode, dispatch.AgentCodex)
	}
	var room string
	var signed []byte
	err := d.sync.db.QueryRowContext(ctx, `SELECT room_id, signed FROM tasks WHERE task_id = ? AND state = ?`, req.Task, taskDispatched).Scan(&room, &signed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st, body := &crewbov1.SignedTask{}, &crewbov1.TaskBody{}
	if err := proto.Unmarshal(signed, st); err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(st.GetBody(), body); err != nil { // verified when stored
		return nil, err
	}
	b, ok := d.sync.roomBinding(room)
	if !ok {
		return nil, errors.New("no repository on this machine is in the task's room; run crewbo room join in your clone")
	}
	has, err := gitx.New("").HasCommit(ctx, b.Repo, body.GetBaseCommit())
	if err != nil {
		return nil, fmt.Errorf("check the base commit: %w", err)
	}
	if !has {
		return nil, fmt.Errorf("the task starts from commit %s, which %s does not have; run git fetch there and approve again", body.GetBaseCommit(), b.Repo)
	}
	sctx, cancel := context.WithTimeout(ctx, signInTimeout)
	defer cancel()
	signedIn := dispatch.ClaudeSignedIn
	if agent == dispatch.AgentCodex {
		signedIn = dispatch.CodexSignedIn
	}
	if err := signedIn(sctx, d.agentBin(agent), os.Environ(), req.KeepAPIKey); err != nil {
		return nil, err
	}
	return &taskRun{b: b, body: body, agent: agent, keepAPIKey: req.KeepAPIKey}, nil
}

// startRun creates the task's worktree at its base commit, claims its
// owned paths for a new agent session, and starts the agent there. A
// failure marks the task failed (data-model.md: Approved to Failed).
func (d *Daemon) startRun(ctx context.Context, r *taskRun) (string, error) {
	id := r.body.GetTaskId()
	failed := func(err error) (string, error) {
		d.finishRun(ctx, r, err.Error())
		return "", err
	}
	wt := filepath.Join(d.cfg.Paths.Data, "worktrees", id)
	if err := gitx.New("").WorktreeAdd(ctx, r.b.Repo, wt, "crewbo/"+id, r.body.GetBaseCommit()); err != nil {
		return failed(fmt.Errorf("create the worktree: %w", err))
	}
	session := uuid.NewString()
	var claimIDs []string
	if len(r.body.GetOwnedPaths()) > 0 {
		resp := d.sharedClaim(ctx, Request{
			Method: MethodClaimAdd, Session: session, Repo: r.b.Repo, Patterns: r.body.GetOwnedPaths(),
			Task: id, LeaseSeconds: int64(claims.DefaultLease / time.Second),
		})
		if resp.Error != "" {
			return failed(fmt.Errorf("claim the task's paths: %s", resp.Error))
		}
		for _, c := range resp.Claims {
			claimIDs = append(claimIDs, c.ID)
		}
	}
	release := func() {
		if len(claimIDs) > 0 {
			d.sharedClaim(ctx, Request{Method: MethodClaimRelease, Repo: r.b.Repo, IDs: claimIDs})
		}
	}
	logPath := filepath.Join(d.cfg.Paths.Data, "tasks", id+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		release()
		return failed(err)
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the daemon's own data directory
	if err != nil {
		release()
		return failed(err)
	}
	command := dispatch.ClaudeCommand
	if r.agent == dispatch.AgentCodex {
		command = dispatch.CodexCommand
	}
	cmd := command(ctx, dispatch.Launch{
		Bin: d.agentBin(r.agent), Worktree: wt, Session: session, Text: r.body.GetText(), Env: os.Environ(), KeepAPIKey: r.keepAPIKey,
	})
	cmd.Stdout, cmd.Stderr = logf, logf
	d.setRun(wt, session)
	if err := cmd.Start(); err != nil {
		d.setRun(wt, "")
		_ = logf.Close()
		release()
		return failed(fmt.Errorf("start %s: %w", agentName(r.agent), err))
	}
	d.setTaskState(ctx, id, taskRunning, "")
	if _, err := d.sendStatus(ctx, r.b.RoomID, id, crewbov1.TaskState_TASK_STATE_RUNNING, "", 0); err != nil && !errors.Is(err, errNotSent) {
		d.cfg.Log.Warn("task status not queued", "task_id", id, "err", err)
	}
	d.cfg.Log.Info("task agent started", "task_id", id, "agent", r.agent, "worktree", wt)
	d.agents.Add(1)
	go func() {
		defer d.agents.Done()
		defer func() { _ = logf.Close() }()
		err := d.superviseRun(ctx, r, cmd, session, len(claimIDs) > 0)
		d.setRun(wt, "")
		release()
		detail := ""
		switch {
		case ctx.Err() != nil:
			detail = "stopped because the Crewbo daemon stopped"
		case err != nil && r.agent == dispatch.AgentCodex && codexFailure(logPath) != "":
			detail = "Codex: " + codexFailure(logPath)
		case err != nil:
			detail = agentName(r.agent) + " " + err.Error()
		}
		d.finishRun(ctx, r, detail)
	}()
	return fmt.Sprintf("%s is working in %s (log: %s).", agentName(r.agent), wt, logPath), nil
}

// codexFailure returns the reason of the last failed turn in a Codex run's
// log (its JSONL events), or "".
func codexFailure(logPath string) string {
	f, err := os.Open(logPath) //nolint:gosec // the daemon's own log
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	reason := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		if r := dispatch.CodexFailure(sc.Bytes()); r != "" {
			reason = r
		}
	}
	return reason
}

// superviseRun waits for the agent, renewing its claims on the server
// meanwhile, and returns how it exited.
func (d *Daemon) superviseRun(ctx context.Context, r *taskRun, cmd *exec.Cmd, session string, renew bool) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }() // ends with the process; ctx kills it
	t := time.NewTicker(d.renewEvery)
	defer t.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-t.C:
			if !renew {
				continue
			}
			_, key, _, ok := d.sync.binding(r.b.Repo)
			if !ok {
				continue
			}
			env := &crewbov1.Envelope{Id: newEnvelopeID(), Body: &crewbov1.Envelope_ClaimRenew{ClaimRenew: &crewbov1.ClaimRenew{RoomId: r.b.RoomID, SessionId: key + "/" + session}}}
			if _, err := d.sync.submit(ctx, env, 0); err != nil && !errors.Is(err, errNotSent) && ctx.Err() == nil {
				d.cfg.Log.Warn("claim renewal not queued", "task_id", r.body.GetTaskId(), "err", err)
			}
		}
	}
}

// finishRun records how a run ended: finished with no detail, otherwise
// failed with the reason, which the room hears. It survives a stopping
// daemon so the outcome is kept and sent on the next start.
func (d *Daemon) finishRun(ctx context.Context, r *taskRun, failure string) {
	ctx = context.WithoutCancel(ctx)
	id := r.body.GetTaskId()
	if failure == "" {
		d.setTaskState(ctx, id, taskFinished, "")
		d.cfg.Log.Info("task agent finished", "task_id", id)
		return
	}
	d.setTaskState(ctx, id, taskFailed, failure)
	d.cfg.Log.Warn("task failed", "task_id", id, "reason", failure)
	if _, err := d.sendStatus(ctx, r.b.RoomID, id, crewbov1.TaskState_TASK_STATE_FAILED, truncate(failure, maxReason), 0); err != nil && !errors.Is(err, errNotSent) {
		d.cfg.Log.Warn("task status not queued", "task_id", id, "err", err)
	}
}

func (d *Daemon) setTaskState(ctx context.Context, id, state, detail string) {
	if _, err := d.sync.db.ExecContext(ctx, `UPDATE tasks SET state = ?, detail = ? WHERE task_id = ?`, state, detail, id); err != nil {
		d.cfg.Log.Warn("record task state", "task_id", id, "state", state, "err", err)
	}
}

// truncate cuts s to at most n bytes without splitting a character
// (protobuf strings must be valid UTF-8).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
