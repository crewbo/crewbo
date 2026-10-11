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
	Claims  *claims.Set      // initial claim set; empty when nil
	Now     func() time.Time // clock; time.Now when nil
	Log     *slog.Logger
}

// Daemon is one running crewbo daemon.
type Daemon struct {
	cfg     Config
	started time.Time
	calls   chan call
}

type call struct {
	req   Request
	reply chan Response
}

// New returns a Daemon; call Run to start it.
func New(cfg Config) *Daemon {
	if cfg.Claims == nil {
		cfg.Claims = claims.NewSet()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	return &Daemon{cfg: cfg, calls: make(chan call)}
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
	case MethodDecide:
		if req.Session == "" {
			return Response{ID: req.ID, Error: "decide: session is required"}
		}
		dec := d.cfg.Claims.Decide(req.Session, req.Paths, now)
		resp := Response{ID: req.ID, Allow: dec.Allow, Path: dec.Path}
		if !dec.Allow {
			blocked := dec.BlockedBy
			resp.BlockedBy = &blocked
		}
		return resp
	case MethodStatus:
		return Response{ID: req.ID, Status: &Status{
			Version: d.cfg.Version, PID: os.Getpid(), Started: d.started,
			ActiveClaims: len(d.cfg.Claims.Active(now)),
		}}
	default:
		return Response{ID: req.ID, Error: fmt.Sprintf("unknown method %q", req.Method)}
	}
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
