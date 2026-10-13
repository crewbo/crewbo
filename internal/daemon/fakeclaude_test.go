package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the machines' claude (Config.ClaudeBin): run
// as "claude -p ..." or "claude auth status" it is a fake agent, enabled
// by CREWBO_FAKE_CLAUDE naming a directory; never the test suite again.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "-p" || os.Args[1] == "auth" || os.Args[1] == "exec" || os.Args[1] == "login") {
		dir := os.Getenv("CREWBO_FAKE_CLAUDE")
		if dir == "" {
			fmt.Fprintln(os.Stderr, "fake claude not enabled in this test")
			os.Exit(3)
		}
		os.Exit(fakeClaudeMain(dir))
	}
	os.Exit(m.Run())
}

// fakeRun is how the fake agent was started.
type fakeRun struct {
	Args    []string
	Cwd     string
	Stdin   string
	Session string
	APIKey  bool
}

// fakeClaudeMain answers claude auth status and codex login status
// (signed out when dir/signed-out exists), or records a run of either in
// dir/run-<pid>.json, writes agent.txt with the contents of dir/write when
// that exists (plus a line naming the run when dir/write-unique exists), then, when dir/hold exists, waits for dir/exit and exits
// with the code in it (a failing codex prints turn.failed first).
func fakeClaudeMain(dir string) int {
	_, signedOut := os.Stat(filepath.Join(dir, "signed-out"))
	switch os.Args[1] {
	case "auth": // claude auth status
		fmt.Printf(`{"loggedIn": %v}`, signedOut != nil)
		return 0
	case "login": // codex login status
		if signedOut == nil {
			fmt.Println("Not logged in")
			return 1
		}
		return 0
	}
	run := fakeRun{Args: os.Args[1:], APIKey: os.Getenv("ANTHROPIC_API_KEY") != ""}
	for i, a := range os.Args {
		if a == "--session-id" && i+1 < len(os.Args) {
			run.Session = os.Args[i+1]
		}
	}
	b, _ := io.ReadAll(os.Stdin)
	run.Stdin = string(b)
	run.Cwd, _ = os.Getwd()
	out, _ := json.Marshal(run)
	_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("run-%d.json", os.Getpid())), out, 0o600)
	if b, err := os.ReadFile(filepath.Join(dir, "write")); err == nil { // the agent's work
		if _, err := os.Stat(filepath.Join(dir, "write-unique")); err == nil {
			b = fmt.Appendf(b, "run %d\n", os.Getpid()) // differs per run
		}
		_ = os.WriteFile("agent.txt", b, 0o600)
	}
	if _, err := os.Stat(filepath.Join(dir, "hold")); err != nil {
		return 0
	}
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(dir, "exit")); err == nil {
			code, _ := strconv.Atoi(strings.TrimSpace(string(b)))
			if code != 0 && os.Args[1] == "exec" { // as codex-cli 0.161.0 reports a failed turn
				fmt.Println(`{"type":"turn.failed","error":{"message":"{\"type\":\"error\",\"status\":400,\"error\":{\"message\":\"The 'x' model is not supported.\"}}"}}`)
			}
			return code
		}
	}
	return 9
}

// fakeClaude enables the fake agent for this test and returns its
// directory.
func fakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CREWBO_FAKE_CLAUDE", dir)
	return dir
}

// fakeRuns returns the runs the fake agent recorded.
func fakeRuns(t *testing.T, dir string) []fakeRun {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "run-*.json"))
	var out []fakeRun
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // test file
		if err != nil {
			t.Fatal(err)
		}
		var r fakeRun
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func touch(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
