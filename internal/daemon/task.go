package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/gitx"
	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/server"
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

// Stored task states (store.Daemon migration 2).
const (
	taskPending    = "pending" // received, not verified yet
	taskDispatched = "dispatched"
	taskRejected   = "rejected"
	taskOther      = "other" // addressed to another member
	taskApproved   = "approved"
	taskDeclined   = "declined"
	taskExpired    = "expired"
)

// Task verification bounds: the room keys request, and the wait before
// retrying tasks whose keys could not be fetched.
const (
	keysTimeout = 5 * time.Second
	taskRetry   = 5 * time.Second
)

// receiveTask stores a task from the room log as pending and wakes the
// verifier. It runs on the syncer goroutine, which must keep reading
// (pongs arrive there), so it does no network I/O. An error means nothing
// was stored and the event is replayed.
func (d *Daemon) receiveTask(ctx context.Context, b auth.Binding, st *crewbov1.SignedTask) error {
	signed, err := proto.Marshal(st)
	if err != nil {
		return err
	}
	// A stored digest is a replayed room log or a resent request.
	_, err = d.sync.db.ExecContext(ctx, `INSERT INTO tasks (digest, room_id, signed, state, received_unix_ms) VALUES (?, ?, ?, ?, ?) ON CONFLICT (digest) DO NOTHING`,
		taskDigest(st), b.RoomID, signed, taskPending, d.cfg.Now().UnixMilli())
	if err != nil {
		return err
	}
	select {
	case d.verifyKick <- struct{}{}:
	default: // a pass is already due
	}
	return nil
}

// verifyTasks verifies pending tasks until ctx ends: on each kick, and
// every retry interval while some wait for keys it could not fetch.
func (d *Daemon) verifyTasks(ctx context.Context, retry time.Duration) {
	roomKeys := map[string]server.RoomKeys{} // per room, refreshed on a key miss
	t := time.NewTicker(retry)
	defer t.Stop()
	for {
		if err := d.verifyPending(ctx, roomKeys); err != nil && ctx.Err() == nil {
			d.cfg.Log.Warn("cannot verify received tasks yet; will retry", "err", err)
		}
		if err := d.expireTasks(ctx); err != nil && ctx.Err() == nil {
			d.cfg.Log.Warn("cannot expire tasks", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-d.verifyKick:
		case <-t.C:
		}
	}
}

type pendingTask struct {
	digest []byte
	room   string
	st     *crewbov1.SignedTask
}

// verifyPending verifies pending tasks in arrival order and records each
// outcome. It stops at the first task whose room keys it cannot fetch.
func (d *Daemon) verifyPending(ctx context.Context, roomKeys map[string]server.RoomKeys) error {
	db := d.sync.db
	rows, err := db.QueryContext(ctx, `SELECT digest, room_id, signed FROM tasks WHERE state = ? ORDER BY received_unix_ms`, taskPending)
	if err != nil {
		return err
	}
	var pending []pendingTask
	for rows.Next() {
		var p pendingTask
		var signed []byte
		if err := rows.Scan(&p.digest, &p.room, &signed); err != nil {
			_ = rows.Close()
			return err
		}
		p.st = &crewbov1.SignedTask{}
		if proto.Unmarshal(signed, p.st) != nil {
			p.st = nil // stored by this daemon, so not expected
		}
		pending = append(pending, p)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sess := d.sync.session()
	if len(pending) == 0 || sess.GitHubLogin == "" {
		return nil // nobody signed in: wait, "me" is unknown
	}
	seen := func(id string) bool {
		var n int
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE task_id = ?`, id).Scan(&n)
		return n > 0
	}
	for _, p := range pending {
		verify := func(fetch bool) (*crewbov1.TaskBody, error) {
			rk, ok := roomKeys[p.room]
			if fetch || !ok {
				var err error
				if rk, err = fetchRoomKeys(ctx, sess, p.room); err != nil {
					return nil, err
				}
				roomKeys[p.room] = rk
			}
			c := dispatch.Check{RoomID: p.room, Lead: rk.LeadLogin, Me: sess.GitHubLogin, Now: d.cfg.Now(), Seen: seen, Keys: map[string]dispatch.Key{}}
			for _, m := range rk.Members {
				for _, k := range m.Keys {
					c.Keys[k.KeyID] = dispatch.Key{Member: m.Login, Public: k.PublicKey, Revoked: k.Revoked}
				}
			}
			return dispatch.Verify(p.st, c)
		}
		var body *crewbov1.TaskBody
		err := dispatch.ErrMalformed
		if p.st != nil {
			body, err = verify(false)
			// Cached keys may predate a new device, a revocation or a new lead.
			if errors.Is(err, dispatch.ErrUnknownKey) || errors.Is(err, dispatch.ErrNotLead) || errors.Is(err, dispatch.ErrRevokedKey) {
				body, err = verify(true)
			}
		}
		var fetchErr *keysError
		if errors.As(err, &fetchErr) {
			return err
		}
		state, detail, taskID, expires := taskDispatched, "", any(nil), any(nil)
		switch {
		case errors.Is(err, dispatch.ErrNotForYou):
			state = taskOther
		case err != nil:
			state, detail = taskRejected, err.Error()
			d.cfg.Log.Warn("rejected a task from the room log", "room", p.room, "reason", detail)
		default:
			taskID, expires = body.GetTaskId(), body.GetExpiresUnixMs()
			d.cfg.Log.Info("task received", "task_id", body.GetTaskId(), "room", p.room, "author", body.GetAuthorMemberId()) // never the text
		}
		if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = ?, detail = ?, task_id = ?, expires_unix_ms = ? WHERE digest = ?`, state, detail, taskID, expires, p.digest); err != nil {
			return err
		}
	}
	return nil
}

// taskDigest identifies a signed task by all of its received bytes, so a
// forged copy of a body cannot shadow the genuine one.
func taskDigest(st *crewbov1.SignedTask) []byte {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(st.GetKeyId()), st.GetSignature(), st.GetBody()} {
		_ = binary.Write(h, binary.BigEndian, uint64(len(part)))
		h.Write(part)
	}
	return h.Sum(nil)
}

// keysError is a failure to get the room's keys: the task cannot be
// verified yet, so it is retried rather than rejected.
type keysError struct{ err error }

func (e *keysError) Error() string { return "get the room's keys: " + e.err.Error() }
func (e *keysError) Unwrap() error { return e.err }

// fetchRoomKeys asks the sync server for a room's lead and device keys
// (GET /v1/rooms/{id}/keys).
func fetchRoomKeys(ctx context.Context, sess auth.Session, roomID string) (server.RoomKeys, error) {
	var rk server.RoomKeys
	fail := func(err error) (server.RoomKeys, error) { return rk, &keysError{err} }
	ctx, cancel := context.WithTimeout(ctx, keysTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sess.Server+"/v1/rooms/"+url.PathEscape(roomID)+"/keys", nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Authorization", "Bearer "+sess.SessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fail(fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg))))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rk); err != nil {
		return fail(err)
	}
	return rk, nil
}

// listTasks returns the tasks sent to this member, newest first. It runs
// on the connection goroutine and reads only the database.
func (d *Daemon) listTasks(ctx context.Context, req Request) Response {
	if d.sync == nil {
		return Response{ID: req.ID, Error: "task_list: the daemon is not ready"}
	}
	rows, err := d.sync.db.QueryContext(ctx, `SELECT room_id, signed, state, detail, received_unix_ms FROM tasks WHERE state NOT IN (?, ?) ORDER BY received_unix_ms DESC`, taskPending, taskOther)
	if err != nil {
		return Response{ID: req.ID, Error: fmt.Sprintf("task_list: %v", err)}
	}
	defer func() { _ = rows.Close() }()
	resp := Response{ID: req.ID, Tasks: []Task{}}
	for rows.Next() {
		var t Task
		var signed []byte
		var received int64
		if err := rows.Scan(&t.Room, &signed, &t.State, &t.Detail, &received); err != nil {
			return Response{ID: req.ID, Error: fmt.Sprintf("task_list: %v", err)}
		}
		t.Received = time.UnixMilli(received)
		if t.State != taskRejected {
			// Verified when it was stored; decoded only now.
			st, body := &crewbov1.SignedTask{}, &crewbov1.TaskBody{}
			if proto.Unmarshal(signed, st) != nil || proto.Unmarshal(st.GetBody(), body) != nil {
				continue
			}
			t.ID, t.Author, t.Text, t.OwnedPaths, t.BaseCommit = body.GetTaskId(), body.GetAuthorMemberId(), body.GetText(), body.GetOwnedPaths(), body.GetBaseCommit()
			t.Created, t.Expires = time.UnixMilli(body.GetCreatedUnixMs()), time.UnixMilli(body.GetExpiresUnixMs())
		}
		resp.Tasks = append(resp.Tasks, t)
	}
	if err := rows.Err(); err != nil {
		return Response{ID: req.ID, Error: fmt.Sprintf("task_list: %v", err)}
	}
	return resp
}

// expireTasks marks tasks still waiting for an answer past their expiry
// as expired and tells the room (data-model.md: Dispatched to Expired).
func (d *Daemon) expireTasks(ctx context.Context) error {
	rows, err := d.sync.db.QueryContext(ctx, `UPDATE tasks SET state = ? WHERE state = ? AND expires_unix_ms <= ? RETURNING task_id, room_id`,
		taskExpired, taskDispatched, d.cfg.Now().UnixMilli())
	if err != nil {
		return err
	}
	type expired struct{ id, room string }
	var gone []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.room); err != nil {
			_ = rows.Close()
			return err
		}
		gone = append(gone, e)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range gone {
		d.cfg.Log.Info("task expired before an answer", "task_id", e.id)
		if _, err := d.sendStatus(ctx, e.room, e.id, crewbov1.TaskState_TASK_STATE_EXPIRED, "", 0); err != nil && !errors.Is(err, errNotSent) {
			return err
		}
	}
	return nil
}

// answerTask records the member's approval or refusal of a task and tells
// the room. Only a task still waiting for an answer, and not expired, can
// be answered. Approving launches nothing yet (M4-04).
func (d *Daemon) answerTask(ctx context.Context, req Request) Response {
	fail := func(format string, a ...any) Response {
		return Response{ID: req.ID, Error: fmt.Sprintf(format, a...)}
	}
	if d.sync == nil {
		return fail("%s: the daemon is not ready", req.Method)
	}
	state, wire := taskApproved, crewbov1.TaskState_TASK_STATE_APPROVED
	if req.Method == MethodTaskDecline {
		state, wire = taskDeclined, crewbov1.TaskState_TASK_STATE_DECLINED
	}
	if len(req.Text) > maxReason {
		return fail("a reason is at most %d bytes", maxReason)
	}
	db := d.sync.db
	var room string
	err := db.QueryRowContext(ctx, `UPDATE tasks SET state = ?, detail = ? WHERE task_id = ? AND state = ? AND expires_unix_ms > ? RETURNING room_id`,
		state, req.Text, req.Task, taskDispatched, d.cfg.Now().UnixMilli()).Scan(&room)
	if errors.Is(err, sql.ErrNoRows) {
		var now string
		switch err := db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE task_id = ?`, req.Task).Scan(&now); {
		case errors.Is(err, sql.ErrNoRows):
			return fail("no task %s", req.Task)
		case err != nil:
			return fail("%s: %v", req.Method, err)
		case now == taskDispatched:
			return fail("task %s has expired", req.Task)
		default:
			return fail("task %s is already %s", req.Task, now)
		}
	}
	if err != nil {
		return fail("%s: %v", req.Method, err)
	}
	d.cfg.Log.Info("task answered", "task_id", req.Task, "state", state) // never the text or reason
	resp := Response{ID: req.ID, TaskID: req.Task}
	reply, err := d.sendStatus(ctx, room, req.Task, wire, req.Text, replyWait)
	switch {
	case errors.Is(err, errNotSent):
		resp.Note = "The lead hears when the sync server answers."
	case err != nil:
		resp.Note = fmt.Sprintf("Recorded here, but telling the room failed: %v", err)
	case reply.GetError() != nil:
		resp.Note = fmt.Sprintf("Recorded here; the sync server said: %s", reply.GetError().GetMessage())
	default:
		resp.Note = "The lead has been told."
	}
	return resp
}

// maxReason is the longest decline reason (the server's TaskStatus detail
// limit).
const maxReason = 512

// sendStatus queues a TaskStatus for the room and waits up to wait for the
// server's answer.
func (d *Daemon) sendStatus(ctx context.Context, room, taskID string, state crewbov1.TaskState, detail string, wait time.Duration) (*crewbov1.Envelope, error) {
	env := &crewbov1.Envelope{Id: newEnvelopeID(), Body: &crewbov1.Envelope_TaskStatus{TaskStatus: &crewbov1.TaskStatus{RoomId: room, TaskId: taskID, State: state, Detail: detail}}}
	return d.sync.submit(ctx, env, wait)
}
