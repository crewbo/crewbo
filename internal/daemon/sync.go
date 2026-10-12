package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/platform"
	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
	"github.com/crewbo/crewbo/internal/roomstate"
)

// Sync defaults (protocol.md, reconnect and resync).
const (
	minBackoff  = time.Second
	maxBackoff  = 30 * time.Second
	reloadEvery = 2 * time.Second
)

// syncer keeps this daemon's rooms in step with the sync server: it reads
// the session and room bindings saved by `crewbo login` and `crewbo room`,
// connects, replays each room from its saved cursor and applies events in
// order. Without a session or a bound room it stays local only, as in M2.
type syncer struct {
	paths platform.Paths
	db    *sql.DB // store.Daemon schema
	log   *slog.Logger
	// apply receives each event in order with the clock that maps its
	// server times to this machine.
	apply func(b auth.Binding, ev *crewbov1.Event, clock roomstate.Clock)
	// refused hears about a queued claim request the server turned down
	// after the member stopped waiting (for example one made offline).
	refused func(b auth.Binding, claimIDs []string, reason string)

	minBackoff, maxBackoff, reloadEvery time.Duration

	// cursors is each room's last applied seq. It lives in memory, so a
	// new daemon replays every room from the start and rebuilds its claims.
	// ponytail: full replay per start; persist the claims or use server
	// snapshots when room logs grow large.
	cursors map[string]uint64
	kick    chan struct{} // new outbox entries to send

	mu        sync.Mutex
	state     string
	current   config // last loaded config
	connected bool
	waiters   map[string]chan *crewbov1.Envelope // envelope id to reply
}

func newSyncer(p platform.Paths, db *sql.DB, log *slog.Logger, apply func(auth.Binding, *crewbov1.Event, roomstate.Clock), refused func(auth.Binding, []string, string)) *syncer {
	return &syncer{
		paths: p, db: db, log: log, apply: apply, refused: refused,
		minBackoff: minBackoff, maxBackoff: maxBackoff, reloadEvery: reloadEvery, state: "starting",
		cursors: map[string]uint64{}, kick: make(chan struct{}, 1), waiters: map[string]chan *crewbov1.Envelope{},
	}
}

// State describes the sync connection for `crewbo status`.
func (s *syncer) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *syncer) setState(format string, a ...any) {
	st := fmt.Sprintf(format, a...)
	s.mu.Lock()
	changed := s.state != st
	s.state = st
	s.mu.Unlock()
	if changed {
		s.log.Info("sync: " + st)
	}
}

// config is what the syncer connects with, read from the config dir.
type config struct {
	session  auth.Session
	bindings []auth.Binding // only those on the session's server
	stamp    string         // changes when either file changes
}

func (s *syncer) load() (config, error) {
	var c config
	for _, p := range []string{auth.SessionPath(s.paths.Config), auth.BindingsPath(s.paths.Config)} {
		if fi, err := os.Stat(p); err == nil {
			c.stamp += fmt.Sprintf("%s:%d:%d;", p, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	sess, err := auth.LoadSession(auth.SessionPath(s.paths.Config))
	if err != nil {
		return c, err
	}
	c.session = sess
	all, err := auth.LoadBindings(auth.BindingsPath(s.paths.Config))
	if err != nil {
		return c, err
	}
	for _, b := range all {
		if b.Server == sess.Server {
			c.bindings = append(c.bindings, b)
		}
	}
	return c, nil
}

// Run syncs until ctx ends. It reconnects with backoff after any failure
// and restarts the connection when the session or bindings change.
func (s *syncer) Run(ctx context.Context) {
	backoff := s.minBackoff
	for ctx.Err() == nil {
		cfg, err := s.load()
		s.mu.Lock()
		s.current = cfg
		s.mu.Unlock()
		switch {
		case errors.Is(err, auth.ErrNoSession):
			s.setState("local only: not signed in (crewbo login)")
		case err != nil:
			s.setState("local only: %v", err)
		case len(cfg.bindings) == 0:
			s.setState("local only: no repository is in a room (crewbo room create or join)")
		default:
			welcomed, err := s.connect(ctx, cfg)
			if ctx.Err() != nil {
				return
			}
			if welcomed {
				backoff = s.minBackoff
			}
			var refused refusedError
			if errors.As(err, &refused) {
				s.setState("local only: %v", err)
				s.waitForChange(ctx, cfg.stamp)
				continue
			}
			if err != nil {
				s.setState("offline, retrying: %v", err)
				if !s.sleep(ctx, jitter(backoff), cfg.stamp) {
					continue // files changed: reconnect now
				}
				backoff = min(backoff*2, s.maxBackoff)
			}
			continue
		}
		s.waitForChange(ctx, cfg.stamp)
	}
}

// jitter spreads reconnects by plus or minus 20 percent.
func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64())) //nolint:gosec // jitter, not security
}

// sleep waits d; it returns false early if ctx ends or the files change.
func (s *syncer) sleep(ctx context.Context, d time.Duration, stamp string) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(s.reloadEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return true
		case <-tick.C:
			if c, _ := s.load(); c.stamp != stamp {
				return false
			}
		}
	}
}

func (s *syncer) waitForChange(ctx context.Context, stamp string) {
	for s.sleep(ctx, time.Hour, stamp) {
	}
}

// wsURL turns the server's http(s) URL into its WebSocket endpoint.
func wsURL(server string) string {
	if rest, ok := strings.CutPrefix(server, "https://"); ok {
		return "wss://" + rest + "/v1/ws"
	}
	return "ws://" + strings.TrimPrefix(server, "http://") + "/v1/ws"
}

// refusedError is the server turning this daemon away (expired session,
// not a member). Retrying cannot help until the member signs in again or
// changes rooms, so the syncer waits for those files to change.
type refusedError struct{ msg string }

func (e refusedError) Error() string { return "the server refused this daemon: " + e.msg }

// errResync means the event stream skipped a sequence number; reconnecting
// makes the server replay from the saved cursor.
var errResync = errors.New("an event was missed; reconnecting to replay")

// connect runs one connection until it fails or the config changes. It
// reports whether the server welcomed it.
func (s *syncer) connect(ctx context.Context, cfg config) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	c, resp, err := websocket.Dial(dctx, wsURL(cfg.session.Server), &websocket.DialOptions{HTTPClient: &http.Client{}})
	dcancel()
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return false, fmt.Errorf("cannot reach %s: %w", cfg.session.Server, err)
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(64 << 10)

	byRoom := map[string]auth.Binding{}
	hello := &crewbov1.Hello{ProtocolVersion: 1, SessionToken: cfg.session.SessionToken, DeviceKeyId: cfg.session.DeviceKeyID}
	for _, b := range cfg.bindings {
		byRoom[b.RoomID] = b
		hello.Rooms = append(hello.Rooms, &crewbov1.RoomCursor{RoomId: b.RoomID, LastSeq: s.cursors[b.RoomID]})
	}
	if err := writeEnv(ctx, c, &crewbov1.Envelope{Id: "hello", Body: &crewbov1.Envelope_Hello{Hello: hello}}); err != nil {
		return false, err
	}
	env, err := readEnv(ctx, c)
	if err != nil {
		return false, err
	}
	if e := env.GetError(); e != nil {
		return false, refusedError{e.GetMessage()}
	}
	w := env.GetWelcome()
	if w == nil {
		return false, errors.New("the server did not answer Hello with Welcome")
	}
	// Lease ends are in server time; shift them by the measured offset.
	offset := time.Until(time.UnixMilli(w.GetServerTimeUnixMs()))
	clock := func(ms int64) time.Time { return time.UnixMilli(ms).Add(-offset) }
	s.setState("connected to %s as %s, %d room(s)", cfg.session.Server, cfg.session.GitHubLogin, len(cfg.bindings))

	var helpers sync.WaitGroup
	defer func() {
		cancel()
		s.mu.Lock()
		s.connected = false
		s.mu.Unlock()
		helpers.Wait()
	}()
	// Restart the connection when the session or bindings change.
	helpers.Add(1)
	go func() {
		defer helpers.Done()
		if !s.sleep(ctx, 1<<62, cfg.stamp) && ctx.Err() == nil {
			cancel()
		}
	}()
	// Send the outbox now and whenever something is queued.
	s.mu.Lock()
	s.connected = true
	s.mu.Unlock()
	helpers.Add(1)
	go func() {
		defer helpers.Done()
		s.sendOutbox(ctx, c, cancel)
	}()

	for {
		env, err := readEnv(ctx, c)
		if err != nil {
			if ctx.Err() != nil {
				return true, nil // config changed or shutting down
			}
			return true, err
		}
		if id := replyID(env); id != "" {
			s.settle(ctx, id, env, byRoom)
			continue
		}
		ev := env.GetEvent()
		if ev == nil {
			continue
		}
		b, ok := byRoom[ev.GetRoomId()]
		if !ok {
			continue
		}
		next, err := advance(s.cursors[b.RoomID], ev.GetSeq())
		if err != nil {
			return true, err
		}
		if next == s.cursors[b.RoomID] {
			continue // duplicate
		}
		s.apply(b, ev, clock)
		s.cursors[b.RoomID] = next
	}
}

func replyID(env *crewbov1.Envelope) string {
	if a := env.GetAck(); a != nil {
		return a.GetRequestId()
	}
	return env.GetError().GetRequestId()
}

// sendOutbox writes every queued request in order, then each new one as it
// is queued, until ctx ends. A request stays queued until its reply.
func (s *syncer) sendOutbox(ctx context.Context, c *websocket.Conn, cancel context.CancelFunc) {
	sent := map[string]bool{}
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT envelope_id, payload FROM outbox ORDER BY seq`)
		if err != nil {
			cancel()
			return
		}
		var batch [][]byte
		for rows.Next() {
			var id string
			var payload []byte
			if rows.Scan(&id, &payload) == nil && !sent[id] {
				sent[id] = true
				batch = append(batch, payload)
			}
		}
		_ = rows.Close()
		for _, p := range batch {
			if err := c.Write(ctx, websocket.MessageText, p); err != nil {
				cancel()
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		}
	}
}

// settle handles the reply to a queued request: it leaves the outbox, and
// goes to whoever waits for it, or, if nobody does and the server refused
// a claim, to the refused callback.
func (s *syncer) settle(ctx context.Context, id string, reply *crewbov1.Envelope, byRoom map[string]auth.Binding) {
	var payload []byte
	_ = s.db.QueryRowContext(ctx, `SELECT payload FROM outbox WHERE envelope_id = ?`, id).Scan(&payload)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE envelope_id = ?`, id); err != nil {
		s.log.Warn("sync: outbox", "err", err)
	}
	s.mu.Lock()
	w, waiting := s.waiters[id]
	delete(s.waiters, id)
	s.mu.Unlock()
	if waiting {
		w <- reply
		return
	}
	e := reply.GetError()
	if e == nil || payload == nil {
		return
	}
	req := &crewbov1.Envelope{}
	if protojson.Unmarshal(payload, req) != nil || req.GetClaimRequest() == nil {
		s.log.Warn("sync: the server refused a queued request", "reason", e.GetMessage())
		return
	}
	if b, ok := byRoom[req.GetClaimRequest().GetRoomId()]; ok {
		s.refused(b, req.GetClaimRequest().GetClaimIds(), e.GetMessage())
	}
}

// binding returns the room binding for a repository on the signed-in
// server, with the device key to qualify session ids and the member's
// GitHub login.
func (s *syncer) binding(repo string) (b auth.Binding, deviceKey, login string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := RepoKey(repo)
	for _, b := range s.current.bindings {
		if RepoKey(b.Repo) == key && s.current.session.DeviceKeyID != "" {
			return b, s.current.session.DeviceKeyID, s.current.session.GitHubLogin, true
		}
	}
	return auth.Binding{}, "", "", false
}

// errNotSent means the request is queued but the server has not answered
// yet (offline, or slow); it will be sent and settled later.
var errNotSent = errors.New("queued; the sync server has not answered yet")

// submit queues env durably, sends it if connected, and waits up to wait
// for the reply. Offline it returns errNotSent at once.
func (s *syncer) submit(ctx context.Context, env *crewbov1.Envelope, wait time.Duration) (*crewbov1.Envelope, error) {
	payload, err := protojson.Marshal(env)
	if err != nil {
		return nil, err
	}
	reply := make(chan *crewbov1.Envelope, 1)
	s.mu.Lock()
	s.waiters[env.GetId()] = reply
	connected := s.connected
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, env.GetId())
		s.mu.Unlock()
	}()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO outbox (envelope_id, payload, created_unix_ms) VALUES (?, ?, ?)`, env.GetId(), payload, time.Now().UnixMilli()); err != nil {
		return nil, fmt.Errorf("queue request: %w", err)
	}
	select {
	case s.kick <- struct{}{}:
	default: // a send is already pending
	}
	if !connected {
		return nil, errNotSent
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case r := <-reply:
		return r, nil
	case <-t.C:
		return nil, errNotSent
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// advance applies the ordering rule to one event: an already-applied seq is
// a duplicate (cursor unchanged), the next seq advances the cursor, and
// anything further ahead is a gap.
func advance(cursor, seq uint64) (uint64, error) {
	switch {
	case seq <= cursor:
		return cursor, nil
	case seq == cursor+1:
		return seq, nil
	default:
		return cursor, errResync
	}
}

func writeEnv(ctx context.Context, c *websocket.Conn, env *crewbov1.Envelope) error {
	b, err := protojson.Marshal(env)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func readEnv(ctx context.Context, c *websocket.Conn) (*crewbov1.Envelope, error) {
	_, b, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	env := &crewbov1.Envelope{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(b, env); err != nil {
		return nil, fmt.Errorf("the server sent an invalid frame: %w", err)
	}
	return env, nil
}
