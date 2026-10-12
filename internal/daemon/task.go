package daemon

import (
	"context"
	"crypto/sha256"
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
	taskDispatched = "dispatched"
	taskRejected   = "rejected"
	taskOther      = "other" // addressed to another member
)

// keysTimeout bounds the room keys request; it runs on the syncer
// goroutine, which also has to answer the server's pings.
const keysTimeout = 5 * time.Second

// receiveTask verifies a task from the room log and stores the outcome.
// It runs on the syncer goroutine: fetching the room's keys needs the
// network. An error means nothing was stored and the event is retried.
func (d *Daemon) receiveTask(ctx context.Context, b auth.Binding, st *crewbov1.SignedTask) error {
	db := d.sync.db
	digest := taskDigest(st)
	var known int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE digest = ?`, digest).Scan(&known); err != nil {
		return err
	}
	if known > 0 {
		return nil // a replayed room log, or a resent request
	}
	sess := d.sync.session()
	seen := func(id string) bool {
		var n int
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE task_id = ?`, id).Scan(&n)
		return n > 0
	}
	verify := func(fetch bool) (*crewbov1.TaskBody, error) {
		rk, ok := d.roomKeys[b.RoomID]
		if fetch || !ok {
			var err error
			if rk, err = fetchRoomKeys(ctx, sess, b.RoomID); err != nil {
				return nil, err
			}
			d.roomKeys[b.RoomID] = rk
		}
		c := dispatch.Check{RoomID: b.RoomID, Lead: rk.LeadLogin, Me: sess.GitHubLogin, Now: d.cfg.Now(), Seen: seen, Keys: map[string]dispatch.Key{}}
		for _, m := range rk.Members {
			for _, k := range m.Keys {
				c.Keys[k.KeyID] = dispatch.Key{Member: m.Login, Public: k.PublicKey, Revoked: k.Revoked}
			}
		}
		return dispatch.Verify(st, c)
	}
	body, err := verify(false)
	// The cached keys may predate a new device, a revocation or a new lead.
	if errors.Is(err, dispatch.ErrUnknownKey) || errors.Is(err, dispatch.ErrNotLead) || errors.Is(err, dispatch.ErrRevokedKey) {
		body, err = verify(true)
	}
	var fetchErr *keysError
	if errors.As(err, &fetchErr) {
		return err
	}
	state, detail, taskID := taskDispatched, "", any(nil)
	switch {
	case errors.Is(err, dispatch.ErrNotForYou):
		state = taskOther
	case err != nil:
		state, detail = taskRejected, err.Error()
		d.cfg.Log.Warn("rejected a task from the room log", "room", b.Name, "reason", detail)
	default:
		taskID = body.GetTaskId()
		d.cfg.Log.Info("task received", "task_id", body.GetTaskId(), "room", b.Name, "author", body.GetAuthorMemberId()) // never the text
	}
	signed, err := proto.Marshal(st)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO tasks (digest, task_id, room_id, signed, state, detail, received_unix_ms) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		digest, taskID, b.RoomID, signed, state, detail, d.cfg.Now().UnixMilli())
	return err
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
	rows, err := d.sync.db.QueryContext(ctx, `SELECT room_id, signed, state, detail, received_unix_ms FROM tasks WHERE state != ? ORDER BY received_unix_ms DESC`, taskOther)
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
		if t.State == taskDispatched {
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
