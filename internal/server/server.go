// Package server is the sync server behind `crewbo serve`: an HTTP health
// check and the daemon WebSocket at /v1/ws carrying one Protobuf JSON
// Envelope per text frame (docs/architecture/protocol.md, ADR 0008).
package server

import (
	"context"
	"crypto/ed25519"
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
	rooms    *Rooms
	events   *Log

	// The hub: one running Room per room with a live connection.
	hubCtx  context.Context
	hubStop context.CancelFunc
	hubMu   sync.Mutex
	live    map[string]*Room
	hubWG   sync.WaitGroup
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
	hubCtx, hubStop := context.WithCancel(context.Background())
	return &Server{
		events:   NewLog(db),
		hubCtx:   hubCtx,
		hubStop:  hubStop,
		live:     map[string]*Room{},
		log:      log,
		sessions: NewSessions(db, time.Now),
		rooms:    NewRooms(db, time.Now),
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
	s.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler routes /healthz, /v1/auth/session, /v1/devices, /v1/rooms... and /v1/ws.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /v1/auth/session", s.serveSession)
	mux.HandleFunc("POST /v1/devices", s.serveDevice)
	mux.HandleFunc("POST /v1/rooms", s.serveCreateRoom)
	mux.HandleFunc("GET /v1/rooms", s.serveListRooms)
	mux.HandleFunc("POST /v1/rooms/join", s.serveJoin)
	mux.HandleFunc("POST /v1/rooms/{id}/invites", s.serveInvite)
	mux.HandleFunc("POST /v1/rooms/{id}/leave", s.serveLeave)
	mux.HandleFunc("GET /v1/rooms/{id}/keys", s.serveRoomKeys)
	mux.HandleFunc("GET /v1/info", s.serveInfo)
	mux.HandleFunc("POST /v1/rooms/{id}/settings", s.serveSettings)
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
	u, err := s.sessions.Lookup(ctx, hello.GetSessionToken())
	if err != nil {
		if !errors.Is(err, ErrUnauthenticated) {
			s.log.Error("ws: session lookup", "err", err)
		}
		s.reject(ctx, c, env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED, "sign in again with crewbo login")
		return
	}
	caller := Caller{Member: u.Login}
	if key := hello.GetDeviceKeyId(); key != "" {
		ok, err := s.deviceOwned(ctx, key, u.ID)
		if err != nil || !ok {
			s.reject(ctx, c, env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED, "this device's key is not registered to you; run crewbo login")
			return
		}
		caller.DeviceKey = key
	}
	subs, code, msg := s.subscribe(ctx, u.ID, hello.GetRooms())
	if code != 0 {
		s.reject(ctx, c, env.GetId(), code, msg)
		return
	}
	var helpers sync.WaitGroup // keepalive and forwarders; all end with ctx
	defer func() {
		cancel()
		for _, sub := range subs {
			sub.Close()
		}
		helpers.Wait()
	}()
	joined := map[string]bool{}
	for _, cur := range hello.GetRooms() {
		joined[cur.GetRoomId()] = true
	}
	welcome := &crewbov1.Welcome{ProtocolVersion: ProtocolVersion, ServerTimeUnixMs: time.Now().UnixMilli()}
	if err := send(ctx, c, &crewbov1.Envelope{Body: &crewbov1.Envelope_Welcome{Welcome: welcome}}); err != nil {
		return
	}
	// Replay each room's missed events, then forward live ones. Live events
	// committed meanwhile wait in the subscription, so none is lost.
	for _, sub := range subs {
		for _, ev := range sub.Backlog {
			if err := send(ctx, c, &crewbov1.Envelope{Body: &crewbov1.Envelope_Event{Event: ev}}); err != nil {
				return
			}
		}
	}
	for _, sub := range subs {
		helpers.Add(1)
		go func() { defer helpers.Done(); s.forward(ctx, c, sub) }()
	}
	helpers.Add(1)
	go func() {
		defer helpers.Done()
		s.keepalive(ctx, c)
		cancel()
	}()

	for {
		env, err := read(ctx, c)
		if err != nil {
			s.log.Debug("ws: closed", "err", err)
			return
		}
		var roomID string
		switch b := env.GetBody().(type) {
		case *crewbov1.Envelope_ClaimRequest:
			roomID = b.ClaimRequest.GetRoomId()
		case *crewbov1.Envelope_ClaimRelease:
			roomID = b.ClaimRelease.GetRoomId()
		case *crewbov1.Envelope_ClaimRenew:
			roomID = b.ClaimRenew.GetRoomId()
		case *crewbov1.Envelope_DispatchTask:
			roomID = b.DispatchTask.GetRoomId()
		case *crewbov1.Envelope_TaskStatus:
			roomID = b.TaskStatus.GetRoomId()
		case *crewbov1.Envelope_ConflictWarning:
			roomID = b.ConflictWarning.GetRoomId()
		case *crewbov1.Envelope_HandoffRequest:
			roomID = b.HandoffRequest.GetRoomId()
		case *crewbov1.Envelope_HandoffAnswer:
			roomID = b.HandoffAnswer.GetRoomId()
		case *crewbov1.Envelope_LeadOverride:
			roomID = b.LeadOverride.GetRoomId()
		default:
			// The protocol answers unknown requests with UNSUPPORTED.
			if err := send(ctx, c, errorEnvelope(env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNSUPPORTED, "not supported by this server")); err != nil {
				return
			}
			continue
		}
		reply := errorEnvelope(env.GetId(), crewbov1.ErrorCode_ERROR_CODE_NOT_MEMBER, "this connection has not joined room "+roomID)
		if joined[roomID] {
			if refusal := s.checkDispatch(ctx, u.ID, caller, env); refusal != nil {
				reply = refusal
			} else if refusal := s.checkLead(ctx, u.ID, env); refusal != nil {
				reply = refusal
			} else {
				reply = s.room(roomID).Handle(ctx, caller, env)
			}
		}
		if err := send(ctx, c, reply); err != nil {
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

// checkDispatch admits a DispatchTask only from the room's lead, signed by
// the device key that lead presented in Hello. The server does not decode
// the task: teammates' daemons verify everything else (ADR 0006). It
// returns nil for other requests and for an admitted task.
func (s *Server) checkDispatch(ctx context.Context, userID int64, caller Caller, env *crewbov1.Envelope) *crewbov1.Envelope {
	d := env.GetDispatchTask()
	if d == nil {
		return nil
	}
	refuse := func(code crewbov1.ErrorCode, msg string) *crewbov1.Envelope {
		return errorEnvelope(env.GetId(), code, msg)
	}
	lead, err := s.rooms.IsLead(ctx, d.GetRoomId(), userID)
	switch {
	case err != nil:
		s.log.Error("dispatch: lead check", "err", err)
		return refuse(crewbov1.ErrorCode_ERROR_CODE_UNSPECIFIED, "internal error")
	case !lead:
		return refuse(crewbov1.ErrorCode_ERROR_CODE_NOT_LEAD, "only the room lead can send tasks")
	case d.GetTask() == nil || len(d.GetTask().GetBody()) == 0:
		return refuse(crewbov1.ErrorCode_ERROR_CODE_INVALID, "no task")
	case caller.DeviceKey == "" || d.GetTask().GetKeyId() != caller.DeviceKey:
		return refuse(crewbov1.ErrorCode_ERROR_CODE_INVALID, "the task must be signed by this device's key")
	}
	pub, err := s.devicePublicKey(ctx, caller.DeviceKey, userID)
	if err != nil {
		return refuse(crewbov1.ErrorCode_ERROR_CODE_UNAUTHENTICATED, "this device's key is not registered to you")
	}
	if !ed25519.Verify(pub, d.GetTask().GetBody(), d.GetTask().GetSignature()) {
		return refuse(crewbov1.ErrorCode_ERROR_CODE_INVALID, "the task's signature does not match its body")
	}
	return nil
}

// checkLead admits a LeadOverride only from the room's lead. It returns nil
// for other requests and for an admitted override.
func (s *Server) checkLead(ctx context.Context, userID int64, env *crewbov1.Envelope) *crewbov1.Envelope {
	o := env.GetLeadOverride()
	if o == nil {
		return nil
	}
	lead, err := s.rooms.IsLead(ctx, o.GetRoomId(), userID)
	switch {
	case err != nil:
		s.log.Error("override: lead check", "err", err)
		return errorEnvelope(env.GetId(), crewbov1.ErrorCode_ERROR_CODE_UNSPECIFIED, "internal error")
	case !lead:
		return errorEnvelope(env.GetId(), crewbov1.ErrorCode_ERROR_CODE_NOT_LEAD, "only the room lead can override a claim")
	}
	return nil
}
