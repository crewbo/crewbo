// Package server is the sync server behind `crewbo serve`: an HTTP health
// check and the daemon WebSocket at /v1/ws carrying one Protobuf JSON
// Envelope per text frame (docs/architecture/protocol.md, ADR 0008).
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/crewbo/crewbo/internal/auth"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// ProtocolVersion is the major protocol version this server speaks.
const ProtocolVersion = 1

// maxEnvelope is the protocol's Envelope size limit; larger frames close
// the connection with status 1009 (message too big).
const maxEnvelope = 64 << 10

// ErrInsecure refuses plain HTTP on a non-loopback address without
// --insecure-lan.
var ErrInsecure = errors.New("server: refusing plain HTTP on a non-loopback address; pass --tls-cert and --tls-key, or --insecure-lan on a trusted network")

// Config says where and how to listen.
type Config struct {
	Addr string
	// CertFile and KeyFile enable TLS; both or neither.
	CertFile, KeyFile string
	// InsecureLAN allows plain HTTP on a non-loopback address.
	InsecureLAN bool
}

// Server serves the health check, sign-in and daemon connections.
type Server struct {
	log      *slog.Logger
	sessions *Sessions
	// lookupUser asks GitHub whose token it is; tests replace it.
	lookupUser func(ctx context.Context, githubToken string) (auth.User, error)
	// HelloWait is how long a new connection may take to send Hello.
	HelloWait time.Duration
	// PingEvery and PongWait keep connections alive: a ping every PingEvery,
	// closed when no pong arrives within PongWait.
	PingEvery, PongWait time.Duration

	wg sync.WaitGroup // connection handlers, including hijacked ones
}

// New returns a Server over a database opened with store.Server, with the
// protocol's keepalive defaults.
func New(log *slog.Logger, db *sql.DB) *Server {
	github := &http.Client{Timeout: 10 * time.Second}
	return &Server{
		log:      log,
		sessions: NewSessions(db, time.Now),
		lookupUser: func(ctx context.Context, token string) (auth.User, error) {
			return auth.LookupUser(ctx, github, auth.APIURL, token)
		},
		HelloWait: 10 * time.Second, PingEvery: 20 * time.Second, PongWait: 60 * time.Second,
	}
}

// Run listens on cfg.Addr and serves until ctx is cancelled. ready, if not
// nil, receives the bound address once listening.
func (s *Server) Run(ctx context.Context, cfg Config, ready func(net.Addr)) error {
	tls := cfg.CertFile != "" || cfg.KeyFile != ""
	if tls && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return errors.New("server: --tls-cert and --tls-key go together")
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("server: listen: %w", err)
	}
	if !tls {
		if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
			if !cfg.InsecureLAN {
				_ = ln.Close()
				return ErrInsecure
			}
			s.log.Warn("serving plain HTTP on a non-loopback address (--insecure-lan); session tokens can be read by anyone on this network", "addr", ln.Addr().String())
		}
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	stop := context.AfterFunc(ctx, func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	})
	defer stop()
	s.log.Info("crewbo serve listening", "addr", ln.Addr().String(), "tls", tls)
	if ready != nil {
		ready(ln.Addr())
	}
	if tls {
		err = srv.ServeTLS(ln, cfg.CertFile, cfg.KeyFile)
	} else {
		err = srv.Serve(ln)
	}
	// Shutdown does not wait for hijacked WebSocket connections; their
	// contexts derive from ctx, so they end now.
	s.wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler routes /healthz, /v1/auth/session, /v1/devices and /v1/ws.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /v1/auth/session", s.serveSession)
	mux.HandleFunc("POST /v1/devices", s.serveDevice)
	mux.HandleFunc("GET /v1/ws", s.serveWS)
	return mux
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	s.wg.Add(1)
	defer s.wg.Done()
	// Default options refuse cross-origin browser requests; daemons send
	// no Origin header.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has written the HTTP error
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(maxEnvelope)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	hctx, hcancel := context.WithTimeout(ctx, s.HelloWait)
	env, err := read(hctx, c)
	hcancel()
	if err != nil {
		s.log.Debug("ws: no hello", "err", err)
		return
	}
	hello := env.GetHello()
	switch {
	case hello == nil:
		s.reject(ctx, c, env.GetId(), crewbov1.ErrorCode_ERROR_CODE_INVALID, "the first frame must be Hello")
		return
	case hello.GetProtocolVersion() != ProtocolVersion:
		s.reject(ctx, c, env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UPGRADE_REQUIRED,
			fmt.Sprintf("this server speaks protocol version %d; update crewbo", ProtocolVersion))
		return
	}
	if _, err := s.sessions.Lookup(ctx, hello.GetSessionToken()); err != nil {
		if !errors.Is(err, ErrUnauthenticated) {
			s.log.Error("ws: session lookup", "err", err)
		}
		s.reject(ctx, c, env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED, "sign in again with crewbo login")
		return
	}
	if err := send(ctx, c, &crewbov1.Envelope{Body: &crewbov1.Envelope_Welcome{Welcome: &crewbov1.Welcome{ProtocolVersion: ProtocolVersion}}}); err != nil {
		return
	}

	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		s.keepalive(ctx, c)
		cancel()
	}()
	defer func() { cancel(); <-pingDone }()

	for {
		env, err := read(ctx, c)
		if err != nil {
			s.log.Debug("ws: closed", "err", err)
			return
		}
		// No request types are served yet; rooms and claims come in M3-02
		// and M3-07. The protocol answers unknown requests with UNSUPPORTED.
		if err := send(ctx, c, errorEnvelope(env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNSUPPORTED, "not supported by this server yet")); err != nil {
			return
		}
	}
}

// keepalive pings until ctx ends or a pong does not arrive in time.
func (s *Server) keepalive(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(s.PingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, s.PongWait)
		err := c.Ping(pctx)
		cancel()
		if err != nil {
			s.log.Debug("ws: no pong", "err", err)
			_ = c.Close(websocket.StatusPolicyViolation, "no pong")
			return
		}
	}
}

func (s *Server) reject(ctx context.Context, c *websocket.Conn, id string, code crewbov1.ErrorCode, msg string) {
	_ = send(ctx, c, errorEnvelope(id, code, msg))
	_ = c.Close(websocket.StatusPolicyViolation, msg)
}

func errorEnvelope(id string, code crewbov1.ErrorCode, msg string) *crewbov1.Envelope {
	return &crewbov1.Envelope{Body: &crewbov1.Envelope_Error{Error: &crewbov1.Error{RequestId: id, Code: code, Message: msg}}}
}

// read returns the next Envelope. A binary or undecodable frame closes the
// connection: receivers ignore unknown fields, never malformed ones.
func read(ctx context.Context, c *websocket.Conn) (*crewbov1.Envelope, error) {
	typ, b, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		_ = c.Close(websocket.StatusUnsupportedData, "envelopes are text frames")
		return nil, errors.New("binary frame")
	}
	env := &crewbov1.Envelope{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(b, env); err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid envelope")
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	return env, nil
}

func send(ctx context.Context, c *websocket.Conn, env *crewbov1.Envelope) error {
	b, err := protojson.Marshal(env)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}
