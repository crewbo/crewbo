package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/server"
)

// sentTasks returns the distinct signed tasks the server recorded for the
// room. The outbox delivers at least once: a request resent after a
// dropped connection is recorded again (decision log, #92), so copies are
// counted once, as receiving daemons do.
func (f *crossFixture) sentTasks() []*crewbov1.SignedTask {
	f.t.Helper()
	b, _ := auth.LoadBindings(auth.BindingsPath(f.a.paths.Config))
	evs, err := server.NewLog(f.db.SQL()).Since(context.Background(), b[0].RoomID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []*crewbov1.SignedTask
	seen := map[string]bool{}
	for _, ev := range evs {
		if st := ev.GetTaskDispatched().GetTask(); st != nil && !seen[string(taskDigest(st))] {
			seen[string(taskDigest(st))] = true
			out = append(out, st)
		}
	}
	return out
}

// checkFor is what gavriel953's daemon would verify a task with.
func (f *crossFixture) checkFor(me string) dispatch.Check {
	f.t.Helper()
	b, _ := auth.LoadBindings(auth.BindingsPath(f.a.paths.Config))
	rk, err := server.NewRooms(f.db.SQL(), time.Now).Keys(context.Background(), auth.User{ID: 2}, b[0].RoomID)
	if err != nil {
		f.t.Fatal(err)
	}
	c := dispatch.Check{RoomID: b[0].RoomID, Lead: rk.LeadLogin, Me: me, Now: time.Now(), Keys: map[string]dispatch.Key{}}
	for _, m := range rk.Members {
		for _, k := range m.Keys {
			c.Keys[k.KeyID] = dispatch.Key{Member: m.Login, Public: k.PublicKey, Revoked: k.Revoked}
		}
	}
	return c
}

func (m *machine) task(to, text string, paths []string, ttl time.Duration) (Response, error) {
	return m.call(Request{Method: MethodTaskCreate, Repo: m.repo, Assignee: to, Text: text, Patterns: paths, TTLSeconds: int64(ttl.Seconds())})
}

func TestLeadSendsASignedTask(t *testing.T) {
	f := newCrossFixture(t)
	text := "Add a retry with backoff to the sync client.\nDo not touch the hooks."
	resp, err := f.a.task("gavriel953", text, []string{"internal/daemon/sync.go"}, time.Hour)
	if err != nil || resp.TaskID == "" || !strings.Contains(resp.Note, "Sent to gavriel953 in room crewbo.") {
		t.Fatalf("task create: %+v %v", resp, err)
	}
	sent := f.sentTasks()
	if len(sent) != 1 {
		t.Fatalf("tasks recorded: %d", len(sent))
	}
	body, err := dispatch.Verify(sent[0], f.checkFor("gavriel953"))
	if err != nil {
		t.Fatalf("the recorded task does not verify for its assignee: %v", err)
	}
	head, _ := exec.Command("git", "-C", f.a.repo, "rev-parse", "HEAD").Output() //nolint:gosec // test
	if body.GetTaskId() != resp.TaskID || body.GetText() != text || body.GetAuthorMemberId() != "RM1338" ||
		body.GetBaseCommit() != strings.TrimSpace(string(head)) || body.GetOwnedPaths()[0] != "internal/daemon/sync.go" {
		t.Errorf("body = %+v", body)
	}
	if body.GetExpiresUnixMs()-body.GetCreatedUnixMs() != time.Hour.Milliseconds() {
		t.Errorf("lifetime = %d ms", body.GetExpiresUnixMs()-body.GetCreatedUnixMs())
	}
	// It is not for the lead.
	if _, err := dispatch.Verify(sent[0], f.checkFor("RM1338")); err == nil {
		t.Error("the task verifies for someone it is not addressed to")
	}

	// Only the lead may send; bad input is refused before signing.
	for name, tc := range map[string]struct {
		m    *machine
		text string
		path []string
		ttl  time.Duration
		want string
	}{
		"a member sends":      {f.b, "x", nil, time.Hour, "only the room lead"},
		"path outside repo":   {f.a, "x", []string{"../secrets"}, time.Hour, "owned path"},
		"too long a lifetime": {f.a, "x", nil, 8 * 24 * time.Hour, "at most"},
		"no text":             {f.a, "", nil, time.Hour, "text must be"},
	} {
		if _, err := tc.m.task("gavriel953", tc.text, tc.path, tc.ttl); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if n := len(f.sentTasks()); n != 1 {
		t.Errorf("refused tasks reached the server: %d recorded", n)
	}
}

func TestTaskSentOfflineIsDeliveredOnReconnect(t *testing.T) {
	f := newCrossFixture(t)
	f.a.link.cut()
	eventually(t, "the lead to notice it is offline", func() bool { return strings.HasPrefix(f.a.status().Sync, "offline") })
	resp, err := f.a.task("gavriel953", "Write the release notes.", nil, 0)
	if err != nil || !strings.Contains(resp.Note, "queued") {
		t.Fatalf("offline task: %+v %v", resp, err)
	}
	f.a.link.restore()
	eventually(t, "the queued task to reach the server", func() bool { return len(f.sentTasks()) == 1 })
	body, err := dispatch.Verify(f.sentTasks()[0], f.checkFor("gavriel953"))
	if err != nil || body.GetTaskId() != resp.TaskID {
		t.Fatalf("delivered task: %v %v", body, err)
	}
	if body.GetExpiresUnixMs()-body.GetCreatedUnixMs() != DefaultTaskTTL.Milliseconds() {
		t.Errorf("default lifetime not applied")
	}
}

// A keychain that now holds a different key than the one registered at
// sign-in (reset, restored from elsewhere) must not sign tasks.
func TestTaskRefusedWithAnUnregisteredKey(t *testing.T) {
	f := newCrossFixture(t)
	f.a.keyring.seed = f.b.keyring.seed // any key other than the registered one
	if _, err := f.a.task("gavriel953", "x", nil, time.Hour); err == nil || !strings.Contains(err.Error(), "run crewbo login again") {
		t.Errorf("task with another key: %v", err)
	}
	if n := len(f.sentTasks()); n != 0 {
		t.Errorf("tasks recorded: %d", n)
	}
}

func (m *machine) tasks() []Task {
	m.t.Helper()
	resp, err := m.call(Request{Method: MethodTaskList})
	if err != nil {
		m.t.Fatal(err)
	}
	return resp.Tasks
}

func (m *machine) restart() {
	m.run.cancel()
	<-m.run.done
	m.start()
}

// The M4-02 acceptance test: a teammate offline at dispatch gets the task
// on the next connection, verified, and a daemon restart (which replays
// the room from the start) stores nothing twice.
func TestTeammateOfflineAtDispatchReceivesTheTask(t *testing.T) {
	f := newCrossFixture(t)
	f.b.link.cut()
	eventually(t, "the teammate to notice it is offline", func() bool { return strings.HasPrefix(f.b.status().Sync, "offline") })
	text := "Rename the sync loop.\n\nKeep the tests green."
	sent, err := f.a.task("gavriel953", text, []string{"internal/daemon/sync.go"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.b.tasks(); len(got) != 0 {
		t.Fatalf("an offline teammate has tasks: %+v", got)
	}
	f.b.link.restore()
	eventually(t, "the teammate to receive the task", func() bool { return len(f.b.tasks()) == 1 })
	got := f.b.tasks()[0]
	if got.ID != sent.TaskID || got.State != "dispatched" || got.Text != text || got.Author != "RM1338" ||
		len(got.OwnedPaths) != 1 || got.OwnedPaths[0] != "internal/daemon/sync.go" || got.BaseCommit == "" {
		t.Errorf("received %+v", got)
	}
	if got := f.a.tasks(); len(got) != 0 {
		t.Errorf("the lead lists a task addressed to someone else: %+v", got)
	}

	// After a restart the room replays from seq 0; the first task must not
	// come back as a duplicate or a replay. A second task arriving proves
	// the replay got past the first.
	f.b.restart()
	second, err := f.a.task("gavriel953", "Second task.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the second task", func() bool { return len(f.b.tasks()) == 2 })
	for _, task := range f.b.tasks() {
		if task.State != "dispatched" || (task.ID != sent.TaskID && task.ID != second.TaskID) {
			t.Errorf("after a restart: %+v", task)
		}
	}
}

// Tasks that fail verification are stored as rejected with the reason,
// and nothing from them is shown.
func TestForgedTasksAreRejected(t *testing.T) {
	f := newCrossFixture(t)
	ctx := context.Background()
	roomID := f.checkFor("gavriel953").RoomID
	now := time.Now()
	body := func(room string) *crewbov1.TaskBody {
		id, _ := dispatch.NewTaskID()
		return &crewbov1.TaskBody{
			TaskId: id, RoomId: room, AuthorKeyId: f.a.key.ID(), AuthorMemberId: "RM1338", AssigneeMemberId: "gavriel953",
			Text: "secret instructions", CreatedUnixMs: now.UnixMilli(), ExpiresUnixMs: now.Add(time.Hour).UnixMilli(),
		}
	}
	tampered, err := dispatch.Sign(f.a.key, body(roomID))
	if err != nil {
		t.Fatal(err)
	}
	tampered.Body[len(tampered.Body)-1] ^= 1
	otherRoom, err := dispatch.Sign(f.a.key, body("r_another"))
	if err != nil {
		t.Fatal(err)
	}
	notLead := body(roomID)
	notLead.AuthorKeyId, notLead.AuthorMemberId = f.b.key.ID(), "gavriel953"
	fromMember, err := dispatch.Sign(f.b.key, notLead)
	if err != nil {
		t.Fatal(err)
	}
	// The server refuses these from a daemon, so write them into the log
	// directly, as a compromised server could.
	log := server.NewLog(f.db.SQL())
	for _, st := range []*crewbov1.SignedTask{tampered, otherRoom, fromMember} {
		ev := &crewbov1.Event{ActorMemberId: "RM1338", Body: &crewbov1.Event_TaskDispatched{TaskDispatched: &crewbov1.TaskDispatched{Task: st}}}
		if _, err := log.Append(ctx, roomID, ev, now); err != nil {
			t.Fatal(err)
		}
	}
	f.b.restart() // replays the log, including the events written above
	eventually(t, "the forged tasks to be handled", func() bool { return len(f.b.tasks()) == 3 })
	reasons := map[string]bool{}
	for _, task := range f.b.tasks() {
		if task.State != "rejected" || task.ID != "" || task.Text != "" || task.Author != "" {
			t.Errorf("a forged task was trusted: %+v", task)
		}
		reasons[task.Detail] = true
	}
	for _, want := range []error{dispatch.ErrBadSignature, dispatch.ErrWrongRoom, dispatch.ErrNotLead} {
		if !reasons[want.Error()] {
			t.Errorf("no task rejected with %q; got %v", want, reasons)
		}
	}
}

// A task signed with a lead device key registered after the teammate
// cached the room's keys verifies once the keys are fetched again.
func TestTaskFromANewLeadDeviceIsVerified(t *testing.T) {
	f := newCrossFixture(t)
	first, err := f.a.task("gavriel953", "First.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first task (fills the key cache)", func() bool { return len(f.b.tasks()) == 1 })

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	laptop := keys.Key{Private: priv}
	if _, err := f.db.SQL().Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, 1, ?, 'laptop', 'linux', 0)`, laptop.ID(), []byte(laptop.Public())); err != nil {
		t.Fatal(err)
	}
	roomID := f.checkFor("gavriel953").RoomID
	id, _ := dispatch.NewTaskID()
	now := time.Now()
	st, err := dispatch.Sign(laptop, &crewbov1.TaskBody{
		TaskId: id, RoomId: roomID, AuthorKeyId: laptop.ID(), AuthorMemberId: "RM1338", AssigneeMemberId: "gavriel953",
		Text: "From the laptop.", CreatedUnixMs: now.UnixMilli(), ExpiresUnixMs: now.Add(time.Hour).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Written straight to the log, it reaches B when the next live event
	// shows B a gap and B replays from its cursor, keys still cached.
	ev := &crewbov1.Event{ActorMemberId: "RM1338", Body: &crewbov1.Event_TaskDispatched{TaskDispatched: &crewbov1.TaskDispatched{Task: st}}}
	if _, err := server.NewLog(f.db.SQL()).Append(context.Background(), roomID, ev, now); err != nil {
		t.Fatal(err)
	}
	third, err := f.a.task("gavriel953", "Third.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "all three tasks", func() bool { return len(f.b.tasks()) == 3 })
	ids := map[string]string{}
	for _, task := range f.b.tasks() {
		ids[task.ID] = task.State
		if task.State != "dispatched" {
			t.Errorf("task %+v", task)
		}
	}
	for _, want := range []string{first.TaskID, id, third.TaskID} {
		if ids[want] != "dispatched" {
			t.Errorf("task %s missing: %v", want, ids)
		}
	}
}

// statuses returns the task status events in the room log.
func (f *crossFixture) statuses() []*crewbov1.Event {
	f.t.Helper()
	evs, err := server.NewLog(f.db.SQL()).Since(context.Background(), f.checkFor("gavriel953").RoomID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []*crewbov1.Event
	for _, ev := range evs {
		if ev.GetTaskStatusChanged() != nil {
			out = append(out, ev)
		}
	}
	return out
}

func (m *machine) answer(method, task, reason string) (Response, error) {
	return m.call(Request{Method: method, Task: task, Text: reason})
}

func TestTeammateApprovesOrDeclines(t *testing.T) {
	f := newCrossFixture(t)
	fakeClaude(t)
	f.b.fetchFrom(f.a)
	one, err := f.a.task("gavriel953", "First.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.a.task("gavriel953", "Second.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "both tasks", func() bool { return len(f.b.tasks()) == 2 })

	// The fake agent exits at once, so the approved task runs and finishes.
	if resp, err := f.b.answer(MethodTaskApprove, one.TaskID, ""); err != nil || !strings.HasSuffix(resp.Note, "The lead has been told.") {
		t.Fatalf("approve: %+v %v", resp, err)
	}
	if resp, err := f.b.answer(MethodTaskDecline, two.TaskID, "busy with the release"); err != nil || resp.Note != "The lead has been told." {
		t.Fatalf("decline: %+v %v", resp, err)
	}
	eventually(t, "the run to finish", func() bool {
		for _, task := range f.b.tasks() {
			if task.ID == one.TaskID {
				return task.State == "finished"
			}
		}
		return false
	})
	states := map[string]string{}
	for _, task := range f.b.tasks() {
		states[task.ID] = task.State + "/" + task.Detail
		if task.Text == "" {
			t.Errorf("an answered task lost its text: %+v", task)
		}
	}
	if states[one.TaskID] != "finished/" || states[two.TaskID] != "declined/busy with the release" {
		t.Errorf("states = %v", states)
	}
	got := map[string][]crewbov1.TaskState{}
	for _, ev := range f.statuses() {
		st := ev.GetTaskStatusChanged().GetStatus()
		got[st.GetTaskId()] = append(got[st.GetTaskId()], st.GetState())
		if ev.GetActorMemberId() != "gavriel953" {
			t.Errorf("status actor %q", ev.GetActorMemberId())
		}
	}
	if !slices.Equal(got[one.TaskID], []crewbov1.TaskState{crewbov1.TaskState_TASK_STATE_APPROVED, crewbov1.TaskState_TASK_STATE_RUNNING}) ||
		!slices.Equal(got[two.TaskID], []crewbov1.TaskState{crewbov1.TaskState_TASK_STATE_DECLINED}) {
		t.Errorf("statuses in the log = %v", got)
	}

	for name, tc := range map[string]struct {
		m            *machine
		method, task string
		want         string
	}{
		"approve twice":        {f.b, MethodTaskApprove, one.TaskID, "already finished"},
		"decline an approved":  {f.b, MethodTaskDecline, one.TaskID, "already finished"},
		"approve a declined":   {f.b, MethodTaskApprove, two.TaskID, "already declined"},
		"unknown task":         {f.b, MethodTaskApprove, "nope", "no task nope"},
		"the lead's own sends": {f.a, MethodTaskApprove, one.TaskID, "no task"},
	} {
		if _, err := tc.m.answer(tc.method, tc.task, ""); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if n := len(f.statuses()); n != 3 {
		t.Errorf("refused answers reached the room: %d statuses", n)
	}
}

func TestUnansweredTaskExpires(t *testing.T) {
	f := newCrossFixture(t)
	sent, err := f.a.task("gavriel953", "Quick one.", nil, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return len(f.b.tasks()) == 1 })
	deadline := time.Now().Add(10 * time.Second)
	for f.b.tasks()[0].State != "expired" {
		if time.Now().After(deadline) {
			t.Fatalf("not expired: %+v", f.b.tasks()[0])
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := f.b.answer(MethodTaskApprove, sent.TaskID, ""); err == nil || !strings.Contains(err.Error(), "already expired") {
		t.Errorf("approve after expiry: %v", err)
	}
	eventually(t, "the expiry in the room log", func() bool {
		s := f.statuses()
		return len(s) == 1 && s[0].GetTaskStatusChanged().GetStatus().GetState() == crewbov1.TaskState_TASK_STATE_EXPIRED
	})
}
