package db

import (
	"context"
	"reflect"
	"testing"
)

func TestIngestCheckpointCommitsAndRollsBackContextWithCursor(t *testing.T) {
	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIngestCheckpoint(ctx, tx, "Player.log", 100, 2, "signature", "context-1", "global-1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetIngestState(ctx, "Player.log")
	if err != nil {
		t.Fatal(err)
	}
	tx, err = store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIngestCheckpoint(ctx, tx, "Player.log", 200, 4, "signature-2", "context-2", "global-2"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetIngestState(ctx, "Player.log")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || before.ContextJSON != "context-1" {
		t.Fatalf("cursor/context changed after rollback: %+v -> %+v", before, after)
	}
	global, err := store.ParserCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if global != "global-1" {
		t.Fatalf("global context after rollback = %q", global)
	}
	cursor, err := store.GetIngestCursor(ctx, "Player.log")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.ContextJSON != "" || cursor.Offset != 100 {
		t.Fatalf("metadata-only cursor = %+v", cursor)
	}
}

func TestMigrateIngestStateAddsContextWithoutChangingCursor(t *testing.T) {
	ctx := context.Background()
	database := openTempSQLiteDB(t)
	mustExec(t, database, `CREATE TABLE ingest_state(log_path TEXT PRIMARY KEY,byte_offset INTEGER NOT NULL,line_no INTEGER NOT NULL,updated_at TEXT NOT NULL)`)
	mustExec(t, database, `INSERT INTO ingest_state VALUES('Player.log',100,2,'now')`)
	for range 2 {
		if err := migrateIngestState(ctx, database); err != nil {
			t.Fatal(err)
		}
	}
	state, err := NewStore(database).GetIngestState(ctx, "Player.log")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Found || state.Offset != 100 || state.LineNo != 2 || state.FileSignature != "" || state.ContextJSON != "" {
		t.Fatalf("migrated checkpoint = %+v", state)
	}
}
