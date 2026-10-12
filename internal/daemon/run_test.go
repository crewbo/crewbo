package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/server"
)

func (m *machine) taskState(id string) Task {
	for _, t := range m.tasks() {
		if t.ID == id {
			return t
		}
	}
	return Task{}
}

func (m *machine) approve(id string, keepKey bool) (Response, error) {
	return m.call(Request{Method: MethodTaskApprove, Task: id, KeepAPIKey: keepKey})
}

// roomEvents returns the room log.
func (f *crossFixture) roomEvents() []*crewbov1.Event {
	f.t.Helper()
	evs, err := server.NewLog(f.db.SQL()).Since(context.Background(), f.checkFor("gavriel953").RoomID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return evs
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output() //nolint:gosec // test
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// M4-04: approving runs Claude Code in a fresh worktree at the task's base
// commit, with the task's paths claimed for its session and renewed on the
// server while it runs, and released when it exits.
func TestApprovedTaskRunsClaudeInAWorktree(t *testing.T) {
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "hold"), "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-must-not-reach-the-agent")
	// Work the teammate's clone does not have yet.
	gitOut(t, f.a.repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "lead work")
	text := "Refactor the sync loop.\nKeep the tests green."
	sent, err := f.a.task("gavriel953", text, []string{"src/a.go"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })

	// Fixable problems leave the task waiting.
	if _, err := f.b.approve(sent.TaskID, false); err == nil || !strings.Contains(err.Error(), "does not have; run git fetch") {
		t.Errorf("approve without the base commit: %v", err)
	}
	f.b.fetchFrom(f.a)
	touch(t, filepath.Join(dir, "signed-out"), "")
	if _, err := f.b.approve(sent.TaskID, false); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("approve while signed out: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "signed-out")); err != nil {
		t.Fatal(err)
	}
	if st := f.b.taskState(sent.TaskID).State; st != "dispatched" {
		t.Fatalf("after refused approvals the task is %s", st)
	}

	resp, err := f.b.approve(sent.TaskID, false)
	if err != nil || !strings.Contains(resp.Note, "Claude Code is working in") {
		t.Fatalf("approve: %+v %v", resp, err)
	}
	eventually(t, "the agent to start", func() bool { return len(fakeRuns(t, dir)) == 1 })
	run := fakeRuns(t, dir)[0]
	wt := filepath.Join(f.b.paths.Data, "worktrees", sent.TaskID)
	if !slices.Equal(run.Args, []string{"-p", "--session-id", run.Session, "--permission-mode", "acceptEdits"}) || run.Session == "" {
		t.Errorf("args = %q", run.Args)
	}
	if run.Stdin != text || run.APIKey {
		t.Errorf("stdin %q, API key passed %v", run.Stdin, run.APIKey)
	}
	if RepoKey(run.Cwd) != RepoKey(f.b.repo) || mustEval(t, run.Cwd) != mustEval(t, wt) {
		t.Errorf("cwd = %q, want the worktree %q", run.Cwd, wt)
	}
	if head, base := gitOut(t, wt, "rev-parse", "HEAD"), gitOut(t, f.a.repo, "rev-parse", "HEAD"); head != base {
		t.Errorf("worktree at %s, task base %s", head, base)
	}
	if br := gitOut(t, wt, "branch", "--show-current"); br != "crewbo/"+sent.TaskID {
		t.Errorf("worktree branch %q", br)
	}
	if st := f.b.taskState(sent.TaskID).State; st != "running" {
		t.Errorf("state %q", st)
	}

	// The task's paths are the agent's: other agents here and on the lead's
	// machine are blocked; the agent's own hook calls, from the worktree,
	// are not.
	eventually(t, "the lead to see the task's claim", func() bool { ok, _ := f.a.agentMay("src/a.go"); return !ok })
	if _, holder := f.a.agentMay("src/a.go"); holder.Holder.MemberID != "gavriel953" || holder.TaskID != sent.TaskID {
		t.Errorf("holder on the lead's machine: %+v", holder)
	}
	if ok, _ := f.b.agentMay("src/a.go"); ok {
		t.Error("another agent on the teammate's machine may edit the task's file")
	}
	own, err := f.b.call(Request{Method: MethodDecide, Session: run.Session, Agent: "claude-code", Repo: wt, Paths: []string{"src/a.go"}})
	if err != nil || !own.Allow {
		t.Errorf("the task's own agent is blocked: %+v %v", own.BlockedBy, err)
	}
	eventually(t, "a claim renewal on the server", func() bool {
		for _, ev := range f.roomEvents() {
			if r := ev.GetClaimsRenewed(); r != nil && strings.HasSuffix(r.GetSessionId(), "/"+run.Session) {
				return true
			}
		}
		return false
	})

	touch(t, filepath.Join(dir, "exit"), "0")
	eventually(t, "the run to finish", func() bool { return f.b.taskState(sent.TaskID).State == "finished" })
	eventually(t, "the claim to be released everywhere", func() bool { ok, _ := f.a.agentMay("src/a.go"); return ok })
	if ok, _ := f.b.agentMay("src/a.go"); !ok {
		t.Error("the task's claim outlived the run on the teammate's machine")
	}
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Errorf("the worktree with the agent's work is gone: %v", err)
	}
	var states []crewbov1.TaskState
	for _, ev := range f.statuses() {
		states = append(states, ev.GetTaskStatusChanged().GetStatus().GetState())
	}
	if !slices.Equal(states, []crewbov1.TaskState{crewbov1.TaskState_TASK_STATE_APPROVED, crewbov1.TaskState_TASK_STATE_RUNNING}) {
		t.Errorf("statuses = %v", states)
	}
}

// A run that exits non-zero is failed and the room is told why; the member
// may opt in to keeping the API key.
func TestFailedRunIsReported(t *testing.T) {
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "hold"), "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-opted-in")
	f.b.fetchFrom(f.a)
	sent, err := f.a.task("gavriel953", "Break.", []string{"src/b.go"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	if _, err := f.b.approve(sent.TaskID, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the agent to start", func() bool { return len(fakeRuns(t, dir)) == 1 })
	if !fakeRuns(t, dir)[0].APIKey {
		t.Error("the opted-in API key did not reach the agent")
	}
	touch(t, filepath.Join(dir, "exit"), "3")
	eventually(t, "the run to fail", func() bool { return f.b.taskState(sent.TaskID).State == "failed" })
	if d := f.b.taskState(sent.TaskID).Detail; d != "Claude Code exit status 3" {
		t.Errorf("detail %q", d)
	}
	eventually(t, "FAILED in the room log", func() bool {
		for _, ev := range f.statuses() {
			if st := ev.GetTaskStatusChanged().GetStatus(); st.GetState() == crewbov1.TaskState_TASK_STATE_FAILED && st.GetDetail() == "Claude Code exit status 3" {
				return true
			}
		}
		return false
	})
	if ok, _ := f.b.agentMay("src/b.go"); !ok {
		t.Error("a failed run kept its claim")
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// M4-05: the member may run a task with Codex. Codex picks its own session
// id, so hook calls from the task's worktree count as the task's session.
func TestApprovedTaskRunsWithCodex(t *testing.T) {
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "hold"), "")
	t.Setenv("OPENAI_API_KEY", "sk-not-for-the-agent")
	f.b.fetchFrom(f.a)
	sent, err := f.a.task("gavriel953", "Port the parser.", []string{"src/c.go"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	if _, err := f.b.call(Request{Method: MethodTaskApprove, Task: sent.TaskID, Agent: "aider"}); err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("unknown agent: %v", err)
	}
	touch(t, filepath.Join(dir, "signed-out"), "")
	if _, err := f.b.call(Request{Method: MethodTaskApprove, Task: sent.TaskID, Agent: "codex"}); err == nil || !strings.Contains(err.Error(), "run codex login") {
		t.Errorf("approve while Codex is signed out: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "signed-out")); err != nil {
		t.Fatal(err)
	}
	resp, err := f.b.call(Request{Method: MethodTaskApprove, Task: sent.TaskID, Agent: "codex"})
	if err != nil || !strings.Contains(resp.Note, "Codex is working in") {
		t.Fatalf("approve: %+v %v", resp, err)
	}
	eventually(t, "Codex to start", func() bool { return len(fakeRuns(t, dir)) == 1 })
	run := fakeRuns(t, dir)[0]
	wt := filepath.Join(f.b.paths.Data, "worktrees", sent.TaskID)
	if len(run.Args) != 7 || run.Args[0] != "exec" || mustEval(t, run.Args[2]) != mustEval(t, wt) ||
		!slices.Equal(run.Args[3:], []string{"--sandbox", "workspace-write", "--json", "-"}) {
		t.Errorf("args = %q", run.Args)
	}
	if run.Stdin != "Port the parser." {
		t.Errorf("stdin %q", run.Stdin)
	}

	// Any session in the worktree is the task's; the same id elsewhere is not.
	if own, err := f.b.call(Request{Method: MethodDecide, Session: "codex-thread-1", Agent: "codex", Repo: wt, Paths: []string{"src/c.go"}}); err != nil || !own.Allow {
		t.Errorf("Codex in the task's worktree is blocked: %+v %v", own.BlockedBy, err)
	}
	if other, err := f.b.call(Request{Method: MethodDecide, Session: "codex-thread-1", Agent: "codex", Repo: f.b.repo, Paths: []string{"src/c.go"}}); err != nil || other.Allow {
		t.Errorf("the same session outside the worktree may edit the task's file: %v", err)
	}
	eventually(t, "the lead to see the claim", func() bool { ok, _ := f.a.agentMay("src/c.go"); return !ok })

	touch(t, filepath.Join(dir, "exit"), "1")
	eventually(t, "the run to fail", func() bool { return f.b.taskState(sent.TaskID).State == "failed" })
	if d := f.b.taskState(sent.TaskID).Detail; d != "Codex: The 'x' model is not supported." {
		t.Errorf("detail %q", d)
	}
	eventually(t, "the claim to be released", func() bool { ok, _ := f.a.agentMay("src/c.go"); return ok })
	if s, ok := f.b.run.d.runSession(wt); ok {
		t.Errorf("the finished run still owns its worktree's sessions (%s)", s)
	}
	if own, err := f.b.call(Request{Method: MethodDecide, Session: "codex-thread-2", Agent: "codex", Repo: wt, Paths: []string{"src/c.go"}}); err != nil || !own.Allow {
		t.Errorf("after the run: %+v %v", own, err)
	}
}
