package daemon

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/dispatch"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/server"
)

// sentTasks returns the signed tasks the server recorded for the room.
func (f *crossFixture) sentTasks() []*crewbov1.SignedTask {
	f.t.Helper()
	b, _ := auth.LoadBindings(auth.BindingsPath(f.a.paths.Config))
	evs, err := server.NewLog(f.db.SQL()).Since(context.Background(), b[0].RoomID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []*crewbov1.SignedTask
	for _, ev := range evs {
		if st := ev.GetTaskDispatched().GetTask(); st != nil {
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
