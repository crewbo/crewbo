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
	// 2: signed tasks received from the room log (ADR 0006), keyed by a
	// SHA-256 digest of the signed bytes so a replayed room log stores
	// nothing twice. task_id is set only for verified tasks addressed to
	// this member (state dispatched); "other" tasks are for someone else;
	// detail says why a task was rejected.
	`CREATE TABLE tasks (
		digest           BLOB    PRIMARY KEY,
		task_id          TEXT    UNIQUE,
		room_id          TEXT    NOT NULL,
		signed           BLOB    NOT NULL,
		state            TEXT    NOT NULL CHECK (state IN ('dispatched', 'rejected', 'other')),
		detail           TEXT    NOT NULL DEFAULT '',
		received_unix_ms INTEGER NOT NULL
	) WITHOUT ROWID;`,
}

// Server is the schema of the sync server's database (`crewbo serve`).
var Server = Schema{
	// 1: rooms and their append-only event logs (data-model.md). next_seq
	// only increases; (room_id, seq) is unique and gap-free because the
	// event insert and the next_seq bump share one transaction. payload is
	// the binary Protobuf crewbo.v1.Event.
	`CREATE TABLE rooms (
		id              TEXT    PRIMARY KEY,
		name            TEXT    NOT NULL,
		created_unix_ms INTEGER NOT NULL,
		next_seq        INTEGER NOT NULL DEFAULT 1 CHECK (next_seq >= 1)
	);
	CREATE TABLE events (
		room_id TEXT    NOT NULL REFERENCES rooms (id),
		seq     INTEGER NOT NULL CHECK (seq >= 1),
		payload BLOB    NOT NULL,
		PRIMARY KEY (room_id, seq)
	) WITHOUT ROWID;`,
	// 2: Crewbo session tokens issued after a GitHub sign-in (ADR 0009).
	// Only a SHA-256 hash of each token is stored; the GitHub token is not
	// stored at all.
	`CREATE TABLE sessions (
		token_hash      BLOB    PRIMARY KEY,
		github_user_id  INTEGER NOT NULL,
		github_login    TEXT    NOT NULL,
		created_unix_ms INTEGER NOT NULL,
		expires_unix_ms INTEGER NOT NULL
	) WITHOUT ROWID;`,
	// 3: device public keys registered by members (ADR 0006). key_id is
	// derived from public_key by the server, never taken from the client.
	`CREATE TABLE device_keys (
		key_id          TEXT    PRIMARY KEY,
		github_user_id  INTEGER NOT NULL,
		public_key      BLOB    NOT NULL CHECK (length(public_key) = 32),
		name            TEXT    NOT NULL,
		os              TEXT    NOT NULL,
		created_unix_ms INTEGER NOT NULL,
		revoked_unix_ms INTEGER
	) WITHOUT ROWID;`,
	// 4: room membership and invites (data-model.md). A member is a GitHub
	// account in a room; leaving sets left_unix_ms. The partial indexes
	// allow one active membership per account per room and one active lead
	// per room. Invite codes are stored only as SHA-256 hashes.
	`CREATE TABLE members (
		room_id        TEXT    NOT NULL REFERENCES rooms (id),
		github_user_id INTEGER NOT NULL,
		github_login   TEXT    NOT NULL,
		role           TEXT    NOT NULL CHECK (role IN ('lead', 'member')),
		joined_unix_ms INTEGER NOT NULL,
		left_unix_ms   INTEGER
	);
	CREATE UNIQUE INDEX members_active ON members (room_id, github_user_id) WHERE left_unix_ms IS NULL;
	CREATE UNIQUE INDEX members_one_lead ON members (room_id) WHERE role = 'lead' AND left_unix_ms IS NULL;
	CREATE TABLE invites (
		code_hash       BLOB    PRIMARY KEY,
		room_id         TEXT    NOT NULL REFERENCES rooms (id),
		created_by      INTEGER NOT NULL,
		created_unix_ms INTEGER NOT NULL,
		expires_unix_ms INTEGER NOT NULL,
		max_uses        INTEGER NOT NULL CHECK (max_uses >= 1),
		uses            INTEGER NOT NULL DEFAULT 0 CHECK (uses <= max_uses)
	) WITHOUT ROWID;`,
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
