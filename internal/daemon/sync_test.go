package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/platform"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/server"
	"github.com/crewbo/crewbo/internal/store"
)

// swappable lets a test take the server down and bring a new one up at
// the same URL, as a restart would.
type swappable struct {
	mu sync.Mutex
	h  http.Handler
}

func (s *swappable) set(h http.Handler) { s.mu.Lock(); s.h = h; s.mu.Unlock() }

func (s *swappable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	h := s.h
	s.mu.Unlock()
	if h == nil {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

type syncFixture struct {
	t      *testing.T
	ts     *httptest.Server
	proxy  *swappable
	dbPath string
	db     *store.DB
	srv    *server.Server
	room   string
	token  string
	paths  platform.Paths
	sy     *syncer

	mu   sync.Mutex
	seqs []uint64
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	ctx := context.Background()
	f := &syncFixture{t: t, proxy: &swappable{}, dbPath: filepath.Join(t.TempDir(), "server.db")}
	f.ts = httptest.NewServer(f.proxy)
	t.Cleanup(f.ts.Close)
	f.start()
	u := auth.User{ID: 1, Login: "RM1338"}
	var err error
	if f.token, _, err = server.NewSessions(f.db.SQL(), time.Now).Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	info, err := server.NewRooms(f.db.SQL(), time.Now).Create(ctx, u, "crewbo")
	if err != nil {
		t.Fatal(err)
	}
	f.room = info.ID
	dir := t.TempDir()
	f.paths = platform.Paths{Data: filepath.Join(dir, "data"), Config: filepath.Join(dir, "config"), Runtime: filepath.Join(dir, "run")}
	ddb, err := openDB(ctx, filepath.Join(t.TempDir(), "daemon.db"), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ddb.Close() })
	f.sy = newSyncer(f.paths, ddb.SQL(), quietLog(), func(_ auth.Binding, ev *crewbov1.Event) {
		f.mu.Lock()
		f.seqs = append(f.seqs, ev.GetSeq())
		f.mu.Unlock()
	})
	f.sy.minBackoff, f.sy.maxBackoff, f.sy.reloadEvery = 20*time.Millisecond, 100*time.Millisecond, 20*time.Millisecond
	return f
}

// start opens the server database and serves it, as a fresh process would.
func (f *syncFixture) start() {
	db, err := store.Open(context.Background(), f.dbPath, store.Server)
	if err != nil {
		f.t.Fatal(err)
	}
	f.db, f.srv = db, server.New(quietLog(), db.SQL())
	f.proxy.set(f.srv.Handler())
}

// stop takes the server down: open connections are told to come back.
func (f *syncFixture) stop() {
	f.proxy.set(nil)
	f.srv.Close()
	_ = f.db.Close()
}

func (f *syncFixture) signIn() {
	f.t.Helper()
	if err := auth.SaveSession(auth.SessionPath(f.paths.Config), auth.Session{Server: f.ts.URL, SessionToken: f.token, GitHubLogin: "RM1338", DeviceKeyID: "ed25519:test"}); err != nil {
		f.t.Fatal(err)
	}
	if err := auth.SaveBindings(auth.BindingsPath(f.paths.Config), []auth.Binding{{Repo: "/r", Server: f.ts.URL, RoomID: f.room, Name: "crewbo"}}); err != nil {
		f.t.Fatal(err)
	}
}

// appendEvents commits n events straight to the server's log.
func (f *syncFixture) appendEvents(n int) {
	f.t.Helper()
	for range n {
		ev := &crewbov1.Event{ActorMemberId: "1", Body: &crewbov1.Event_ClaimsReleased{ClaimsReleased: &crewbov1.ClaimsReleased{ClaimIds: []string{"c"}}}}
		if _, err := server.NewLog(f.db.SQL()).Append(context.Background(), f.room, ev, time.Now()); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *syncFixture) applied() []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint64(nil), f.seqs...)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func seqsAre(got []uint64, from, to uint64) bool {
	if uint64(len(got)) != to-from+1 {
		return false
	}
	for i, s := range got {
		if s != from+uint64(i) {
			return false
		}
	}
	return true
}

func TestSyncReplaysAcrossRestartsAndOutages(t *testing.T) {
	f := newSyncFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.sy.Run(ctx) }()
	defer func() { cancel(); <-done }()

	// Not signed in: local only, as in M2.
	eventually(t, "local-only state", func() bool { return strings.Contains(f.sy.State(), "not signed in") })
	f.appendEvents(3)
	// Signing in and binding a repository is picked up without a restart.
	f.signIn()
	eventually(t, "events 1 to 3", func() bool { return seqsAre(f.applied(), 1, 3) })
	if !strings.Contains(f.sy.State(), "connected to "+f.ts.URL+" as RM1338, 1 room") {
		t.Errorf("state = %q", f.sy.State())
	}

	// Restart: a new server process over the same database file; events
	// committed while it was down arrive once, in order.
	f.stop()
	eventually(t, "offline state", func() bool { return strings.HasPrefix(f.sy.State(), "offline, retrying") })
	f.start()
	f.appendEvents(2)
	eventually(t, "events 4 and 5 after the restart", func() bool { return seqsAre(f.applied(), 1, 5) })

	// A longer outage with events committed meanwhile.
	f.stop()
	time.Sleep(150 * time.Millisecond) // several failed reconnects
	f.start()
	f.appendEvents(1)
	eventually(t, "event 6 after the outage", func() bool { return seqsAre(f.applied(), 1, 6) })
	if seq, err := f.sy.cursor(context.Background(), f.room); err != nil || seq != 6 {
		t.Errorf("saved cursor = %d %v", seq, err)
	}

	// Leaving the room (bindings emptied) drops back to local only.
	if err := auth.SaveBindings(auth.BindingsPath(f.paths.Config), nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "no-room state", func() bool { return strings.Contains(f.sy.State(), "no repository is in a room") })
}

// A refused session waits for a new sign-in instead of retrying.
func TestSyncRefusedWaitsForANewSession(t *testing.T) {
	f := newSyncFixture(t)
	good := f.token
	f.token = "cbs_expired"
	f.signIn()
	var hits int
	var mu sync.Mutex
	inner := f.srv.Handler()
	f.proxy.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		inner.ServeHTTP(w, r)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.sy.Run(ctx) }()
	defer func() { cancel(); <-done }()

	eventually(t, "refused state", func() bool { return strings.Contains(f.sy.State(), "sign in again") })
	time.Sleep(300 * time.Millisecond) // three times maxBackoff
	mu.Lock()
	n := hits
	mu.Unlock()
	if n != 1 {
		t.Errorf("connected %d times after a refusal, want 1", n)
	}
	f.token = good
	f.signIn()
	eventually(t, "connected after signing in again", func() bool { return strings.HasPrefix(f.sy.State(), "connected") })
}

func TestAdvance(t *testing.T) {
	for _, tc := range []struct {
		cursor, seq, want uint64
		gap               bool
	}{{0, 1, 1, false}, {5, 6, 6, false}, {5, 5, 5, false}, {5, 2, 5, false}, {5, 7, 5, true}, {0, 2, 0, true}} {
		got, err := advance(tc.cursor, tc.seq)
		if got != tc.want || (err != nil) != tc.gap {
			t.Errorf("advance(%d, %d) = %d, %v", tc.cursor, tc.seq, got, err)
		}
	}
}

func TestDamagedDaemonDBIsMovedAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("not a database ", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(context.Background(), path, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	matches, _ := filepath.Glob(path + ".damaged-*")
	if len(matches) != 1 {
		t.Errorf("damaged file not kept aside: %v", matches)
	}
}

func TestWSURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.trycloudflare.com": "wss://x.trycloudflare.com/v1/ws",
		"http://127.0.0.1:7420":       "ws://127.0.0.1:7420/v1/ws",
	} {
		if got := wsURL(in); got != want {
			t.Errorf("wsURL(%q) = %q", in, got)
		}
	}
}
