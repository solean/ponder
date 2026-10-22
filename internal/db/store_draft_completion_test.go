package db

import (
	"context"
	"testing"
)

func TestCompleteUnnamedDraftAttachesExistingEventRun(t *testing.T) {
	t.Parallel()
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
	defer tx.Rollback()
	const eventName = "PremierDraft_HOB_20260811"
	const startedAt = "2026-08-12T12:00:00Z"
	if err := store.UpsertEventRunJoin(ctx, tx, eventName, "Gold", 10000, startedAt); err != nil {
		t.Fatal(err)
	}
	sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString("human-draft"), false, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteDraftSession(ctx, tx, eventName, nil, false, "2026-08-12T12:30:00Z"); err != nil {
		t.Fatal(err)
	}
	var linkedSessionID, paid, runs int64
	if err := tx.QueryRowContext(ctx, `SELECT draft_session_id, entry_currency_paid FROM event_runs WHERE event_name = ?`, eventName).Scan(&linkedSessionID, &paid); err != nil {
		t.Fatal(err)
	}
	if linkedSessionID != sessionID || paid != 10000 {
		t.Fatalf("run linked session/entry = %d/%d, want %d/10000", linkedSessionID, paid, sessionID)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_runs WHERE event_name = ?`, eventName).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("event runs = %d, want existing run reused", runs)
	}
}
