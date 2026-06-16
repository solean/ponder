package db

import (
	"context"
	"testing"
)

func TestGetLiveMatchID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}

	store := NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	// A completed match should never count as live.
	if _, err := store.UpsertMatchStart(ctx, tx, "match-done", "Traditional_Ladder", 1, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-done): %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-done", 1, 1, 9, 420, "Game", "2026-03-12T19:13:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd(match-done): %v", err)
	}

	// An in-progress match (no result/ended_at) is the live one.
	liveID, err := store.UpsertMatchStart(ctx, tx, "match-live", "Traditional_Ladder", 1, "2026-03-12T20:06:52Z")
	if err != nil {
		t.Fatalf("UpsertMatchStart(match-live): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	gotID, ok, err := store.GetLiveMatchID(ctx)
	if err != nil {
		t.Fatalf("GetLiveMatchID: %v", err)
	}
	if !ok {
		t.Fatalf("GetLiveMatchID: expected a live match, got none")
	}
	if gotID != liveID {
		t.Fatalf("GetLiveMatchID = %d, want %d (match-live)", gotID, liveID)
	}

	// Once the live match completes, there should be no live match.
	tx2, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx2, "match-live", 1, 1, 5, 300, "Game", "2026-03-12T20:11:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd(match-live): %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, ok, err := store.GetLiveMatchID(ctx); err != nil {
		t.Fatalf("GetLiveMatchID (after end): %v", err)
	} else if ok {
		t.Fatalf("GetLiveMatchID: expected no live match after completion")
	}
}

func TestGetLiveMatchIDExcludesStale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}

	store := NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "match-abandoned", "Traditional_Ladder", 1, "2026-01-01T10:00:00Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-abandoned): %v", err)
	}
	// Backdate updated_at so the recency window excludes this abandoned game.
	if _, err := tx.ExecContext(ctx, `UPDATE matches SET updated_at = datetime('now', '-2 days') WHERE arena_match_id = ?`, "match-abandoned"); err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, ok, err := store.GetLiveMatchID(ctx); err != nil {
		t.Fatalf("GetLiveMatchID: %v", err)
	} else if ok {
		t.Fatalf("GetLiveMatchID: expected stale in-progress match to be excluded")
	}
}
