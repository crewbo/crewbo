// Package store is Crewbo's SQLite persistence: opening the database with
// the settings Crewbo relies on, detecting damage, and applying schema
// migrations. It uses modernc.org/sqlite, a pure-Go SQLite, because release
// builds use CGO_ENABLED=0 (ADR 0002, ADR 0008).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite" // also registers the "sqlite" driver
	sqlite3 "modernc.org/sqlite/lib"
)

// Errors returned by Open.
var (
	// ErrCorrupt means the file is not a usable SQLite database or failed
	// its integrity check (failure mode F19). The caller moves it aside and
	// starts empty.
	ErrCorrupt = errors.New("store: database is damaged")
	// ErrSchemaTooNew means the database was written by a newer Crewbo.
	ErrSchemaTooNew = errors.New("store: database schema is newer than this crewbo")
)

// DB is an open, migrated Crewbo database.
type DB struct {
	sql *sql.DB
}

// Open opens or creates the database at path, checks its integrity, and
// applies pending migrations. Pragmas are passed in the DSN so they apply to
// every connection (modernc.org/sqlite: "_pragma" query parameters).
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection: the daemon's owner goroutine is the only writer, and a
	// single connection rules out SQLITE_BUSY between our own connections.
	sqldb.SetMaxOpenConns(1)
	db := &DB{sql: sqldb}
	if err := db.check(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.migrate(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// SQL returns the underlying handle for packages that own their queries.
func (db *DB) SQL() *sql.DB { return db.sql }

// Close closes the database.
func (db *DB) Close() error { return db.sql.Close() }

// SchemaVersion returns the number of applied migrations.
func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// check runs SQLite's quick_check; any failure to read the file as a
// database is reported as ErrCorrupt.
func (db *DB) check(ctx context.Context) error {
	var result string
	if err := db.sql.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		if isCorruption(err) {
			return fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		return err
	}
	if result != "ok" {
		return fmt.Errorf("%w: quick_check: %s", ErrCorrupt, result)
	}
	return nil
}

// isCorruption reports SQLITE_NOTADB and SQLITE_CORRUPT, including extended
// codes, whose low byte is the primary code (https://www.sqlite.org/rescode.html).
func isCorruption(err error) bool {
	var serr *sqlite.Error
	if !errors.As(err, &serr) {
		return false
	}
	switch serr.Code() & 0xff {
	case sqlite3.SQLITE_NOTADB, sqlite3.SQLITE_CORRUPT:
		return true
	}
	return false
}
