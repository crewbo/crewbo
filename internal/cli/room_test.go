package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

// machine is one teammate's computer: a config dir with a session and a
// clone of the repository.
type machine struct {
	t    *testing.T
	rc   roomCmd
	repo string
}

func (m machine) room(args ...string) (int, string, string) {
	m.t.Helper()
	var out, errOut bytes.Buffer
	m.rc.stdout, m.rc.stderr = &out, &errOut
	code := m.rc.run(context.Background(), args)
	return code, out.String(), errOut.String()
}

func newMachine(t *testing.T, ts *httptest.Server, db *store.DB, id int64, login string) machine {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "crewbo-app")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config")
	if login != "" {
		tok, _, err := server.NewSessions(db.SQL(), time.Now).Create(context.Background(), auth.User{ID: id, Login: login})
		if err != nil {
			t.Fatal(err)
		}
		if err := auth.SaveSession(auth.SessionPath(cfg), auth.Session{Server: ts.URL, SessionToken: tok, GitHubLogin: login}); err != nil {
			t.Fatal(err)
		}
	}
	return machine{t: t, rc: roomCmd{http: ts.Client(), configDir: cfg, cwd: filepath.Join(repo, "sub")}, repo: repo}
}

func TestRoomCommands(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ts := httptest.NewServer(server.New(slog.New(slog.NewTextHandler(io.Discard, nil)), db.SQL()).Handler())
	defer ts.Close()
	lead := newMachine(t, ts, db, 1, "RM1338")
	mate := newMachine(t, ts, db, 2, "gavriel953")

	expect := func(m machine, wantCode int, want string, args ...string) string {
		t.Helper()
		code, out, errOut := m.room(args...)
		if code != wantCode || !strings.Contains(out+errOut, want) {
			t.Fatalf("room %v = %d\nstdout %q\nstderr %q\nwant %d and %q", args, code, out, errOut, wantCode, want)
		}
		return out
	}
	expect(lead, ExitOK, "is not in a room", "status")
	expect(lead, ExitOK, "Created room crewbo-app", "create")
	expect(lead, ExitError, "already in room crewbo-app", "create")
	expect(lead, ExitOK, "is in room crewbo-app", "status")
	expect(mate, ExitError, "not in a room", "invite")
	out := expect(lead, ExitOK, "valid until", "invite", "--uses", "2", "--for", "2h")
	code := regexp.MustCompile(`crewbo room join (cbi_\S+)`).FindStringSubmatch(out)
	if code == nil || !strings.Contains(out, "up to 2 people") {
		t.Fatalf("no invite code in %q", out)
	}
	expect(lead, ExitError, "max_uses must be 1 to 9", "invite", "--uses", "20")
	expect(mate, ExitError, "unknown invite code", "join", "cbi_wrong")
	expect(mate, ExitOK, "Joined room crewbo-app (lead: RM1338, 2 members)", "join", code[1])
	expect(mate, ExitError, "already in room", "join", code[1])
	expect(mate, ExitError, "only the room lead", "invite")
	expect(lead, ExitError, "lead cannot leave while others are in the room", "leave")
	expect(mate, ExitOK, "Left room crewbo-app", "leave")
	expect(mate, ExitOK, "is not in a room", "status")
	expect(lead, ExitOK, "Left room crewbo-app", "leave")
	// The server no longer has the lead in the room: leaving the stale local
	// binding still works.
	expect(lead, ExitOK, "Created room", "create", "--name", "second")
	bs, _ := auth.LoadBindings(auth.BindingsPath(lead.rc.configDir))
	if len(bs) != 1 || bs[0].Repo != lead.repo || bs[0].Name != "second" {
		t.Fatalf("bindings = %+v", bs)
	}
	if _, err := db.SQL().Exec(`UPDATE members SET left_unix_ms = 1`); err != nil {
		t.Fatal(err)
	}
	expect(lead, ExitOK, "Left room second", "leave")
}

func TestRoomNeedsRepoAndLogin(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()
	noRepo := roomCmd{http: ts.Client(), configDir: t.TempDir(), cwd: t.TempDir()}
	var out, errOut bytes.Buffer
	noRepo.stdout, noRepo.stderr = &out, &errOut
	if code := noRepo.run(context.Background(), []string{"status"}); code != ExitError || !strings.Contains(errOut.String(), "git repository") {
		t.Errorf("outside a repo: %d %q", code, errOut.String())
	}
	m := newMachine(t, ts, nil, 0, "")
	if code, _, errOut := m.room("status"); code != ExitError || !strings.Contains(errOut, "crewbo login") {
		t.Errorf("not signed in: %d %q", code, errOut)
	}
	if code, _, _ := m.room(); code != ExitUsage {
		t.Errorf("no subcommand: %d", code)
	}
	if code, _, _ := m.room("frob"); code != ExitUsage {
		t.Errorf("unknown subcommand: %d", code)
	}
}
