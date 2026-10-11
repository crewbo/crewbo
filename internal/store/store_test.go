package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenCreatesMigratedWALDatabase(t *testing.T) {
	ctx := context.Background()
	db := open(t, filepath.Join(t.TempDir(), "crewbo.db"))
	v, err := db.SchemaVersion(ctx)
	if err != nil || v != len(migrations) {
		t.Fatalf("schema version = %d, %v; want %d", v, err, len(migrations))
	}
	var mode string
	if err := db.SQL().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v; want wal", mode, err)
	}
	var fk int
	if err := db.SQL().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; want 1", fk, err)
	}
	// The migrated schema accepts an outbox entry and enforces its constraints.
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox (envelope_id, payload, created_unix_ms) VALUES ('e1', x'00', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO outbox (envelope_id, payload, created_unix_ms) VALUES ('e1', x'00', 2)`); err == nil {
		t.Error("duplicate envelope_id accepted")
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO room_cursor (room_id, last_seq) VALUES ('r1', -1)`); err == nil {
		t.Error("negative last_seq accepted")
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crewbo.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO room_cursor (room_id, last_seq) VALUES ('r1', 7)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open(t, path)
	var seq int
	if err := db.SQL().QueryRowContext(ctx, `SELECT last_seq FROM room_cursor WHERE room_id = 'r1'`).Scan(&seq); err != nil || seq != 7 {
		t.Errorf("after reopen last_seq = %d, %v; want 7", seq, err)
	}
	if v, _ := db.SchemaVersion(ctx); v != len(migrations) {
		t.Errorf("schema version %d after reopen, want %d", v, len(migrations))
	}
}

func TestOpenRejectsNonDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crewbo.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database, it is plain text padded to look like a file header"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crewbo.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, "PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
}

func TestIsCorruption(t *testing.T) {
	if isCorruption(errors.New("file is not a database")) {
		t.Error("a plain error with the same text must not count; only driver codes do")
	}
}
