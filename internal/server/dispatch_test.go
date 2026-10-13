package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// dispatchRoom has a lead and a member, each with a real registered key.
type dispatchRoom struct {
	cr             claimRoom
	leadKey, mKey  ed25519.PrivateKey
	leadID, mateID string
}

func newDispatchRoom(t *testing.T) dispatchRoom {
	t.Helper()
	cr := newClaimRoom(t) // user 1 "tester" is lead; user 2 is a member
	_, lk, _ := ed25519.GenerateKey(rand.Reader)
	_, mk, _ := ed25519.GenerateKey(rand.Reader)
	d := dispatchRoom{
		cr: cr, leadKey: lk, mKey: mk,
		leadID: keys.KeyID(lk.Public().(ed25519.PublicKey)), mateID: keys.KeyID(mk.Public().(ed25519.PublicKey)),
	}
	for id, row := range map[string]struct {
		uid int64
		pub ed25519.PublicKey
	}{d.leadID: {1, lk.Public().(ed25519.PublicKey)}, d.mateID: {2, mk.Public().(ed25519.PublicKey)}} {
		if _, err := cr.s.sessions.db.Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, ?, ?, 'd', 'linux', 0)`, id, row.uid, []byte(row.pub)); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func dispatchEnv(id, room string, st *crewbov1.SignedTask) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_DispatchTask{DispatchTask: &crewbov1.DispatchTask{RoomId: room, Task: st}}}
}

func signed(k ed25519.PrivateKey, body []byte) *crewbov1.SignedTask {
	return &crewbov1.SignedTask{Body: body, Signature: ed25519.Sign(k, body), KeyId: keys.KeyID(k.Public().(ed25519.PublicKey))}
}

func TestDispatchFromTheLead(t *testing.T) {
	d := newDispatchRoom(t)
	lead := d.cr.connect(t, testToken, d.leadID)
	mate := d.cr.connect(t, d.cr.tokenB, d.mateID)
	body := []byte("opaque task bytes: the server never decodes them")
	st := signed(d.leadKey, body)

	r := request(t, lead, dispatchEnv("d1", d.cr.room, st))
	if r.GetAck().GetSeq() == 0 {
		t.Fatalf("dispatch: %v", r)
	}
	ev := nextEvent(t, mate)
	got := ev.GetTaskDispatched().GetTask()
	if !bytes.Equal(got.GetBody(), body) || !bytes.Equal(got.GetSignature(), st.GetSignature()) || got.GetKeyId() != d.leadID || ev.GetActorMemberId() != "tester" {
		t.Fatalf("mate received %v (actor %q)", got, ev.GetActorMemberId())
	}

	tampered := signed(d.leadKey, body)
	tampered.Signature[0] ^= 1
	// Correctly signed by the lead, but labelled with another key's id:
	// every teammate would reject it, so the server refuses it at once.
	mislabelled := signed(d.leadKey, body)
	mislabelled.KeyId = d.mateID
	for name, tc := range map[string]struct {
		conn *websocket.Conn
		env  *crewbov1.Envelope
		code crewbov1.ErrorCode
	}{
		"from a member":                  {mate, dispatchEnv("x1", d.cr.room, signed(d.mKey, body)), crewbov1.ErrorCode_ERROR_CODE_NOT_LEAD},
		"signed by another key":          {lead, dispatchEnv("x2", d.cr.room, signed(d.mKey, body)), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"bad signature":                  {lead, dispatchEnv("x3", d.cr.room, tampered), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"labelled with another key":      {lead, dispatchEnv("x7", d.cr.room, mislabelled), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"no task":                        {lead, dispatchEnv("x4", d.cr.room, nil), crewbov1.ErrorCode_ERROR_CODE_INVALID},
		"a room this connection skipped": {lead, dispatchEnv("x5", "r_other", st), crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER},
	} {
		if r := request(t, tc.conn, tc.env); r.GetError().GetCode() != tc.code {
			t.Errorf("%s: %v, want %v", name, r, tc.code)
		}
	}
	// A lead connected without a device key cannot send tasks.
	nokey := d.cr.connect(t, testToken, "")
	if r := request(t, nokey, dispatchEnv("x6", d.cr.room, st)); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_INVALID {
		t.Errorf("lead without a device key: %v", r)
	}
	// Nothing refused reached the log: only the one task.
	evs, err := d.cr.s.events.Since(context.Background(), d.cr.room, 0)
	n := 0
	for _, e := range evs {
		if e.GetTaskDispatched() != nil {
			n++
		}
	}
	if err != nil || n != 1 {
		t.Errorf("tasks in the log = %d, %v; want 1", n, err)
	}
}

// A revoked lead key is refused even though the signature is valid.
func TestDispatchWithARevokedKey(t *testing.T) {
	d := newDispatchRoom(t)
	lead := d.cr.connect(t, testToken, d.leadID)
	if _, err := d.cr.s.sessions.db.Exec(`UPDATE device_keys SET revoked_unix_ms = ? WHERE key_id = ?`, time.Now().UnixMilli(), d.leadID); err != nil {
		t.Fatal(err)
	}
	if r := request(t, lead, dispatchEnv("d1", d.cr.room, signed(d.leadKey, []byte("x")))); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
		t.Errorf("revoked key: %v", r)
	}
}

func statusEnv(id, room, task string, state crewbov1.TaskState, detail string) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_TaskStatus{TaskStatus: &crewbov1.TaskStatus{RoomId: room, TaskId: task, State: state, Detail: detail}}}
}

// A member's task status is recorded with the member as actor; malformed
// ones are refused.
func TestTaskStatusIsRecorded(t *testing.T) {
	d := newDispatchRoom(t)
	lead := d.cr.connect(t, testToken, d.leadID)
	mate := d.cr.connect(t, d.cr.tokenB, d.mateID)
	r := request(t, mate, statusEnv("s1", d.cr.room, "task-1", crewbov1.TaskState_TASK_STATE_DECLINED, "not today"))
	if r.GetAck().GetSeq() == 0 {
		t.Fatalf("status: %v", r)
	}
	ev := nextEvent(t, lead)
	st := ev.GetTaskStatusChanged().GetStatus()
	if st.GetTaskId() != "task-1" || st.GetState() != crewbov1.TaskState_TASK_STATE_DECLINED || st.GetDetail() != "not today" || ev.GetActorMemberId() == "tester" || ev.GetActorMemberId() == "" {
		t.Fatalf("lead received %v (actor %q)", st, ev.GetActorMemberId())
	}
	long := string(make([]byte, maxDetail+1))
	for name, env := range map[string]*crewbov1.Envelope{
		"no task id":    statusEnv("x1", d.cr.room, "", crewbov1.TaskState_TASK_STATE_APPROVED, ""),
		"long task id":  statusEnv("x2", d.cr.room, string(make([]byte, maxTaskID+1)), crewbov1.TaskState_TASK_STATE_APPROVED, ""),
		"no state":      statusEnv("x3", d.cr.room, "t", crewbov1.TaskState_TASK_STATE_UNSPECIFIED, ""),
		"dispatched":    statusEnv("x4", d.cr.room, "t", crewbov1.TaskState_TASK_STATE_DISPATCHED, ""),
		"unknown state": statusEnv("x5", d.cr.room, "t", crewbov1.TaskState(99), ""),
		"long detail":   statusEnv("x6", d.cr.room, "t", crewbov1.TaskState_TASK_STATE_DECLINED, long),
	} {
		if r := request(t, mate, env); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_INVALID {
			t.Errorf("%s: %v", name, r)
		}
	}
	if r := request(t, mate, statusEnv("x7", "r_other", "t", crewbov1.TaskState_TASK_STATE_DECLINED, "")); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER {
		t.Errorf("another room: %v", r)
	}
}

func warningEnv(id, room string, w *crewbov1.ConflictWarning) *crewbov1.Envelope {
	w.RoomId = room
	return &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_ConflictWarning{ConflictWarning: w}}
}

// A conflict warning from a member reaches the room with the member as
// actor; malformed ones are refused. Checkpoint refs ride on statuses.
func TestConflictWarningIsRecorded(t *testing.T) {
	d := newDispatchRoom(t)
	lead := d.cr.connect(t, testToken, d.leadID)
	mate := d.cr.connect(t, d.cr.tokenB, d.mateID)
	w := &crewbov1.ConflictWarning{TaskId: "t1", Commit: "c1", OtherTaskId: "t2", OtherCommit: "c2", Paths: []string{"src/a.go", "README.md"}}
	if r := request(t, mate, warningEnv("w1", d.cr.room, w)); r.GetAck().GetSeq() == 0 {
		t.Fatalf("warning: %v", r)
	}
	ev := nextEvent(t, lead)
	got := ev.GetConflictWarning()
	if got.GetTaskId() != "t1" || got.GetOtherTaskId() != "t2" || len(got.GetPaths()) != 2 || got.GetPaths()[1] != "README.md" || ev.GetActorMemberId() == "tester" {
		t.Fatalf("lead received %v (actor %q)", got, ev.GetActorMemberId())
	}
	long := string(make([]byte, maxPath+1))
	many := make([]string, maxPatterns+1)
	for i := range many {
		many[i] = "p"
	}
	for name, bad := range map[string]*crewbov1.ConflictWarning{
		"no task":        {OtherTaskId: "t2"},
		"no other task":  {TaskId: "t1"},
		"long commit":    {TaskId: "t1", OtherTaskId: "t2", Commit: string(make([]byte, maxCommit+1))},
		"empty path":     {TaskId: "t1", OtherTaskId: "t2", Paths: []string{""}},
		"long path":      {TaskId: "t1", OtherTaskId: "t2", Paths: []string{long}},
		"too many paths": {TaskId: "t1", OtherTaskId: "t2", Paths: many},
	} {
		if r := request(t, mate, warningEnv("x", d.cr.room, bad)); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_INVALID {
			t.Errorf("%s: %v", name, r)
		}
	}
	if r := request(t, mate, warningEnv("x", "r_other", &crewbov1.ConflictWarning{TaskId: "t1", OtherTaskId: "t2"})); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER {
		t.Errorf("another room: %v", r)
	}
	st := statusEnv("s", d.cr.room, "t1", crewbov1.TaskState_TASK_STATE_CHECKPOINTED, "")
	st.GetTaskStatus().Ref, st.GetTaskStatus().Commit = "refs/crewbo/m/t1", "abc"
	if r := request(t, mate, st); r.GetAck().GetSeq() == 0 {
		t.Fatalf("checkpoint status: %v", r)
	}
	if got := nextEvent(t, lead).GetTaskStatusChanged().GetStatus(); got.GetRef() != "refs/crewbo/m/t1" || got.GetCommit() != "abc" {
		t.Errorf("status lost its ref: %v", got)
	}
	st.GetTaskStatus().Ref = string(make([]byte, maxRef+1))
	if r := request(t, mate, st); r.GetError().GetCode() != crewbov1.ErrorCode_ERROR_CODE_INVALID {
		t.Errorf("long ref: %v", r)
	}
}
