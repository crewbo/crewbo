package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/daemon"
	"github.com/crewbo/crewbo/internal/dispatch"
	"github.com/crewbo/crewbo/internal/keys"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

func task(t *testing.T, args []string, stdin io.Reader) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), append([]string{"task"}, args...), stdin, &out, &errOut, BuildInfo{})
	return code, out.String(), errOut.String()
}

// The member side of the task commands against a real daemon and server:
// two tasks signed by the lead wait in the room log.
func TestTaskCommandsForTheAssignee(t *testing.T) {
	p := isolate(t)
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(slog.New(slog.DiscardHandler), db.SQL())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close(); _ = db.Close() })
	rooms := server.NewRooms(db.SQL(), time.Now)
	lead, mate := auth.User{ID: 1, Login: "RM1338"}, auth.User{ID: 2, Login: "gavriel953"}
	room, err := rooms.Create(ctx, lead, "crewbo")
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := rooms.Invite(ctx, lead, room.ID, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rooms.Join(ctx, mate, code); err != nil {
		t.Fatal(err)
	}
	var leadKey, mateKey keys.Key
	for _, k := range []struct {
		key *keys.Key
		uid int64
	}{{&leadKey, 1}, {&mateKey, 2}} {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		*k.key = keys.Key{Private: priv}
		if _, err := db.SQL().Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, ?, ?, 'd', 'linux', 0)`, k.key.ID(), k.uid, []byte(k.key.Public())); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	texts := []string{"Fix the flaky test.\nRun it 20 times.", "Hidden \x1b[2Kpart and \u202eright to left"}
	for _, text := range texts {
		id, _ := dispatch.NewTaskID()
		st, err := dispatch.Sign(leadKey, &crewbov1.TaskBody{
			TaskId: id, RoomId: room.ID, AuthorKeyId: leadKey.ID(), AuthorMemberId: "RM1338", AssigneeMemberId: "gavriel953",
			Text: text, OwnedPaths: []string{"internal/x.go"}, BaseCommit: "abc123", CreatedUnixMs: now.UnixMilli(), ExpiresUnixMs: now.Add(time.Hour).UnixMilli(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ev := &crewbov1.Event{ActorMemberId: "RM1338", Body: &crewbov1.Event_TaskDispatched{TaskDispatched: &crewbov1.TaskDispatched{Task: st}}}
		if _, err := server.NewLog(db.SQL()).Append(ctx, room.ID, ev, now); err != nil {
			t.Fatal(err)
		}
	}
	token, _, err := server.NewSessions(db.SQL(), time.Now).Create(ctx, mate)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveSession(auth.SessionPath(p.Config), auth.Session{Server: ts.URL, SessionToken: token, GitHubLogin: "gavriel953", DeviceKeyID: mateKey.ID()}); err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveBindings(auth.BindingsPath(p.Config), []auth.Binding{{Repo: t.TempDir(), Server: ts.URL, RoomID: room.ID, Name: "crewbo"}}); err != nil {
		t.Fatal(err)
	}
	dctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = daemon.New(daemon.Config{Paths: p}).Run(dctx) }()
	t.Cleanup(func() { cancel(); <-done })

	var out string
	for deadline := time.Now().Add(10 * time.Second); strings.Count(out, "dispatched") != 2; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("task list never showed both tasks: %q", out)
		}
		_, out, _ = task(t, []string{"list"}, nil)
	}
	if !strings.Contains(out, "from RM1338") || !strings.Contains(out, "Fix the flaky test. ...") || !strings.Contains(out, `\x1b[2K`) {
		t.Errorf("list = %q", out)
	}
	var first, second string
	resp := func() []daemon.Task {
		c, err := daemon.Dial(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		r, err := c.Call(ctx, daemon.Request{Method: daemon.MethodTaskList})
		if err != nil {
			t.Fatal(err)
		}
		return r.Tasks
	}()
	for _, tk := range resp {
		if strings.HasPrefix(tk.Text, "Fix") {
			first = tk.ID
		} else {
			second = tk.ID
		}
	}

	// show prints the whole text, with control characters made visible.
	if code, out, errOut := task(t, []string{"show", first[:30]}, nil); code != ExitOK || !strings.Contains(out, "Fix the flaky test.\nRun it 20 times.\n") || !strings.Contains(out, "Base:     abc123") || !strings.Contains(out, "Paths:    internal/x.go") {
		t.Errorf("show = %d %q %q", code, out, errOut)
	}
	if _, out, _ := task(t, []string{"show", second}, nil); !strings.Contains(out, `Hidden \x1b[2Kpart and \u202eright to left`) || strings.ContainsAny(out, "\x1b\u202e") {
		t.Errorf("show did not neutralise control characters: %q", out)
	}
	// approve needs a terminal; a pipe is refused before anything is shown.
	if code, out, errOut := task(t, []string{"approve", first}, strings.NewReader("yes\n")); code != ExitError || out != "" || !strings.Contains(errOut, "interactive terminal") {
		t.Errorf("approve from a pipe = %d %q %q", code, out, errOut)
	}
	// In order: the second decline must follow the first.
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"ambiguous id", []string{"show", "0"}, ExitError, "matches 2 tasks"},
		{"unknown id", []string{"decline", "zzz"}, ExitError, "no task zzz"},
		{"reason when showing", []string{"show", first, "--reason", "x"}, ExitUsage, "Usage:"},
		{"no id", []string{"decline"}, ExitUsage, "Usage:"},
		{"unknown subcommand", []string{"frobnicate"}, ExitUsage, "Usage:"},
		{"decline with reason", []string{"decline", first, "--reason", "on holiday"}, ExitOK, "Task " + first + " declined. The lead has been told."},
		{"decline again", []string{"decline", first}, ExitError, "already declined"},
	} {
		if code, out, errOut := task(t, tc.args, nil); code != tc.code || !strings.Contains(out+errOut, tc.want) {
			t.Errorf("%s: %d %q %q; want %d and %q", tc.name, code, out, errOut, tc.code, tc.want)
		}
	}
	if _, out, _ := task(t, []string{"show", first}, nil); !strings.Contains(out, "State:    declined") || !strings.Contains(out, "Reason:   on holiday") {
		t.Errorf("after decline: %q", out)
	}
}

func TestVisible(t *testing.T) {
	for in, want := range map[string]string{
		"plain\ttext\nnext line": "plain\ttext\nnext line",
		"ünïcødé 日本":             "ünïcødé 日本",
		"\x1b[31mred":            `\x1b[31mred`,
		"a\rb\x00c\x7f":          `a\rb\x00c\x7f`,
		"\u202eevil\u2066":       `\u202eevil\u2066`,
		"line\u2028sep":          `line\u2028sep`,
	} {
		if got := visible(in); got != want {
			t.Errorf("visible(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfirmNeedsYes(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{{"yes\n", true}, {" yes \n", true}, {"y\n", false}, {"YES\n", false}, {"", false}, {"no\n", false}} {
		if got := confirm(strings.NewReader(tc.in), io.Discard); got != tc.want {
			t.Errorf("confirm(%q) = %v", tc.in, got)
		}
	}
}

func TestListLine(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.Local)
	long := strings.Repeat("x", 70)
	got := listLine(daemon.Task{ID: "id1", State: "dispatched", Author: "RM1338", Text: long, Expires: now.Add(90 * time.Minute)}, now)
	if !strings.Contains(got, strings.Repeat("x", 60)+" ...") || strings.Contains(got, strings.Repeat("x", 61)) || !strings.Contains(got, "(in 1h30m0s)") {
		t.Errorf("long task: %q", got)
	}
	if got := listLine(daemon.Task{ID: "id2", State: "dispatched", Author: "RM1338", Text: "done", Expires: now.Add(-time.Minute)}, now); !strings.Contains(got, "expired 2026-10-12 11:59:00") {
		t.Errorf("expired task: %q", got)
	}
	if got := listLine(daemon.Task{State: "rejected", Detail: "bad \x1b", Received: now}, now); got != `(rejected)  received 2026-10-12 12:00:00: bad \x1b` {
		t.Errorf("rejected task: %q", got)
	}
}
