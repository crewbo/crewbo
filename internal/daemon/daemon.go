// Package daemon runs crewbo's per-user background process: one owner
// goroutine holds the claim set, and a local socket serves the hook, MCP
// server and CLI (docs/architecture/components.md).
package daemon

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/keys"
	"github.com/crewbo/crewbo/internal/platform"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/roomstate"
	"github.com/crewbo/crewbo/internal/store"
)

// TokenName is the token file inside platform.Paths.Runtime.
const TokenName = "daemon.token"

const (
	helloTimeout = 2 * time.Second
	maxLine      = 1 << 20
)

// Config configures a Daemon. Zero values get defaults.
type Config struct {
	Paths   platform.Paths
	Version string
	// Claims holds initial claim sets keyed by repository root (any path
	// spelling; keys are normalised with RepoKey).
	Claims map[string]*claims.Set
	Now    func() time.Time // clock; time.Now when nil
	Log    *slog.Logger

	// Keyring holds this device's key; the OS keychain when nil.
	Keyring keys.Keyring
	// ClaudeBin and CodexBin are the agent executables for approved
	// tasks; "claude" and "codex" from PATH when empty.
	ClaudeBin string
	CodexBin  string

	syncFast bool // tests: reconnect and reload in milliseconds
}

// Daemon is one running crewbo daemon.
type Daemon struct {
	cfg     Config
	started time.Time
	calls   chan call
	// Everything below is owned by the owner goroutine.
	sessions   map[string]*Session
	claims     map[string]*claims.Set // keyed by RepoKey
	snapshots  map[string]*snapshot   // session + tool use -> pre-shell hashes
	violations []Violation

	sync       *syncer       // set by Run before the owner starts; safe for concurrent use
	funcs      chan func()   // work for the owner goroutine from the syncer
	refused    int           // claims the server refused after the fact
	verifyKick chan struct{} // received tasks to verify

	agents sync.WaitGroup // supervisors of running task agents
	// The dashboard (M5-06), started on the first `crewbo dashboard`.
	webMu      sync.Mutex
	webURL     string
	web        sync.WaitGroup
	renewEvery time.Duration // ClaimRenew interval for running agents

	runsMu sync.Mutex
	runs   map[string]string             // running task's worktree (pathKey) to its agent session
	stops  map[string]context.CancelFunc // running dispatched task id to what stops its agent

	ownMu             sync.Mutex           // own task checkpoints, one at a time
	lastOwnCheckpoint map[string]time.Time // own task id to its last checkpoint
	ownEvery          time.Duration        // automatic checkpoints at most this often
}

type call struct {
	req   Request
	reply chan Response
}

// New returns a Daemon; call Run to start it.
func New(cfg Config) *Daemon {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	d := &Daemon{cfg: cfg, calls: make(chan call), funcs: make(chan func()), sessions: map[string]*Session{}, verifyKick: make(chan struct{}, 1), renewEvery: claimRenewEvery, runs: map[string]string{}, stops: map[string]context.CancelFunc{}, lastOwnCheckpoint: map[string]time.Time{}, ownEvery: ownCheckpointEvery, claims: map[string]*claims.Set{}, snapshots: map[string]*snapshot{}}
	for repo, set := range cfg.Claims {
		d.claims[RepoKey(repo)] = set
	}
	return d
}

// Run serves until ctx is cancelled, then closes the socket, waits for open
// connections and removes the token file. It returns
// platform.ErrAlreadyRunning when another daemon owns the socket.
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.cfg.Paths.Ensure(); err != nil {
		return err
	}
	ln, err := platform.Listen(d.cfg.Paths.SocketPath())
	if err != nil {
		return err
	}
	// The token is written only after Listen succeeds, so a second daemon
	// that fails with ErrAlreadyRunning never replaces the live token.
	token, err := writeToken(filepath.Join(d.cfg.Paths.Runtime, TokenName))
	if err != nil {
		_ = ln.Close()
		return err
	}
	d.started = d.cfg.Now()
	d.cfg.Log.Info("daemon started", "socket", d.cfg.Paths.SocketPath(), "pid", os.Getpid())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	db, err := openDB(ctx, filepath.Join(d.cfg.Paths.Data, "daemon.db"), d.cfg.Log)
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer func() { _ = db.Close() }()
	d.sync = newSyncer(d.cfg.Paths, db.SQL(), d.cfg.Log,
		func(b auth.Binding, ev *crewbov1.Event, clock roomstate.Clock) error {
			return d.applyEvent(ctx, b, ev, clock)
		},
		func(b auth.Binding, ids []string, reason string) { d.claimRefused(ctx, b, ids, reason) })
	if d.cfg.syncFast {
		d.sync.minBackoff, d.sync.maxBackoff, d.sync.reloadEvery = 20*time.Millisecond, 100*time.Millisecond, 20*time.Millisecond
		d.sync.pingEvery, d.sync.pongWait = 200*time.Millisecond, 200*time.Millisecond
		d.renewEvery = 100 * time.Millisecond
		d.ownEvery = 300 * time.Millisecond
	}
	var conns sync.WaitGroup
	ownerDone := make(chan struct{})
	go func() { defer close(ownerDone); d.own(ctx) }()
	syncDone := make(chan struct{})
	go func() { defer close(syncDone); d.sync.Run(ctx) }()
	retry := taskRetry
	if d.cfg.syncFast {
		retry = 50 * time.Millisecond
	}
	d.restoreOwnTasks(ctx)
	verifyDone := make(chan struct{})
	go func() { defer close(verifyDone); d.verifyTasks(ctx, retry) }()
	go func() { <-ctx.Done(); _ = ln.Close() }()

	for {
		c, err := ln.Accept()
		if err != nil {
			break // listener closed on shutdown
		}
		conns.Add(1)
		go func() { defer conns.Done(); d.serve(ctx, c, token) }()
	}
	cancel()
	conns.Wait()
	d.web.Wait()
	<-ownerDone
	<-syncDone
	<-verifyDone
	d.agents.Wait() // agents were stopped with ctx; their final state is recorded
	if err := os.Remove(filepath.Join(d.cfg.Paths.Runtime, TokenName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		d.cfg.Log.Warn("remove token file", "err", err)
	}
	d.cfg.Log.Info("daemon stopped")
	return nil
}

// own is the owner goroutine: the only code that touches the claim set.
func (d *Daemon) own(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-d.calls:
			c.reply <- d.handle(c.req)
		case f := <-d.funcs:
			f()
		}
	}
}

func (d *Daemon) handle(req Request) Response {
	now := d.cfg.Now()
	switch req.Method {
	case MethodEvent:
		if req.Session == "" || req.Event == "" {
			return Response{ID: req.ID, Error: "event: session and event are required"}
		}
		d.touch(req, req.Event, now)
		if req.Event == "post_edit" {
			return Response{ID: req.ID, Violations: d.checkSnapshot(req, now)}
		}
		return Response{ID: req.ID}
	case MethodDecide:
		if req.Session == "" {
			return Response{ID: req.ID, Error: "decide: session is required"}
		}
		d.touch(req, "pre_edit", now)
		set := d.set(req.Repo)
		dec := set.Decide(req.Session, req.Paths, now)
		if dec.Allow && req.Opaque {
			if c, ok := set.MentionedBy(req.Session, req.Command, now); ok {
				dec = claims.Decision{Allow: false, Path: c.Pattern, BlockedBy: c}
			}
		}
		if dec.Allow && req.Command != "" {
			d.takeSnapshot(req, now)
		}
		resp := Response{ID: req.ID, Allow: dec.Allow, Path: dec.Path}
		if !dec.Allow {
			blocked := dec.BlockedBy
			resp.BlockedBy = &blocked
		}
		return resp
	case MethodClaimAdd:
		return d.claimAdd(req, now)
	case MethodClaimList:
		return Response{ID: req.ID, Claims: d.set(req.Repo).Active(now)}
	case MethodClaimRelease:
		set := d.set(req.Repo)
		var released []string
		for _, id := range req.IDs {
			c, ok := set.Get(id)
			if !ok {
				continue
			}
			if !ownedBy(c, req.OwnerPrefix) {
				return Response{ID: req.ID, Error: fmt.Sprintf("claim %s is held by %s; only they can release it", id, c.Holder.MemberID)}
			}
			set.Release(id)
			released = append(released, id)
		}
		if len(released) == 0 && len(req.IDs) > 0 {
			return Response{ID: req.ID, Error: "no matching claims in this repository"}
		}
		return Response{ID: req.ID, Released: len(released), ReleasedIDs: released}
	case MethodStatus:
		return Response{ID: req.ID, Status: &Status{
			Version: d.cfg.Version, PID: os.Getpid(), Started: d.started,
			ActiveClaims: d.activeClaims(now), Sessions: len(d.sessions), Violations: len(d.violations),
			Sync: d.syncState(), Refused: d.refused, Login: d.login(), Agents: d.agentsSeen(),
		}}
	default:
		return Response{ID: req.ID, Error: fmt.Sprintf("unknown method %q", req.Method)}
	}
}

// touch registers or updates the session and renews its leases: any hook
// event proves the agent is alive (ADR 0003).
func (d *Daemon) touch(req Request, event string, now time.Time) {
	s, ok := d.sessions[req.Session]
	if !ok {
		s = &Session{ID: req.Session}
		d.sessions[req.Session] = s
		d.cfg.Log.Info("agent session registered", "session", req.Session, "agent", req.Agent)
	}
	if req.Agent != "" {
		s.Agent = req.Agent
	}
	if req.Repo != "" {
		s.Repo = req.Repo
	}
	s.LastSeen, s.LastEvent = now, event
	for _, set := range d.claims {
		set.Renew(req.Session, now, claims.DefaultLease)
	}
}

// set returns the claim set for a repository, creating it on first use.
func (d *Daemon) set(repo string) *claims.Set {
	key := RepoKey(repo)
	s, ok := d.claims[key]
	if !ok {
		s = claims.NewSet()
		d.claims[key] = s
	}
	return s
}

func (d *Daemon) activeClaims(now time.Time) int {
	n := 0
	for _, s := range d.claims {
		n += len(s.Active(now))
	}
	return n
}

// claimAdd grants all patterns or none: on a conflict, the claims already
// added by this request are released and the blocking claim is returned.
func (d *Daemon) claimAdd(req Request, now time.Time) Response {
	if req.Session == "" || len(req.Patterns) == 0 {
		return Response{ID: req.ID, Error: "claim_add: session and patterns are required"}
	}
	lease := DefaultCLILease
	if req.LeaseSeconds > 0 {
		lease = min(time.Duration(req.LeaseSeconds)*time.Second, MaxCLILease)
	}
	set := d.set(req.Repo)
	var granted []claims.Claim
	for _, p := range req.Patterns {
		c := claims.Claim{
			ID: newClaimID(), Pattern: p, TaskID: req.Task, LeaseExpires: now.Add(lease),
			Holder: claims.Holder{SessionID: req.Session, MemberID: req.Member},
		}
		if err := set.Add(c, now); err != nil {
			for _, g := range granted {
				set.Release(g.ID)
			}
			resp := Response{ID: req.ID, Error: err.Error()}
			var ce *claims.ConflictError
			if errors.As(err, &ce) {
				blocked := ce.Existing
				resp.BlockedBy, resp.Path = &blocked, p
			}
			return resp
		}
		granted = append(granted, c)
	}
	return Response{ID: req.ID, Claims: granted}
}

func newClaimID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return "c-" + hex.EncodeToString(b)
}

// agentsSeen returns when each agent last sent a hook event. It runs on
// the owner goroutine, like every reader of d.sessions.
func (d *Daemon) agentsSeen() map[string]time.Time {
	out := map[string]time.Time{}
	for _, s := range d.sessions {
		if s.Agent != "" && s.LastSeen.After(out[s.Agent]) {
			out[s.Agent] = s.LastSeen
		}
	}
	return out
}

// login is the GitHub login this machine is signed in with, if any.
func (d *Daemon) login() string {
	if d.sync == nil {
		return ""
	}
	return d.sync.session().GitHubLogin
}

func (d *Daemon) syncState() string {
	if d.sync == nil {
		return ""
	}
	return d.sync.State()
}

// openDB opens the daemon database, moving a damaged file aside and
// starting empty (failure mode F19): the database holds only cursors and
// queued requests, which the server can rebuild.
func openDB(ctx context.Context, path string, log *slog.Logger) (*store.DB, error) {
	db, err := store.Open(ctx, path, store.Daemon)
	if errors.Is(err, store.ErrCorrupt) {
		aside := fmt.Sprintf("%s.damaged-%d", path, time.Now().Unix())
		log.Warn("daemon database is damaged; starting a new one", "moved_to", aside, "err", err)
		if rerr := os.Rename(path, aside); rerr != nil {
			return nil, fmt.Errorf("%w (and moving it aside failed: %w)", err, rerr)
		}
		return store.Open(ctx, path, store.Daemon)
	}
	return db, err
}

// RepoKey identifies a repository across path spellings: linked worktrees
// mapped to their main worktree, symlinks resolved (macOS /tmp is
// /private/tmp), forward slashes, case folded (Windows tools disagree on
// drive-letter case). Claims are per repository, so a hook and a terminal
// in the same repository, in any of its worktrees, map to the same key.
func RepoKey(repo string) string {
	if repo == "" {
		return ""
	}
	return pathKey(mainWorktree(repo))
}

// pathKey identifies a directory across path spellings, as RepoKey does
// without mapping worktrees.
func pathKey(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(dir)))
}

// mainWorktree returns the main worktree of a linked worktree, and any
// other directory unchanged (a normal checkout, a submodule, a worktree of
// a bare repository). A linked worktree's .git is a file holding
// "gitdir: <path>" (gitrepository-layout(5), gitfile; relative to the
// worktree), and that directory's commondir file names the main $GIT_DIR
// (relative to that directory). It reads files only: hooks call it on
// every decision.
func mainWorktree(repo string) string {
	b, err := os.ReadFile(filepath.Join(repo, ".git")) //nolint:gosec // the repository's own gitfile; fails for a .git directory
	if err != nil {
		return repo
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return repo
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(repo, gitdir)
	}
	c, err := os.ReadFile(filepath.Join(gitdir, "commondir")) //nolint:gosec // git's own metadata for this repository
	if err != nil {
		return repo // a submodule has no commondir: it is its own repository
	}
	common := strings.TrimSpace(string(c))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	if filepath.Base(common) != ".git" {
		return repo // a bare main repository has no main worktree
	}
	return filepath.Dir(common)
}

// serve authenticates one connection and answers its requests in order.
func (d *Daemon) serve(ctx context.Context, c net.Conn, token string) {
	done := make(chan struct{})
	defer func() { close(done); _ = c.Close() }()
	// Unblock a pending read on shutdown; exits with the connection.
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), maxLine)

	_ = c.SetReadDeadline(time.Now().Add(helloTimeout))
	var hello Hello
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &hello) != nil ||
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(token)) != 1 {
		d.cfg.Log.Warn("rejected local connection: missing or wrong token")
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	enc := json.NewEncoder(c)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(Response{Error: "invalid request: " + err.Error()})
			continue
		}
		// An agent working in a running task's worktree is that task's
		// session, whatever id its agent gave it (Codex picks its own).
		// In a repository in a room, agent sessions are qualified with this
		// device's key, as the claims they hold on the server are.
		if (req.Method == MethodDecide || req.Method == MethodEvent) && req.Session != "" {
			if s, ok := d.runSession(req.Repo); ok {
				req.Session = s
				if req.Method == MethodEvent && (req.Event == "prompt" || req.Event == "stop") && d.sync != nil {
					d.agents.Add(1)
					go func(event, session string) { defer d.agents.Done(); d.ownTaskEvent(ctx, event, session) }(req.Event, s)
				}
			}
		}
		if (req.Method == MethodDecide || req.Method == MethodEvent) && req.Session != "" && d.sync != nil {
			if _, key, _, ok := d.sync.binding(req.Repo); ok {
				req.Session = key + "/" + req.Session
			}
		}
		var resp Response
		switch {
		case req.Method == MethodTaskCreate:
			resp = d.createTask(ctx, req)
		case req.Method == MethodTaskList:
			resp = d.listTasks(ctx, req)
		case req.Method == MethodTaskApprove || req.Method == MethodTaskDecline:
			resp = d.answerTask(ctx, req)
		case req.Method == MethodTaskMerge:
			resp = d.mergeTask(ctx, req)
		case req.Method == MethodTaskStart:
			resp = d.startOwnTask(ctx, req)
		case req.Method == MethodTaskCancel:
			resp = d.cancelTask(ctx, req)
		case req.Method == MethodTaskCheckpoint:
			resp = d.checkpointOwnTask(ctx, req)
		case req.Method == MethodDashboard:
			resp = d.dashboard(ctx, req)
		case req.Method == MethodClaimRequest || req.Method == MethodClaimRequests || req.Method == MethodClaimHandoff || req.Method == MethodClaimOverride:
			resp = d.handoff(ctx, req)
		case d.shared(req):
			resp = d.sharedClaim(ctx, req)
		default:
			reply := make(chan Response, 1)
			select {
			case d.calls <- call{req: req, reply: reply}:
			case <-ctx.Done():
				return
			}
			resp = <-reply
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// writeToken creates a fresh random token readable only by the user.
func writeToken(path string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	// Remove first so a leftover file with wider permissions is not reused.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("replace token file: %w", err)
	}
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("write token file: %w", err)
	}
	return token, nil
}
