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

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/internal/platform"
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
}

// Daemon is one running crewbo daemon.
type Daemon struct {
	cfg     Config
	started time.Time
	calls   chan call
	// sessions and claims are owned by the owner goroutine.
	sessions map[string]*Session
	claims   map[string]*claims.Set // keyed by RepoKey
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
	d := &Daemon{cfg: cfg, calls: make(chan call), sessions: map[string]*Session{}, claims: map[string]*claims.Set{}}
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
	var conns sync.WaitGroup
	ownerDone := make(chan struct{})
	go func() { defer close(ownerDone); d.own(ctx) }()
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
	<-ownerDone
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
		n := 0
		set := d.set(req.Repo)
		for _, id := range req.IDs {
			if set.Release(id) {
				n++
			}
		}
		if n == 0 && len(req.IDs) > 0 {
			return Response{ID: req.ID, Error: "no matching claims in this repository"}
		}
		return Response{ID: req.ID, Released: n}
	case MethodStatus:
		return Response{ID: req.ID, Status: &Status{
			Version: d.cfg.Version, PID: os.Getpid(), Started: d.started,
			ActiveClaims: d.activeClaims(now), Sessions: len(d.sessions),
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

// RepoKey identifies a repository across path spellings: symlinks resolved
// (macOS /tmp is /private/tmp), forward slashes, case folded (Windows tools
// disagree on drive-letter case). Claims are per repository, so a hook and
// a terminal in the same checkout must map to the same key.
func RepoKey(repo string) string {
	if repo == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(repo)))
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
		reply := make(chan Response, 1)
		select {
		case d.calls <- call{req: req, reply: reply}:
		case <-ctx.Done():
			return
		}
		if err := enc.Encode(<-reply); err != nil {
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
