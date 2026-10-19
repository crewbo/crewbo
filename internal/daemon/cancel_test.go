package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// runningTask sends a task from the lead to the teammate, who approves it;
// the fake agent keeps running until the test ends.
func runningTask(t *testing.T, f *crossFixture) string {
	t.Helper()
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "hold"), "")
	sent, err := f.a.task("gavriel953", "Rework the login page.", []string{"src/a.go"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	f.b.fetchFrom(f.a)
	if _, err := f.b.approve(sent.TaskID, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the agent to start", func() bool { return len(fakeRuns(t, dir)) == 1 })
	eventually(t, "the lead to see the task's claim", func() bool { ok, _ := f.a.agentMay("src/a.go"); return !ok })
	return sent.TaskID
}

// agentStopped reports whether a machine's daemon no longer supervises
// the task's agent, which happens only once the process has exited.
func agentStopped(m *machine, id string) bool {
	m.run.d.runsMu.Lock()
	defer m.run.d.runsMu.Unlock()
	_, running := m.run.d.stops[id]
	return !running
}

func cancelled(f *crossFixture, id, actor, detail string) bool {
	for _, ev := range f.statuses() {
		st := ev.GetTaskStatusChanged().GetStatus()
		if st.GetTaskId() == id && st.GetState() == crewbov1.TaskState_TASK_STATE_CANCELLED {
			return ev.GetActorMemberId() == actor && st.GetDetail() == detail
		}
	}
	return false
}

func TestTeammateCancelsARunningTask(t *testing.T) {
	f := newCrossFixture(t)
	id := runningTask(t, f)
	if agentStopped(f.b, id) {
		t.Fatal("the running agent is not supervised")
	}
	resp, err := f.b.call(Request{Method: MethodTaskCancel, Task: id, Text: "wrong approach"})
	wt := filepath.Join(f.b.paths.Data, "worktrees", id)
	if err != nil || !strings.Contains(resp.Note, wt) {
		t.Fatalf("cancel: %+v %v", resp, err)
	}
	eventually(t, "the agent to stop", func() bool { return agentStopped(f.b, id) })
	if st := f.b.taskState(id); st.State != "cancelled" || st.Detail != "wrong approach" {
		t.Errorf("the teammate lists %+v", st)
	}
	eventually(t, "the claim to go on the lead's machine", func() bool { ok, _ := f.a.agentMay("src/a.go"); return ok })
	eventually(t, "the lead to list it cancelled", func() bool { return f.a.taskState(id).State == "cancelled" })
	if !cancelled(f, id, "gavriel953", "wrong approach") {
		t.Error("the room has no CANCELLED from the teammate")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("the worktree is gone: %v", err)
	}
	if _, err := f.b.call(Request{Method: MethodTaskCancel, Task: id}); err == nil || !strings.Contains(err.Error(), "is cancelled; only a running or checkpointed task") {
		t.Errorf("a second cancel: %v", err)
	}
	if _, err := f.b.call(Request{Method: MethodTaskCancel, Task: "t-none"}); err == nil || !strings.Contains(err.Error(), "no task t-none") {
		t.Errorf("an unknown task: %v", err)
	}
}

func TestLeadCancelsASentTask(t *testing.T) {
	f := newCrossFixture(t)
	waiting, err := f.a.task("gavriel953", "Later.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the lead to list it", func() bool { return f.a.taskState(waiting.TaskID).State == "dispatched" })
	if _, err := f.a.call(Request{Method: MethodTaskCancel, Task: waiting.TaskID}); err == nil || !strings.Contains(err.Error(), "is dispatched; only a running") {
		t.Errorf("the lead cancels a waiting task: %v", err)
	}

	id := runningTask(t, f)
	eventually(t, "the lead to see it running", func() bool { return f.a.taskState(id).State == "running" })
	resp, err := f.a.call(Request{Method: MethodTaskCancel, Task: id, Text: "plans changed"})
	if err != nil || !strings.Contains(resp.Note, "gavriel953's daemon stops its agent") {
		t.Fatalf("the lead cancels: %+v %v", resp, err)
	}
	eventually(t, "the teammate's agent to stop", func() bool { return agentStopped(f.b, id) })
	eventually(t, "the teammate to list it cancelled", func() bool { return f.b.taskState(id).State == "cancelled" })
	eventually(t, "the claim to go", func() bool { ok, _ := f.a.agentMay("src/a.go"); return ok })
	if st := f.a.taskState(id); st.State != "cancelled" || st.Detail != "plans changed" {
		t.Errorf("the lead lists %+v", st)
	}
	if !cancelled(f, id, "RM1338", "plans changed") {
		t.Error("the room has no CANCELLED from the lead")
	}
	// A replayed room log does not cancel anything twice or fail.
	f.b.restart()
	if st := f.b.taskState(id); st.State != "cancelled" {
		t.Errorf("after a restart the teammate lists %+v", st)
	}
}

func TestMemberCancelsTheirOwnTask(t *testing.T) {
	f := newCrossFixture(t)
	resp, err := f.b.call(Request{Method: MethodTaskStart, Repo: f.b.repo, Text: "Try a new parser.", Patterns: []string{"src/parse/**"}})
	if err != nil {
		t.Fatal(err)
	}
	id := resp.TaskID
	eventually(t, "the lead to see the claim", func() bool { ok, _ := f.a.agentMay("src/parse/x.go"); return !ok })
	// The lead lists the teammate's task with its text and state.
	eventually(t, "the lead to list the task", func() bool { return f.a.taskState(id).State == "running" })
	if got := f.a.taskState(id); !got.Teammate || got.Author != "gavriel953" || got.Text != "Try a new parser." {
		t.Errorf("the lead's view: %+v", got)
	}
	if _, err := f.a.call(Request{Method: MethodTaskCancel, Task: id}); err == nil || !strings.Contains(err.Error(), "no task") {
		t.Errorf("the lead cancels a teammate's own task: %v", err)
	}
	if _, err := f.b.call(Request{Method: MethodTaskCancel, Task: id}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the claim to go", func() bool { ok, _ := f.a.agentMay("src/parse/x.go"); return ok })
	if st := f.b.taskState(id); st.State != "cancelled" {
		t.Errorf("listed as %+v", st)
	}
	if !cancelled(f, id, "gavriel953", "") {
		t.Error("the room has no CANCELLED")
	}
	eventually(t, "the lead to see it cancelled", func() bool { return f.a.taskState(id).State == "cancelled" })
}
