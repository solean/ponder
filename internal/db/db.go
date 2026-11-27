package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Pragmas are connection-scoped in SQLite, so they must ride on the DSN to
// apply to every pooled connection — foreign_keys in particular guards the
// ON DELETE CASCADE cleanup that keeps the database free of orphan rows.
// _txlock=immediate makes concurrent write transactions queue on the busy
// handler instead of failing with SQLITE_BUSY on lock upgrade.
const dsnOptions = "_txlock=immediate" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)"

func dsn(path string) string {
	// url.URL renders a relative path as file://<first-segment>/..., which
	// SQLite reads as an authority, so the path must be absolute.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: dsnOptions}
	return u.String()
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// WAL allows readers to run alongside the single writer; a small pool
	// keeps API reads from queueing behind ingest write batches.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

// dbConn abstracts *sql.DB and *sql.Conn so helpers can run on the pool or on
// a dedicated connection whose pragmas differ from the pool's.
type dbConn interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Init brings the database up to SchemaVersion, backing it up first when a
// migration has to change it, then runs the repairs that every launch needs.
func Init(ctx context.Context, db *sql.DB) error {
	if err := migrate(ctx, db); err != nil {
		return err
	}
	if err := NewStore(db).RepairEventRunInstances(ctx); err != nil {
		return err
	}
	// Last, so rows the repair above recreated are assigned too.
	return assignSingleArenaAccount(ctx, db)
}
