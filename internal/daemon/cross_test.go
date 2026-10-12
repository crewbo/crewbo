package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/keys"
	"github.com/crewbo/crewbo/internal/platform"
	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

// machine is one teammate's computer: a daemon reaching the shared server
// through its own proxy, so the test can take one machine offline.
type machine struct {
	t       *testing.T
	paths   platform.Paths
	repo    string
	link    *link
	run     *running
	key     keys.Key
	keyring *memKeyring
}

// memKeyring is a keychain holding one device key's seed.
type memKeyring struct{ seed string }

func (k *memKeyring) Get(string, string) (string, error) { return k.seed, nil }
func (k *memKeyring) Set(_, _, s string) error           { k.seed = s; return nil }

type crossFixture struct {
	t   *testing.T
	srv *server.Server
	db  *store.DB
	a   *machine // RM1338, room lead
	b   *machine // gavriel953
}

func newCrossFixture(t *testing.T) *crossFixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "server.db"), store.Server)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(quietLog(), db.SQL())
	t.Cleanup(func() { srv.Close(); _ = db.Close() })
	f := &crossFixture{t: t, srv: srv, db: db}
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
	f.a = f.newMachine(lead, room)
	f.b = f.newMachine(mate, room)
	return f
}

func (f *crossFixture) newMachine(u auth.User, room server.RoomInfo) *machine {
	t := f.t
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	devKey := keys.Key{Private: priv}
	key := devKey.ID()
	if _, err := f.db.SQL().Exec(`INSERT INTO device_keys (key_id, github_user_id, public_key, name, os, created_unix_ms) VALUES (?, ?, ?, 'd', 'linux', 0)`, key, u.ID, []byte(devKey.Public())); err != nil {
		t.Fatal(err)
	}
	token, _, err := server.NewSessions(f.db.SQL(), time.Now).Create(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	m := &machine{
		t: t, paths: testPaths(t), repo: filepath.Join(t.TempDir(), "crewbo"), key: devKey,
		keyring: &memKeyring{seed: base64.StdEncoding.EncodeToString(priv.Seed())},
	}
	gitInit(t, m.repo)
	ts := httptest.NewServer(f.srv.Handler())
	t.Cleanup(ts.Close)
	m.link = newLink(t, strings.TrimPrefix(ts.URL, "http://"))
	url := "http://" + m.link.addr
	if err := auth.SaveSession(auth.SessionPath(m.paths.Config), auth.Session{Server: url, SessionToken: token, GitHubLogin: u.Login, DeviceKeyID: key}); err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveBindings(auth.BindingsPath(m.paths.Config), []auth.Binding{{Repo: m.repo, Server: url, RoomID: room.ID, Name: room.Name}}); err != nil {
		t.Fatal(err)
	}
	m.start()
	return m
}

func (m *machine) start() {
	exe, err := os.Executable() // a fake claude once a test calls fakeClaude
	if err != nil {
		m.t.Fatal(err)
	}
	m.run = start(m.t, Config{Paths: m.paths, Log: quietLog(), Keyring: m.keyring, ClaudeBin: exe, CodexBin: exe, syncFast: true})
	eventually(m.t, "the daemon to connect", func() bool { return strings.HasPrefix(m.status().Sync, "connected") })
}

func (m *machine) call(req Request) (Response, error) {
	m.t.Helper()
	c, err := dial(m.t, m.paths)
	if err != nil {
		m.t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Call(ctx, req)
}

func (m *machine) status() Status {
	resp, _ := m.call(Request{Method: MethodStatus})
	if resp.Status == nil {
		return Status{}
	}
	return *resp.Status
}

func (m *machine) claim(path string) (Response, error) {
	return m.call(Request{Method: MethodClaimAdd, Session: TerminalSession, Member: "local", Repo: m.repo, Patterns: []string{path}, LeaseSeconds: 1800})
}

// agentMay asks the daemon whether an agent session may edit path.
func (m *machine) agentMay(path string) (bool, claims.Claim) {
	resp, err := m.call(Request{Method: MethodDecide, Session: "agent-1", Agent: "codex", Repo: m.repo, Paths: []string{path}})
	if err != nil {
		m.t.Fatal(err)
	}
	if resp.BlockedBy != nil {
		return resp.Allow, *resp.BlockedBy
	}
	return resp.Allow, claims.Claim{}
}

func TestClaimOnOneMachineBlocksTheOther(t *testing.T) {
	f := newCrossFixture(t)
	a, b := f.a, f.b

	resp, err := a.claim("src/a.go")
	if err != nil || !strings.Contains(resp.Note, "Shared with room crewbo") || resp.Claims[0].Holder.MemberID != "RM1338" {
		t.Fatalf("A claims: %+v %v", resp, err)
	}
	eventually(t, "B to see A's claim", func() bool { ok, _ := b.agentMay("src/a.go"); return !ok })
	if _, holder := b.agentMay("src/a.go"); holder.Holder.MemberID != "RM1338" {
		t.Errorf("B's agent is told the holder is %q", holder.Holder.MemberID)
	}
	if ok, _ := a.agentMay("src/a.go"); ok {
		t.Error("A's own agent may edit A's terminal claim (M2: terminal claims block agents)")
	}
	if ok, _ := b.agentMay("src/other.go"); !ok {
		t.Error("an unclaimed file is blocked")
	}
	// B's terminal cannot take it, and cannot release A's claim.
	if resp, err := b.claim("src/a.go"); err == nil || resp.BlockedBy == nil || resp.BlockedBy.Holder.MemberID != "RM1338" {
		t.Errorf("B claims A's file: %+v %v", resp.BlockedBy, err)
	}
	if _, err := b.call(Request{Method: MethodClaimRelease, Repo: b.repo, IDs: []string{resp.Claims[0].ID}}); err == nil || !strings.Contains(err.Error(), "only they can release") {
		t.Errorf("B releases A's claim: %v", err)
	}
	// A daemon restart keeps the room's claims (it replays the room).
	b.run.cancel()
	<-b.run.done
	b.start()
	eventually(t, "B to rebuild A's claim after a restart", func() bool { ok, _ := b.agentMay("src/a.go"); return !ok })
	// A releases; B's agent may edit again.
	if rel, err := a.call(Request{Method: MethodClaimRelease, Repo: a.repo, IDs: []string{resp.Claims[0].ID}}); err != nil || !strings.Contains(rel.Note, "Shared with room crewbo") {
		t.Fatalf("release: %+v %v", rel, err)
	}
	eventually(t, "B to see the release", func() bool { ok, _ := b.agentMay("src/a.go"); return ok })
}

// The acceptance target: a claim reaches the other machine with p95 under
// 2 s on a LAN. Here both are on one host, so this mostly checks that
// nothing in the path waits.
func TestClaimPropagationTime(t *testing.T) {
	f := newCrossFixture(t)
	var times []time.Duration
	for i := range 20 {
		path := "src/p" + string(rune('a'+i)) + ".go"
		start := time.Now()
		if _, err := f.a.claim(path); err != nil {
			t.Fatal(err)
		}
		for ok, _ := f.b.agentMay(path); ok; ok, _ = f.b.agentMay(path) {
			if time.Since(start) > 5*time.Second {
				t.Fatalf("%s did not reach B in 5 s", path)
			}
			time.Sleep(2 * time.Millisecond)
		}
		times = append(times, time.Since(start))
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	p50, p95 := times[len(times)/2], times[len(times)*95/100]
	t.Logf("claim to remote deny over 20 claims: p50 %v, p95 %v, max %v", p50, p95, times[len(times)-1])
	if p95 >= 2*time.Second {
		t.Errorf("p95 %v, want under 2 s", p95)
	}
}

// A claim made offline is kept locally and shared on reconnect; if someone
// else took the file meanwhile, it is dropped and counted.
func TestOfflineClaimsReconcile(t *testing.T) {
	f := newCrossFixture(t)
	a, b := f.a, f.b
	a.link.cut() // A loses the server; B keeps it
	eventually(t, "A to notice", func() bool { return strings.HasPrefix(a.status().Sync, "offline") })

	kept, err := a.claim("src/kept.go")
	if err != nil || !strings.Contains(kept.Note, "will get it when the sync server answers") {
		t.Fatalf("offline claim: %+v %v", kept, err)
	}
	if _, err := a.claim("src/raced.go"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.agentMay("src/raced.go"); ok {
		t.Error("an offline claim does not block this machine's agents")
	}
	if _, err := b.claim("src/raced.go"); err != nil { // B takes it on the server first
		t.Fatal(err)
	}

	a.link.restore() // A is back
	eventually(t, "B to see A's offline claim", func() bool { ok, _ := b.agentMay("src/kept.go"); return !ok })
	eventually(t, "A to drop its refused claim", func() bool { return a.status().Refused == 1 })
	if ok, holder := a.agentMay("src/raced.go"); ok || holder.Holder.MemberID != "gavriel953" {
		t.Errorf("after reconcile, raced.go on A: allowed %v, holder %q", ok, holder.Holder.MemberID)
	}
	// A's refused local claim is gone: only B's claim on the file remains.
	list, err := a.call(Request{Method: MethodClaimList, Repo: a.repo})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list.Claims {
		if c.Pattern == "src/raced.go" && c.Holder.MemberID != "gavriel953" {
			t.Errorf("A still holds its refused claim: %+v", c)
		}
	}
}

// link is a TCP proxy standing in for one machine's network: cut drops
// every open connection and refuses new ones until restore.
type link struct {
	addr   string
	target string
	mu     sync.Mutex
	down   bool
	conns  []net.Conn
}

func newLink(t *testing.T, target string) *link {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := &link{addr: ln.Addr().String(), target: target}
	var wg sync.WaitGroup
	t.Cleanup(func() { _ = ln.Close(); l.cut(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			in, err := ln.Accept()
			if err != nil {
				return
			}
			l.mu.Lock()
			if l.down {
				l.mu.Unlock()
				_ = in.Close()
				continue
			}
			out, err := net.Dial("tcp", target)
			if err != nil {
				l.mu.Unlock()
				_ = in.Close()
				continue
			}
			l.conns = append(l.conns, in, out)
			l.mu.Unlock()
			wg.Add(2)
			go func() { defer wg.Done(); _, _ = io.Copy(out, in); _ = out.Close() }()
			go func() { defer wg.Done(); _, _ = io.Copy(in, out); _ = in.Close() }()
		}
	}()
	return l
}

func (l *link) cut() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.down = true
	for _, c := range l.conns {
		_ = c.Close()
	}
	l.conns = nil
}

func (l *link) restore() {
	l.mu.Lock()
	l.down = false
	l.mu.Unlock()
}

// gitInit makes dir a git repository with one commit.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "base"}} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", args...) //nolint:gosec // test helper with fixed arguments
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// fetchFrom brings other's commits into m's clone, as git fetch from a
// shared remote would.
func (m *machine) fetchFrom(other *machine) {
	m.t.Helper()
	cmd := exec.Command("git", "-C", m.repo, "fetch", "-q", other.repo, "HEAD") //nolint:gosec // test
	if out, err := cmd.CombinedOutput(); err != nil {
		m.t.Fatalf("git fetch: %v\n%s", err, out)
	}
}
