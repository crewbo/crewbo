package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

// ErrNoRoom means the room does not exist.
var ErrNoRoom = errors.New("server: no such room")

// Log is the server's persistent, append-only event log per room, in a
// database opened with store.Server.
type Log struct {
	db *sql.DB
}

// NewLog returns a Log over db.
func NewLog(db *sql.DB) *Log { return &Log{db: db} }

// CreateRoom adds an empty room whose first event will get seq 1.
func (l *Log) CreateRoom(ctx context.Context, id, name string, now time.Time) error {
	_, err := l.db.ExecContext(ctx, `INSERT INTO rooms (id, name, created_unix_ms) VALUES (?, ?, ?)`, id, name, now.UnixMilli())
	if err != nil {
		return fmt.Errorf("create room %s: %w", id, err)
	}
	return nil
}

// Append commits a copy of ev to room roomID with the next sequence number
// and the server time, and returns that copy. The seq bump and the insert
// share a transaction, so a failed append leaves no gap.
func (l *Log) Append(ctx context.Context, roomID string, ev *crewbov1.Event, now time.Time) (*crewbov1.Event, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	var seq uint64
	// RETURNING needs SQLite 3.35.0 (https://www.sqlite.org/lang_returning.html);
	// modernc.org/sqlite v1.60.1 bundles 3.53.4.
	err = tx.QueryRowContext(ctx, `UPDATE rooms SET next_seq = next_seq + 1 WHERE id = ? RETURNING next_seq - 1`, roomID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNoRoom, roomID)
	}
	if err != nil {
		return nil, fmt.Errorf("append to %s: %w", roomID, err)
	}
	out := proto.CloneOf(ev)
	out.RoomId = roomID
	out.Seq = seq
	out.ServerTimeUnixMs = now.UnixMilli()
	payload, err := proto.Marshal(out)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (room_id, seq, payload) VALUES (?, ?, ?)`, out.GetRoomId(), seq, payload); err != nil {
		return nil, fmt.Errorf("append to %s: %w", out.GetRoomId(), err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("append to %s: %w", out.GetRoomId(), err)
	}
	return out, nil
}

// Since returns the room's events with seq greater than after, in order.
// ponytail: returns the whole tail; add a limit and snapshots (Welcome)
// when retention and compaction arrive.
func (l *Log) Since(ctx context.Context, roomID string, after uint64) ([]*crewbov1.Event, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT payload FROM events WHERE room_id = ? AND seq > ? ORDER BY seq`, roomID, after)
	if err != nil {
		return nil, fmt.Errorf("replay %s: %w", roomID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*crewbov1.Event
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		ev := &crewbov1.Event{}
		if err := proto.Unmarshal(payload, ev); err != nil {
			return nil, fmt.Errorf("replay %s: stored event: %w", roomID, err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
