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
	// apply receives each event in order; M3-07 turns events into claims.
	apply func(b auth.Binding, ev *crewbov1.Event)

	minBackoff, maxBackoff, reloadEvery time.Duration

	mu    sync.Mutex
	state string
}

func newSyncer(p platform.Paths, db *sql.DB, log *slog.Logger, apply func(auth.Binding, *crewbov1.Event)) *syncer {
	return &syncer{paths: p, db: db, log: log, apply: apply, minBackoff: minBackoff, maxBackoff: maxBackoff, reloadEvery: reloadEvery, state: "starting"}
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

	cursors := map[string]uint64{}
	byRoom := map[string]auth.Binding{}
	hello := &crewbov1.Hello{ProtocolVersion: 1, SessionToken: cfg.session.SessionToken, DeviceKeyId: cfg.session.DeviceKeyID}
	for _, b := range cfg.bindings {
		seq, err := s.cursor(ctx, b.RoomID)
		if err != nil {
			return false, err
		}
		cursors[b.RoomID], byRoom[b.RoomID] = seq, b
		hello.Rooms = append(hello.Rooms, &crewbov1.RoomCursor{RoomId: b.RoomID, LastSeq: seq})
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
	if env.GetWelcome() == nil {
		return false, errors.New("the server did not answer Hello with Welcome")
	}
	s.setState("connected to %s as %s, %d room(s)", cfg.session.Server, cfg.session.GitHubLogin, len(cfg.bindings))

	// Restart the connection when the session or bindings change.
	watch := make(chan struct{})
	go func() {
		defer close(watch)
		if !s.sleep(ctx, 1<<62, cfg.stamp) && ctx.Err() == nil {
			cancel()
		}
	}()
	defer func() { cancel(); <-watch }()

	for {
		env, err := readEnv(ctx, c)
		if err != nil {
			if ctx.Err() != nil {
				return true, nil // config changed or shutting down
			}
			return true, err
		}
		ev := env.GetEvent()
		if ev == nil {
			continue // replies to requests arrive in M3-07
		}
		b, ok := byRoom[ev.GetRoomId()]
		if !ok {
			continue
		}
		next, err := advance(cursors[b.RoomID], ev.GetSeq())
		if err != nil {
			return true, err
		}
		if next == cursors[b.RoomID] {
			continue // duplicate
		}
		s.apply(b, ev)
		if err := s.saveCursor(ctx, b.RoomID, next); err != nil {
			return true, err
		}
		cursors[b.RoomID] = next
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

func (s *syncer) cursor(ctx context.Context, roomID string) (uint64, error) {
	var seq uint64
	err := s.db.QueryRowContext(ctx, `SELECT last_seq FROM room_cursor WHERE room_id = ?`, roomID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq, err
}

func (s *syncer) saveCursor(ctx context.Context, roomID string, seq uint64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO room_cursor (room_id, last_seq) VALUES (?, ?) ON CONFLICT (room_id) DO UPDATE SET last_seq = excluded.last_seq`, roomID, seq)
	return err
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
