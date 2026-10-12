package store

import (
	"context"
	"fmt"
)

// Schema is the ordered migrations of one kind of database. Open applies
// them in order and records how many have run in PRAGMA user_version.
// Never edit or reorder an entry once released: add a new one. Daemon and
// server databases are separate files with separate schemas.
type Schema []string

// Daemon is the schema of a member's daemon database.
var Daemon = Schema{
	// 1: daemon outbox for requests queued while the sync server is
	// unreachable (protocol.md, reconnect and resync), and the last applied
	// event sequence number per room.
	`CREATE TABLE outbox (
		seq          INTEGER PRIMARY KEY AUTOINCREMENT,
		envelope_id  TEXT    NOT NULL UNIQUE,
		payload      BLOB    NOT NULL,
		created_unix_ms INTEGER NOT NULL
	);
	CREATE TABLE room_cursor (
		room_id  TEXT    PRIMARY KEY,
		last_seq INTEGER NOT NULL CHECK (last_seq >= 0)
	);`,
}

// migrate applies pending migrations, each in its own transaction together
// with the user_version bump, so a crash never leaves a half-applied step.
func (db *DB) migrate(ctx context.Context, migrations Schema) error {
	current, err := db.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current > len(migrations) {
		return fmt.Errorf("%w: database at version %d, this build knows %d", ErrSchemaTooNew, current, len(migrations))
	}
	for v := current; v < len(migrations); v++ {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		// PRAGMA does not accept bound parameters; v is our own integer.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
	}
	return nil
}
