package cli

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/hooks"
)

const taskUsage = `Usage:
  crewbo task create --to LOGIN [--path PATH]... [--for DURATION] (TEXT... | --file FILE)

Sends a signed task to a teammate's agent in this repository's room. Only
the room lead can send tasks. The teammate sees the full text and must
approve it before anything runs. --path names the files or globs the task
may change (repeat it); --for is how long the teammate has to approve
(default 24h, at most 7 days); --file - reads the text from stdin.
`

// paths is a repeatable --path flag.
type pathList []string

func (p *pathList) String() string     { return strings.Join(*p, ",") }
func (p *pathList) Set(v string) error { *p = append(*p, v); return nil }

func runTask(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create" {
		return write(stderr, ExitUsage, "%s", taskUsage)
	}
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
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	c, err := daemon.Dial(dctx, p)
	cancel()
	if err != nil {
		return write(stderr, ExitError, "crewbo: the daemon is not running; start it with crewbo daemon\n")
	}
	defer func() { _ = c.Close() }()
	cctx, ccancel := context.WithTimeout(ctx, 30*time.Second)
	defer ccancel()
	resp, err := c.Call(cctx, daemon.Request{
		Method: daemon.MethodTaskCreate, Repo: root, Assignee: *to, Text: text,
		Patterns: patterns, TTLSeconds: int64(ttl.Seconds()),
	})
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return write(stdout, ExitOK, "Task %s: %s\n", resp.TaskID, resp.Note)
}
