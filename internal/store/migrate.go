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
	// nothing twice. A task is pending until verified; task_id is set only
	// for verified tasks addressed to this member (state dispatched);
	// "other" tasks are for someone else; detail says why a task was
	// rejected.
	`CREATE TABLE tasks (
		digest           BLOB    PRIMARY KEY,
		task_id          TEXT    UNIQUE,
		room_id          TEXT    NOT NULL,
		signed           BLOB    NOT NULL,
		state            TEXT    NOT NULL CHECK (state IN ('pending', 'dispatched', 'rejected', 'other')),
		detail           TEXT    NOT NULL DEFAULT '',
		received_unix_ms INTEGER NOT NULL
	) WITHOUT ROWID;`,
	// 3: the member's answer to a task (approved, declined) or its expiry,
	// and when a verified task expires. SQLite cannot alter a CHECK, so the
	// table is rebuilt. Verified tasks go back to pending (task id cleared)
	// so the daemon verifies them again and records their expiry.
	`CREATE TABLE tasks_3 (
		digest           BLOB    PRIMARY KEY,
		task_id          TEXT    UNIQUE,
		room_id          TEXT    NOT NULL,
		signed           BLOB    NOT NULL,
		state            TEXT    NOT NULL CHECK (state IN ('pending', 'dispatched', 'rejected', 'other', 'approved', 'declined', 'expired')),
		detail           TEXT    NOT NULL DEFAULT '',
		received_unix_ms INTEGER NOT NULL,
		expires_unix_ms  INTEGER
	) WITHOUT ROWID;
	INSERT INTO tasks_3 (digest, task_id, room_id, signed, state, detail, received_unix_ms)
		SELECT digest, CASE state WHEN 'dispatched' THEN NULL ELSE task_id END, room_id, signed,
			CASE state WHEN 'dispatched' THEN 'pending' ELSE state END, detail, received_unix_ms FROM tasks;
	DROP TABLE tasks;
	ALTER TABLE tasks_3 RENAME TO tasks;`,
	// 4: task runs (M4-04): running, then finished (the agent exited
	// cleanly; checkpoints come with M5-01) or failed. The remaining
	// data-model.md states are allowed now so later milestones need no
	// rebuild.
	`CREATE TABLE tasks_4 (
		digest           BLOB    PRIMARY KEY,
		task_id          TEXT    UNIQUE,
		room_id          TEXT    NOT NULL,
		signed           BLOB    NOT NULL,
		state            TEXT    NOT NULL CHECK (state IN ('pending', 'dispatched', 'rejected', 'other', 'approved', 'declined', 'expired',
			'running', 'finished', 'failed', 'checkpointed', 'merging', 'merged', 'cancelled')),
		detail           TEXT    NOT NULL DEFAULT '',
		received_unix_ms INTEGER NOT NULL,
		expires_unix_ms  INTEGER
	) WITHOUT ROWID;
	INSERT INTO tasks_4 SELECT digest, task_id, room_id, signed, state, detail, received_unix_ms, expires_unix_ms FROM tasks;
	DROP TABLE tasks;
	ALTER TABLE tasks_4 RENAME TO tasks;`,
	// 5: task statuses from the room log (TaskStatusChanged), so a lead can
	// follow the tasks they sent; actor is the sender's GitHub login as the
	// server recorded it.
	`CREATE TABLE task_status (
		room_id TEXT    NOT NULL,
		seq     INTEGER NOT NULL,
		task_id TEXT    NOT NULL,
		actor   TEXT    NOT NULL,
		state   TEXT    NOT NULL,
		detail  TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (room_id, seq)
	) WITHOUT ROWID;
	CREATE INDEX task_status_task ON task_status (room_id, task_id, seq);`,
	// 6: checkpoint refs on task statuses, and the room's conflict
	// warnings (M5-02): paths only, as a JSON array.
	`ALTER TABLE task_status ADD COLUMN ref TEXT NOT NULL DEFAULT '';
	ALTER TABLE task_status ADD COLUMN commit_id TEXT NOT NULL DEFAULT '';
	CREATE TABLE conflict_warning (
		room_id         TEXT    NOT NULL,
		seq             INTEGER NOT NULL,
		task_id         TEXT    NOT NULL,
		other_task_id   TEXT    NOT NULL,
		actor           TEXT    NOT NULL,
		commit_id       TEXT    NOT NULL DEFAULT '',
		other_commit_id TEXT    NOT NULL DEFAULT '',
		paths           TEXT    NOT NULL,
		PRIMARY KEY (room_id, seq)
	) WITHOUT ROWID;`,
	// 7: tasks a member started themselves (ADR 0014, M5-07): own marks
	// them, and session is the agent session that holds their claims, so
	// a restarted daemon still knows the task's worktree is that session.
	`ALTER TABLE tasks ADD COLUMN own INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE tasks ADD COLUMN session TEXT NOT NULL DEFAULT '';`,
	// 8: where an own task is worked on when it is not the default
	// <data>/worktrees/<task id>: the member's own checkout of a branch
	// they registered (M5-07).
	`ALTER TABLE tasks ADD COLUMN worktree TEXT NOT NULL DEFAULT '';`,
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
	// 5: who may merge tasks in a room (ADR 0014): its members (default)
	// or only its lead.
	`ALTER TABLE rooms ADD COLUMN merge_mode TEXT NOT NULL DEFAULT 'members' CHECK (merge_mode IN ('members', 'lead'));`,
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
