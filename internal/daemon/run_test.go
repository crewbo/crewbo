package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
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
	want := []crewbov1.TaskState{crewbov1.TaskState_TASK_STATE_APPROVED, crewbov1.TaskState_TASK_STATE_RUNNING, crewbov1.TaskState_TASK_STATE_FINISHED}
	eventually(t, "APPROVED, RUNNING and FINISHED in the room log", func() bool {
		var states []crewbov1.TaskState
		for _, ev := range f.statuses() {
			states = append(states, ev.GetTaskStatusChanged().GetStatus().GetState())
		}
		return slices.Equal(states, want)
	})
	eventually(t, "the lead to see the run finish", func() bool { return f.a.taskState(sent.TaskID).State == "finished" })
	// The daemon refuses moves the state machine does not allow.
	ctx := context.Background()
	for _, to := range []string{"running", "approved", "dispatched", "failed"} {
		if f.b.run.d.setTaskState(ctx, sent.TaskID, to, "") {
			t.Errorf("a finished task moved to %s", to)
		}
	}
	if st := f.b.taskState(sent.TaskID).State; st != "finished" {
		t.Errorf("state after refused moves: %s", st)
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
	eventually(t, "the lead to see the failure and why", func() bool {
		lt := f.a.taskState(sent.TaskID)
		return lt.Sent && lt.State == "failed" && lt.Detail == "Claude Code exit status 3"
	})
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

// remoteFor gives m's clone a bare origin (standing in for GitHub) and a
// git identity of its own, and returns the bare repository.
func (m *machine) remoteFor(t *testing.T, identity bool) string {
	t.Helper()
	bare := t.TempDir()
	gitOut(t, bare, "init", "-q", "--bare", "-b", "main")
	m.useRemote(t, bare, identity)
	return bare
}

// useRemote makes bare m's origin, with a git identity when asked.
func (m *machine) useRemote(t *testing.T, bare string, identity bool) {
	t.Helper()
	gitOut(t, m.repo, "remote", "add", "origin", bare)
	if identity {
		gitOut(t, m.repo, "config", "user.name", "Gavriel")
		gitOut(t, m.repo, "config", "user.email", "gavriel@example.com")
	}
}

// M5-01: a run that ends cleanly with changes is committed as the member
// and pushed to refs/crewbo/<member>/<task id>; the lead sees it
// checkpointed. A run with nothing to commit stays finished.
func TestCleanRunIsCheckpointed(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull) // only the clone's own identity counts
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "work from the agent\n")
	f.b.fetchFrom(f.a)
	bare := f.b.remoteFor(t, true)
	sent, err := f.a.task("gavriel953", "Write agent.txt.", []string{"agent.txt"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	if _, err := f.b.approve(sent.TaskID, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the checkpoint", func() bool { return f.b.taskState(sent.TaskID).State == "checkpointed" })
	ref := "refs/crewbo/gavriel953/" + sent.TaskID
	commit := gitOut(t, bare, "rev-parse", ref)
	if d := f.b.taskState(sent.TaskID).Detail; d != ref+" at "+commit[:12] {
		t.Errorf("detail %q", d)
	}
	if got := gitOut(t, bare, "show", ref+":agent.txt"); got != "work from the agent" {
		t.Errorf("agent.txt in the checkpoint = %q", got)
	}
	if author := gitOut(t, bare, "log", "-1", "--format=%an <%ae>", ref); author != "Gavriel <gavriel@example.com>" {
		t.Errorf("author %q", author)
	}
	if parent := gitOut(t, bare, "rev-parse", ref+"^"); parent != gitOut(t, f.a.repo, "rev-parse", "HEAD") {
		t.Errorf("the checkpoint does not sit on the task's base commit")
	}
	// The room log carries the ref and commit for other machines (M5-02).
	eventually(t, "the CHECKPOINTED status with its ref", func() bool {
		for _, ev := range f.statuses() {
			st := ev.GetTaskStatusChanged().GetStatus()
			if st.GetState() == crewbov1.TaskState_TASK_STATE_CHECKPOINTED {
				return st.GetRef() == ref && st.GetCommit() == commit
			}
		}
		return false
	})
	eventually(t, "the lead to see the checkpoint", func() bool {
		lt := f.a.taskState(sent.TaskID)
		return lt.State == "checkpointed" && lt.Detail == ref+" at "+commit[:12]
	})
}

// Without a git identity nothing is committed under an invented name: the
// run is finished and says why, and the work stays in the worktree.
func TestCheckpointNeedsAGitIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "work\n")
	f.b.fetchFrom(f.a)
	bare := f.b.remoteFor(t, false)
	sent, err := f.a.task("gavriel953", "Write agent.txt.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	if _, err := f.b.approve(sent.TaskID, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the run to end", func() bool { return f.b.taskState(sent.TaskID).State == "finished" })
	wt := filepath.Join(f.b.paths.Data, "worktrees", sent.TaskID)
	if d := f.b.taskState(sent.TaskID).Detail; !strings.Contains(d, "user.name and user.email are not set") || !strings.Contains(d, "the work is kept in "+wt) {
		t.Errorf("detail %q", d)
	}
	if b, err := os.ReadFile(filepath.Join(wt, "agent.txt")); err != nil || string(b) != "work\n" {
		t.Errorf("the agent's work is gone: %q %v", b, err)
	}
	if refs := gitOut(t, bare, "for-each-ref"); refs != "" {
		t.Errorf("something was pushed: %s", refs)
	}
}

// A remote that refuses refs/crewbo/ gets the checkpoint as the branch
// wip/<member>/<task id> instead (ADR 0005, U-06 fallback).
func TestCheckpointFallsBackToAWipBranch(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "work\n")
	f.b.fetchFrom(f.a)
	bare := f.b.remoteFor(t, true)
	// A server-side hook that refuses custom refs, as some hosts might.
	hook := filepath.Join(bare, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nwhile read old new ref; do case \"$ref\" in refs/crewbo/*) echo refused >&2; exit 1;; esac; done\n"), 0o700); err != nil { //nolint:gosec // test hook must be executable
		t.Fatal(err)
	}
	sent, err := f.a.task("gavriel953", "Write agent.txt.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	if _, err := f.b.approve(sent.TaskID, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the checkpoint", func() bool { return f.b.taskState(sent.TaskID).State == "checkpointed" })
	ref := "refs/heads/wip/gavriel953/" + sent.TaskID
	if d := f.b.taskState(sent.TaskID).Detail; !strings.HasPrefix(d, ref+" at ") {
		t.Errorf("detail %q", d)
	}
	if got := gitOut(t, bare, "show", ref+":agent.txt"); got != "work" {
		t.Errorf("agent.txt on the wip branch = %q", got)
	}
}

// M5-02: conflict warnings in the room log reach every member's task list:
// the latest warning per pair of tasks counts, and one with no paths means
// the pair merges cleanly again.
func TestConflictWarningsAreListed(t *testing.T) {
	f := newCrossFixture(t)
	sent, err := f.a.task("gavriel953", "Work on the parser.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	roomID := f.checkFor("gavriel953").RoomID
	log := server.NewLog(f.db.SQL())
	for _, w := range []*crewbov1.ConflictWarning{
		{TaskId: sent.TaskID, OtherTaskId: "task-x", Paths: []string{"src/a.go"}},
		{TaskId: "task-y", OtherTaskId: sent.TaskID, Paths: []string{"src/b.go", "src/c.go"}},
		{TaskId: sent.TaskID, OtherTaskId: "task-x"}, // x now merges cleanly
	} {
		w.RoomId = roomID
		ev := &crewbov1.Event{ActorMemberId: "gavriel953", Body: &crewbov1.Event_ConflictWarning{ConflictWarning: w}}
		if _, err := log.Append(context.Background(), roomID, ev, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	f.a.restart() // replay the log, including the warnings written above
	f.b.restart()
	want := []Conflict{{Task: "task-y", Paths: []string{"src/b.go", "src/c.go"}}}
	for _, m := range []*machine{f.a, f.b} {
		eventually(t, "the conflict on "+m.paths.Data, func() bool {
			return reflect.DeepEqual(m.taskState(sent.TaskID).Conflicts, want)
		})
	}
}

// M5-02: when a checkpoint conflicts with another task's latest checkpoint,
// the room is warned with the conflicting paths, and every member sees it.
func TestConflictingCheckpointsAreWarned(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "agent work\n")
	touch(t, filepath.Join(dir, "write-unique"), "")
	f.b.fetchFrom(f.a)
	// One shared remote, as the team's GitHub repository: the lead's
	// checkpoint reaches the teammate's machine only through it.
	bare := f.a.remoteFor(t, true)
	f.b.useRemote(t, bare, true)
	run := func(on *machine, to, text string) string {
		t.Helper()
		sent, err := f.a.task(to, text, nil, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "the task", func() bool { return on.taskState(sent.TaskID).State == "dispatched" })
		if _, err := on.approve(sent.TaskID, false); err != nil {
			t.Fatal(err)
		}
		eventually(t, "its checkpoint in the room", func() bool {
			for _, ev := range f.statuses() {
				if st := ev.GetTaskStatusChanged().GetStatus(); st.GetTaskId() == sent.TaskID && st.GetState() == crewbov1.TaskState_TASK_STATE_CHECKPOINTED {
					return true
				}
			}
			return false
		})
		return sent.TaskID
	}
	one := run(f.a, "RM1338", "First version of agent.txt.") // the lead's own task, on the lead's machine
	if n := len(f.warnings()); n != 0 {
		t.Errorf("a first checkpoint with nothing to compare sent %d warnings", n)
	}
	two := run(f.b, "gavriel953", "Second version of agent.txt.") // both add agent.txt with different contents
	eventually(t, "the warning", func() bool { return len(f.warnings()) == 1 })
	w := f.warnings()[0].GetConflictWarning()
	if w.GetTaskId() != two || w.GetOtherTaskId() != one || !slices.Equal(w.GetPaths(), []string{"agent.txt"}) || w.GetCommit() == "" || w.GetOtherCommit() == "" {
		t.Errorf("warning %v", w)
	}
	// The lead lists both tasks (one theirs, one sent); the teammate lists
	// their own, and each shows the conflict with the other task.
	eventually(t, "the conflict on the lead's machine", func() bool {
		return reflect.DeepEqual(f.a.taskState(one).Conflicts, []Conflict{{Task: two, Paths: []string{"agent.txt"}}}) &&
			reflect.DeepEqual(f.a.taskState(two).Conflicts, []Conflict{{Task: one, Paths: []string{"agent.txt"}}})
	})
	eventually(t, "the conflict on the teammate's machine", func() bool {
		return reflect.DeepEqual(f.b.taskState(two).Conflicts, []Conflict{{Task: one, Paths: []string{"agent.txt"}}})
	})
}

// warnings returns the conflict warnings in the room log.
func (f *crossFixture) warnings() []*crewbov1.Event {
	var out []*crewbov1.Event
	for _, ev := range f.roomEvents() {
		if ev.GetConflictWarning() != nil {
			out = append(out, ev)
		}
	}
	return out
}

// Checkpoints that merge cleanly send no warning.
func TestCleanCheckpointsSendNoWarning(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "the same work\n") // identical additions merge cleanly
	f.b.fetchFrom(f.a)
	f.b.remoteFor(t, true)
	var ids []string
	for _, text := range []string{"One.", "Two."} {
		sent, err := f.a.task("gavriel953", text, nil, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
		if _, err := f.b.approve(sent.TaskID, false); err != nil {
			t.Fatal(err)
		}
		eventually(t, "its checkpoint", func() bool { return f.a.taskState(sent.TaskID).State == "checkpointed" })
		ids = append(ids, sent.TaskID)
	}
	// A third, later event proves the second check had time to send.
	marker, err := f.a.task("gavriel953", "Marker.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the marker", func() bool { return f.b.taskState(marker.TaskID).ID != "" })
	if w := f.warnings(); len(w) != 0 {
		t.Errorf("clean checkpoints sent warnings: %v", w)
	}
	if c := f.b.taskState(ids[1]).Conflicts; len(c) != 0 {
		t.Errorf("conflicts listed: %v", c)
	}
}

// M5-04: the assignee merges their checkpointed task into main through the
// conflict check (ADR 0014): a conflict with main or with another task's
// checkpoint is refused; a clean merge is pushed as a merge commit; a
// merged task no longer counts in later conflict checks.
func TestMemberMergesTheirTask(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "agent work\n")
	touch(t, filepath.Join(dir, "write-unique"), "")
	f.b.fetchFrom(f.a)
	bare := f.a.remoteFor(t, true)
	f.b.useRemote(t, bare, true)
	gitOut(t, f.a.repo, "push", "-q", "origin", "HEAD:refs/heads/main") // the team's main
	base := gitOut(t, bare, "rev-parse", "main")
	run := func(text string) string {
		t.Helper()
		sent, err := f.a.task("gavriel953", text, nil, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
		if _, err := f.b.approve(sent.TaskID, false); err != nil {
			t.Fatal(err)
		}
		eventually(t, "its checkpoint in the room", func() bool { return f.a.taskState(sent.TaskID).State == "checkpointed" })
		return sent.TaskID
	}
	merge := func(id string, despite ...string) (Response, error) {
		return f.b.call(Request{Method: MethodTaskMerge, Task: id, Despite: despite})
	}

	one := run("First version of agent.txt.")
	two := run("Second version of agent.txt.") // conflicts with one
	// Two unmerged tasks conflict: each merge is refused until the member
	// confirms, so whoever goes first decides (ADR 0015).
	if _, err := merge(two); err == nil || !strings.Contains(err.Error(), "conflicts with task "+one+" on agent.txt") ||
		!strings.Contains(err.Error(), "crewbo task merge "+two+" --despite "+one) {
		t.Errorf("merging over another task's checkpoint: %v", err)
	}
	if _, err := merge(one); err == nil || !strings.Contains(err.Error(), "--despite "+two) {
		t.Errorf("merging the other one without confirming: %v", err)
	}
	if _, err := merge(one, "some-other-task"); err == nil || !strings.Contains(err.Error(), "--despite "+two) {
		t.Errorf("confirming an unrelated task let the merge through: %v", err)
	}
	resp, err := merge(one, two)
	if err != nil || !strings.HasPrefix(resp.Note, "Merged into main as ") {
		t.Fatalf("merge: %+v %v", resp, err)
	}
	merged := gitOut(t, bare, "rev-parse", "main")
	checkpoint := gitOut(t, bare, "rev-parse", "refs/crewbo/gavriel953/"+one)
	if parents := gitOut(t, bare, "rev-list", "--parents", "-n", "1", merged); parents != merged+" "+base+" "+checkpoint {
		t.Errorf("main is %q, want a merge of %s and %s", parents, base, checkpoint)
	}
	if author := gitOut(t, bare, "log", "-1", "--format=%an", merged); author != "Gavriel" {
		t.Errorf("merge author %q", author)
	}
	if st := f.b.taskState(one); st.State != "merged" || st.Detail != "refs/heads/main at "+merged[:12] {
		t.Errorf("after merge: %s %q", st.State, st.Detail)
	}
	eventually(t, "the lead to see it merged", func() bool { return f.a.taskState(one).State == "merged" })
	if _, err := merge(one); err == nil || !strings.Contains(err.Error(), "only a checkpointed task can be merged") {
		t.Errorf("merging twice: %v", err)
	}
	// main now has one's agent.txt, so two conflicts with main, and no
	// confirmation gets past that.
	if _, err := merge(two, one); err == nil || !strings.Contains(err.Error(), "conflicts with main on agent.txt") {
		t.Errorf("merging over main: %v", err)
	}
	if st := f.b.taskState(two).State; st != "checkpointed" {
		t.Errorf("a refused merge changed the state to %s", st)
	}
	// A merged task no longer counts: a third conflicting checkpoint is
	// compared with two only.
	warningsBefore := len(f.warnings())
	three := run("Third version of agent.txt.")
	eventually(t, "the third check", func() bool { return len(f.warnings()) == warningsBefore+1 })
	if w := f.warnings()[len(f.warnings())-1].GetConflictWarning(); w.GetTaskId() != three || w.GetOtherTaskId() != two {
		t.Errorf("the third task was compared with %s, want %s only", w.GetOtherTaskId(), two)
	}
}

// The lead may merge a task they sent once its assignee checkpointed it
// (ADR 0014): the lead's machine fetches the checkpoint, merges it as the
// lead, and every member, the assignee included, sees it merged.
func TestLeadMergesASentTask(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	dir := fakeClaude(t)
	touch(t, filepath.Join(dir, "write"), "agent work\n")
	f.b.fetchFrom(f.a)
	bare := f.a.remoteFor(t, true)
	gitOut(t, f.a.repo, "config", "user.name", "Ronel")
	f.b.useRemote(t, bare, true)
	gitOut(t, f.a.repo, "push", "-q", "origin", "HEAD:refs/heads/main")
	sent, err := f.a.task("gavriel953", "Write agent.txt.", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the lead to list the sent task", func() bool { return f.a.taskState(sent.TaskID).Sent })
	if _, err := f.a.call(Request{Method: MethodTaskMerge, Task: sent.TaskID}); err == nil || !strings.Contains(err.Error(), "is dispatched; only a checkpointed task can be merged") {
		t.Errorf("merging before a checkpoint: %v", err)
	}
	eventually(t, "the task", func() bool { return f.b.taskState(sent.TaskID).State == "dispatched" })
	if _, err := f.b.approve(sent.TaskID, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the checkpoint on the lead's machine", func() bool { return f.a.taskState(sent.TaskID).State == "checkpointed" })
	// Merge statuses from anyone but the task's lead are ignored, on the
	// assignee's own row too.
	roomID := f.checkFor("gavriel953").RoomID
	for _, st := range []crewbov1.TaskState{crewbov1.TaskState_TASK_STATE_MERGING, crewbov1.TaskState_TASK_STATE_MERGED} {
		ev := &crewbov1.Event{ActorMemberId: "mallory", Body: &crewbov1.Event_TaskStatusChanged{TaskStatusChanged: &crewbov1.TaskStatusChanged{
			Status: &crewbov1.TaskStatus{RoomId: roomID, TaskId: sent.TaskID, State: st},
		}}}
		if _, err := server.NewLog(f.db.SQL()).Append(context.Background(), roomID, ev, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	marker, err := f.a.task("gavriel953", "Marker.", nil, time.Hour) // a live event after them makes B replay the gap
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the marker", func() bool { return f.b.taskState(marker.TaskID).ID != "" })
	if st := f.b.taskState(sent.TaskID).State; st != "checkpointed" {
		t.Errorf("a forged merge moved the assignee's task to %s", st)
	}
	// In a lead-only room the assignee may not merge; the lead may.
	if _, err := server.NewRooms(f.db.SQL(), time.Now).SetMergeMode(context.Background(), auth.User{ID: 1, Login: "RM1338"}, roomID, server.MergeLead); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.call(Request{Method: MethodTaskMerge, Task: sent.TaskID}); err == nil || !strings.Contains(err.Error(), "only the lead (RM1338) merges tasks") {
		t.Errorf("assignee merge in a lead-only room: %v", err)
	}
	resp, err := f.a.call(Request{Method: MethodTaskMerge, Task: sent.TaskID})
	if err != nil || !strings.HasPrefix(resp.Note, "Merged into main as ") {
		t.Fatalf("lead merge: %+v %v", resp, err)
	}
	merged := gitOut(t, bare, "rev-parse", "main")
	if author := gitOut(t, bare, "log", "-1", "--format=%an", merged); author != "Ronel" {
		t.Errorf("merge author %q, want the lead", author)
	}
	if got := gitOut(t, bare, "show", "main:agent.txt"); got != "agent work" {
		t.Errorf("agent.txt on main = %q", got)
	}
	eventually(t, "the lead's list to say merged", func() bool { return f.a.taskState(sent.TaskID).State == "merged" })
	eventually(t, "the assignee's own task to say merged", func() bool { return f.b.taskState(sent.TaskID).State == "merged" })
	if _, err := f.b.call(Request{Method: MethodTaskMerge, Task: sent.TaskID}); err == nil || !strings.Contains(err.Error(), "only a checkpointed task can be merged") {
		t.Errorf("the assignee merging it again: %v", err)
	}
}

// M5-07: a member starts a task of their own, with no lead or approval: a
// worktree on its own branch, its paths claimed for whatever agent works
// there, the room told it is running. A restart keeps that mapping.
func TestMemberStartsTheirOwnTask(t *testing.T) {
	f := newCrossFixture(t)
	resp, err := f.b.call(Request{Method: MethodTaskStart, Repo: f.b.repo, Text: "Build the login page.", Patterns: []string{"src/auth/**"}})
	if err != nil || resp.TaskID == "" || !strings.Contains(resp.Note, "Crewbo checkpoints your work") {
		t.Fatalf("start: %+v %v", resp, err)
	}
	id := resp.TaskID
	wt := filepath.Join(f.b.paths.Data, "worktrees", id)
	if br := gitOut(t, wt, "branch", "--show-current"); br != "crewbo/"+id {
		t.Errorf("branch %q", br)
	}
	if head := gitOut(t, wt, "rev-parse", "HEAD"); head != gitOut(t, f.b.repo, "rev-parse", "HEAD") {
		t.Errorf("the worktree does not start at the member's current commit")
	}
	if own := f.b.taskState(id); own.State != "running" || own.Author != "gavriel953" || own.Text != "Build the login page." {
		t.Errorf("listed as %+v", own)
	}
	eventually(t, "the lead to see the claim", func() bool { ok, _ := f.a.agentMay("src/auth/login.go"); return !ok })
	if _, holder := f.a.agentMay("src/auth/login.go"); holder.Holder.MemberID != "gavriel953" || holder.TaskID != id {
		t.Errorf("holder %+v", holder)
	}
	decide := func(session, repo string) bool {
		t.Helper()
		r, err := f.b.call(Request{Method: MethodDecide, Session: session, Agent: "claude-code", Repo: repo, Paths: []string{"src/auth/login.go"}})
		if err != nil {
			t.Fatal(err)
		}
		return r.Allow
	}
	if !decide("interactive-1", wt) {
		t.Error("the member's agent in the task's worktree is blocked by its own claim")
	}
	if decide("interactive-1", f.b.repo) {
		t.Error("an agent outside the worktree may edit the task's file")
	}
	eventually(t, "RUNNING in the room", func() bool {
		for _, ev := range f.statuses() {
			if st := ev.GetTaskStatusChanged().GetStatus(); st.GetTaskId() == id && st.GetState() == crewbov1.TaskState_TASK_STATE_RUNNING {
				return ev.GetActorMemberId() == "gavriel953"
			}
		}
		return false
	})
	f.b.restart()
	eventually(t, "the claim after the restart", func() bool { ok, _ := f.b.agentMay("src/auth/login.go"); return !ok })
	if !decide("interactive-2", wt) {
		t.Error("after a restart the worktree is no longer the task's session")
	}
	// A claim someone else holds refuses the start, and nothing is left behind.
	if _, err := f.a.claim("docs/**"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the lead's claim", func() bool { ok, _ := f.b.agentMay("docs/a.md"); return !ok })
	if _, err := f.b.call(Request{Method: MethodTaskStart, Repo: f.b.repo, Text: "Docs.", Patterns: []string{"docs/a.md"}}); err == nil || !strings.Contains(err.Error(), "claim the task's paths") {
		t.Errorf("start over a held claim: %v", err)
	}
	if n := len(f.b.tasks()); n != 1 {
		t.Errorf("a refused start left a task: %d tasks", n)
	}
	if branches := gitOut(t, f.b.repo, "branch", "--list", "crewbo/*"); strings.Count(branches, "crewbo/") != 1 {
		t.Errorf("a refused start left a branch: %q", branches)
	}
}

// M5-07: an own task is checkpointed on request, and when its agent
// finishes a reply at most every ownEvery; a new prompt after a checkpoint
// means it is running again; merging it releases its claims.
func TestOwnTaskCheckpointsAndMerges(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	bare := f.a.remoteFor(t, true)
	gitOut(t, f.a.repo, "push", "-q", "origin", "HEAD:refs/heads/main")
	f.b.fetchFrom(f.a)
	gitOut(t, f.b.repo, "reset", "-q", "--hard", "FETCH_HEAD") // the member's clone of main
	f.b.useRemote(t, bare, true)
	started, err := f.b.call(Request{Method: MethodTaskStart, Repo: f.b.repo, Text: "Own work.", Patterns: []string{"agent.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	id := started.TaskID
	wt := filepath.Join(f.b.paths.Data, "worktrees", id)
	ref := "refs/crewbo/gavriel953/" + id
	work := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(wt, "agent.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	event := func(name string) {
		t.Helper()
		if _, err := f.b.call(Request{Method: MethodEvent, Session: "interactive", Agent: "claude-code", Repo: wt, Event: name}); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := func() (Response, error) { return f.b.call(Request{Method: MethodTaskCheckpoint, Task: id}) }

	work("first\n")
	resp, err := checkpoint()
	if err != nil || !strings.HasPrefix(resp.Note, "Checkpointed as "+ref+" at ") {
		t.Fatalf("checkpoint: %+v %v", resp, err)
	}
	first := gitOut(t, bare, "rev-parse", ref)
	if st := f.b.taskState(id).State; st != "checkpointed" {
		t.Errorf("state %s", st)
	}
	if resp, err := checkpoint(); err != nil || resp.Note != "Nothing new since the last checkpoint." {
		t.Errorf("checkpoint with nothing new: %+v %v", resp, err)
	}
	event("prompt")
	eventually(t, "the task to run again", func() bool { return f.b.taskState(id).State == "running" })

	work("second\n")
	event("stop") // within ownEvery of the last checkpoint: not yet
	time.Sleep(400 * time.Millisecond)
	if got := gitOut(t, bare, "rev-parse", ref); got != first {
		t.Error("an automatic checkpoint ran within the interval")
	}
	event("stop") // the interval has passed
	eventually(t, "the automatic checkpoint", func() bool { return gitOut(t, bare, "rev-parse", ref) != first })
	if got := gitOut(t, bare, "show", ref+":agent.txt"); got != "second" {
		t.Errorf("the automatic checkpoint holds %q", got)
	}
	eventually(t, "checkpointed again", func() bool { return f.b.taskState(id).State == "checkpointed" })

	if resp, err := f.b.call(Request{Method: MethodTaskMerge, Task: id}); err != nil || !strings.HasPrefix(resp.Note, "Merged into main") {
		t.Fatalf("merge: %+v %v", resp, err)
	}
	if got := gitOut(t, bare, "show", "main:agent.txt"); got != "second" {
		t.Errorf("main has %q", got)
	}
	eventually(t, "the claim to be released", func() bool { ok, _ := f.a.agentMay("agent.txt"); return ok })
	if _, ok := f.b.run.d.runSession(wt); ok {
		t.Error("the merged task still owns its worktree's sessions")
	}
}

// A conflict that a later checkpoint resolves is cleared for everyone.
func TestResolvedConflictIsCleared(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f := newCrossFixture(t)
	bare := f.a.remoteFor(t, true)
	f.b.fetchFrom(f.a)
	gitOut(t, f.b.repo, "reset", "-q", "--hard", "FETCH_HEAD")
	f.b.useRemote(t, bare, true)
	start := func(m *machine) (string, string) {
		t.Helper()
		r, err := m.call(Request{Method: MethodTaskStart, Repo: m.repo, Text: "Own work."})
		if err != nil {
			t.Fatal(err)
		}
		return r.TaskID, filepath.Join(m.paths.Data, "worktrees", r.TaskID)
	}
	save := func(m *machine, id, wt, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(wt, "agent.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := m.call(Request{Method: MethodTaskCheckpoint, Task: id}); err != nil {
			t.Fatal(err)
		}
	}
	leadTask, leadWT := start(f.a)
	mateTask, mateWT := start(f.b)
	save(f.a, leadTask, leadWT, "lead version\n")
	eventually(t, "the lead's checkpoint on the teammate's machine", func() bool {
		cs, err := f.b.run.d.otherCheckpoints(context.Background(), f.checkFor("gavriel953").RoomID, mateTask)
		return err == nil && len(cs) == 1
	})
	save(f.b, mateTask, mateWT, "mate version\n")
	eventually(t, "the conflict", func() bool { return len(f.b.taskState(mateTask).Conflicts) == 1 })
	save(f.b, mateTask, mateWT, "lead version\n") // now identical: merges cleanly
	eventually(t, "the conflict to clear on both machines", func() bool {
		return len(f.b.taskState(mateTask).Conflicts) == 0 && len(f.a.taskState(leadTask).Conflicts) == 0
	})
	if w := f.warnings(); len(w) != 2 || len(w[1].GetConflictWarning().GetPaths()) != 0 {
		t.Errorf("warnings %v", w)
	}
}
