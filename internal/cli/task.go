package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"

	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
)

const taskUsage = `Usage:
  crewbo task create --to LOGIN [--path PATH]... [--for DURATION] (TEXT... | --file FILE)
  crewbo task list
  crewbo task show ID
  crewbo task approve ID [--agent claude-code|codex] [--use-api-key]
  crewbo task decline ID [--reason TEXT]

create sends a signed task to a teammate's agent in this repository's room.
Only the room lead can send tasks. --path names the files or globs the task
may change (repeat it); --for is how long the teammate has to approve
(default 24h, at most 7 days); --file - reads the text from stdin.

list shows the tasks sent to you and, for a lead, the tasks you sent with
the state their assignee reported; show prints one in full. approve shows
the full task and asks you to type yes (only in an interactive terminal),
then runs your agent on it in a new git worktree, with the task's paths
claimed for it: Claude Code by default, or Codex with --agent codex (not
on Windows yet). API key variables (ANTHROPIC_API_KEY for Claude Code,
OPENAI_API_KEY and CODEX_API_KEY for Codex) are removed from the agent's
environment so it uses your plan; --use-api-key keeps them. decline tells the lead, with an optional reason. ID may be
any unique start of a task id.
`

// paths is a repeatable --path flag.
type pathList []string

func (p *pathList) String() string     { return strings.Join(*p, ",") }
func (p *pathList) Set(v string) error { *p = append(*p, v); return nil }

func runTask(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return write(stderr, ExitUsage, "%s", taskUsage)
	}
	switch args[0] {
	case "create":
		return taskCreate(ctx, args, stdin, stdout, stderr)
	case "list":
		if len(args) != 1 {
			return write(stderr, ExitUsage, "%s", taskUsage)
		}
		tasks, code := listTasks(ctx, stderr)
		if code != ExitOK {
			return code
		}
		if len(tasks) == 0 {
			return write(stdout, ExitOK, "No tasks.\n")
		}
		now := time.Now()
		for _, t := range tasks {
			_, _ = fmt.Fprintln(stdout, listLine(t, now))
		}
		return ExitOK
	case "show", "approve", "decline":
		return taskAnswer(ctx, args, stdin, stdout, stderr)
	}
	return write(stderr, ExitUsage, "%s", taskUsage)
}

// taskAnswer runs show, approve and decline.
func taskAnswer(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("task "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	reason := fs.String("reason", "", "why you decline (decline only)")
	useAPIKey := fs.Bool("use-api-key", false, "let the agent use API key variables (approve only)")
	agent := fs.String("agent", "", "claude-code (default) or codex (approve only)")
	rest, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return ExitUsage
	}
	if len(rest) != 1 || (*reason != "" && args[0] != "decline") || ((*useAPIKey || *agent != "") && args[0] != "approve") {
		return write(stderr, ExitUsage, "%s", taskUsage)
	}
	// Refuse before showing anything: an agent's shell has no terminal.
	if args[0] == "approve" && !interactive(stdin) {
		return write(stderr, ExitError, "crewbo: approve a task from an interactive terminal, where you can read it first\n")
	}
	tasks, code := listTasks(ctx, stderr)
	if code != ExitOK {
		return code
	}
	t, err := resolveTask(tasks, rest[0])
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	switch args[0] {
	case "show":
		printTask(stdout, t, time.Now())
		return ExitOK
	case "approve":
		printTask(stdout, t, time.Now())
		if !confirm(stdin, stdout) {
			return write(stdout, ExitError, "Not approved.\n")
		}
	}
	method := daemon.MethodTaskApprove
	if args[0] == "decline" {
		method = daemon.MethodTaskDecline
	}
	// Approving checks the repository and Claude Code and starts the agent.
	resp, code := callTaskDaemon(ctx, daemon.Request{Method: method, Task: t.ID, Text: *reason, KeepAPIKey: *useAPIKey, Agent: *agent}, stderr)
	if code != ExitOK {
		return code
	}
	return write(stdout, ExitOK, "Task %s %s. %s\n", t.ID, map[string]string{"approve": "approved", "decline": "declined"}[args[0]], resp.Note)
}

func taskCreate(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	to := fs.String("to", "", "the teammate's GitHub login")
	ttl := fs.Duration("for", 0, "how long the teammate has to approve (default 24h)")
	file := fs.String("file", "", "read the task text from FILE (- for stdin)")
	var owned pathList
	fs.Var(&owned, "path", "a file or glob the task may change (repeatable)")
	rest, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return ExitUsage
	}
	text := strings.Join(rest, " ")
	switch {
	case *file == "-":
		b, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
		if err != nil {
			return write(stderr, ExitError, "crewbo: read stdin: %v\n", err)
		}
		text = string(b)
	case *file != "":
		b, err := os.ReadFile(*file) //nolint:gosec // the member names the file
		if err != nil {
			return write(stderr, ExitError, "crewbo: %v\n", err)
		}
		text = string(b)
	}
	if *to == "" || strings.TrimSpace(text) == "" || (*file != "" && len(rest) > 0) {
		return write(stderr, ExitUsage, "%s", taskUsage)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	root, ok := hooks.RepoRoot(cwd)
	if !ok {
		return write(stderr, ExitError, "crewbo: run this inside a git repository\n")
	}
	var patterns []string
	for _, p := range owned {
		pat, err := toPattern(root, cwd, p)
		if err != nil {
			return write(stderr, ExitError, "crewbo: %s: %v\n", p, err)
		}
		patterns = append(patterns, pat)
	}
	resp, code := callTaskDaemon(ctx, daemon.Request{
		Method: daemon.MethodTaskCreate, Repo: root, Assignee: *to, Text: text,
		Patterns: patterns, TTLSeconds: int64(ttl.Seconds()),
	}, stderr)
	if code != ExitOK {
		return code
	}
	return write(stdout, ExitOK, "Task %s: %s\n", resp.TaskID, resp.Note)
}

// callTaskDaemon sends one request to the daemon; a failure is written to
// stderr and returned as an exit code.
func callTaskDaemon(ctx context.Context, req daemon.Request, stderr io.Writer) (daemon.Response, int) {
	p, err := paths()
	if err != nil {
		return daemon.Response{}, write(stderr, ExitError, "crewbo: %v\n", err)
	}
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	c, err := daemon.Dial(dctx, p)
	cancel()
	if err != nil {
		return daemon.Response{}, write(stderr, ExitError, "crewbo: the daemon is not running; start it with crewbo daemon\n")
	}
	defer func() { _ = c.Close() }()
	cctx, ccancel := context.WithTimeout(ctx, 60*time.Second)
	defer ccancel()
	resp, err := c.Call(cctx, req)
	if err != nil {
		return daemon.Response{}, write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return resp, ExitOK
}

func listTasks(ctx context.Context, stderr io.Writer) ([]daemon.Task, int) {
	resp, code := callTaskDaemon(ctx, daemon.Request{Method: daemon.MethodTaskList}, stderr)
	return resp.Tasks, code
}

// resolveTask finds the task whose id starts with prefix.
func resolveTask(tasks []daemon.Task, prefix string) (daemon.Task, error) {
	var found []daemon.Task
	for _, t := range tasks {
		if t.ID != "" && strings.HasPrefix(t.ID, prefix) {
			found = append(found, t)
		}
	}
	switch len(found) {
	case 0:
		return daemon.Task{}, fmt.Errorf("no task %s (crewbo task list shows yours)", prefix)
	case 1:
		return found[0], nil
	}
	return daemon.Task{}, fmt.Errorf("%s matches %d tasks; give more of the id", prefix, len(found))
}

// listLine is one task in crewbo task list.
func listLine(t daemon.Task, now time.Time) string {
	if t.ID == "" {
		return fmt.Sprintf("(rejected)  received %s: %s", t.Received.Format(time.DateTime), visible(t.Detail))
	}
	first, _, more := strings.Cut(t.Text, "\n")
	if r := []rune(first); len(r) > 60 {
		first, more = string(r[:60]), true
	}
	if more {
		first += " ..."
	}
	who := "from " + t.Author
	if t.Sent {
		who = "to " + t.Assignee
	}
	line := fmt.Sprintf("%s  %-10s  %s, %s  %s", t.ID, t.State, who, until(t.Expires, now), visible(first))
	if n := len(t.Conflicts); n > 0 {
		line += fmt.Sprintf("  [conflicts with %d task(s); crewbo task show %s]", n, t.ID[:min(13, len(t.ID))])
	}
	return line
}

// until describes an expiry relative to now.
func until(expires, now time.Time) string {
	if !expires.After(now) {
		return "expired " + expires.Format(time.DateTime)
	}
	return fmt.Sprintf("expires %s (in %s)", expires.Format(time.DateTime), expires.Sub(now).Round(time.Minute))
}

// printTask prints a task in full: its text verbatim, with control
// characters made visible so a terminal cannot hide or rewrite any of it.
func printTask(w io.Writer, t daemon.Task, now time.Time) {
	owned := "none"
	if len(t.OwnedPaths) > 0 {
		owned = strings.Join(t.OwnedPaths, ", ")
	}
	who := "From:     " + t.Author + " (room lead)"
	if t.Sent {
		who = "To:       " + t.Assignee + " (sent by you)"
	}
	_, _ = fmt.Fprintf(w, "Task:     %s\n%s\nState:    %s\nBase:     %s\nPaths:    %s\nCreated:  %s\nExpiry:   %s\n",
		t.ID, who, t.State, t.BaseCommit, visible(owned), t.Created.Format(time.DateTime), until(t.Expires, now))
	if t.Detail != "" {
		_, _ = fmt.Fprintf(w, "Reason:   %s\n", visible(t.Detail))
	}
	for _, c := range t.Conflicts {
		_, _ = fmt.Fprintf(w, "Conflict: with task %s on %s\n", c.Task, visible(strings.Join(c.Paths, ", ")))
	}
	_, _ = fmt.Fprintf(w, "\n----- task text -----\n%s\n----- end of task text -----\n", visible(t.Text))
}

// visible keeps newlines and tabs and writes every other control
// character, including escape sequences, as a Go escape such as \x1b.
func visible(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) && r != '\u2028' && r != '\u2029' && !unicode.Is(unicode.Bidi_Control, r) {
			b.WriteRune(r)
			continue
		}
		q := strconv.QuoteRune(r)
		b.WriteString(q[1 : len(q)-1])
	}
	return b.String()
}

// interactive reports whether stdin is a terminal. A character device is
// not enough: agents run commands with stdin on /dev/null, which is one
// (M4 two-machine check, C7).
func interactive(stdin io.Reader) bool {
	f, ok := stdin.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor or Windows handle fits in int
}

// confirm asks the member to type yes.
func confirm(stdin io.Reader, stdout io.Writer) bool {
	_, _ = fmt.Fprint(stdout, "\nApprove this task? Type yes to approve: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}
