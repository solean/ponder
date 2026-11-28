package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func openTempSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	// One connection catches store code that holds two result sets open at
	// once. Foreign keys start off, as they are while Init migrates; Init
	// turns them back on.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign_keys: %v", err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()

	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec %s: %v", fmt.Sprintf("%.80s", stmt), err)
	}
}
