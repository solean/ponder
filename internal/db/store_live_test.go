package db

import (
	"context"
	"testing"
	"time"
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

// An abandoned match whose updated_at is on the same UTC day but older than the
// recency window must not count as live. Comparing against SQLite's
// datetime('now', ...) (a space-separated string) instead of a stored-format
// timestamp made every same-day row pass, because 'T' sorts after ' '.
func TestGetLiveMatchIDExcludesStaleWithinSameDay(t *testing.T) {
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
	if _, err := store.UpsertMatchStart(ctx, tx, "match-abandoned", "Traditional_Ladder", 1, utcTimestamp(time.Now().Add(-7*time.Hour))); err != nil {
		t.Fatalf("UpsertMatchStart(match-abandoned): %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE matches SET updated_at = ? WHERE arena_match_id = ?`,
		utcTimestamp(time.Now().Add(-7*time.Hour)), "match-abandoned",
	); err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, ok, err := store.GetLiveMatchID(ctx); err != nil {
		t.Fatalf("GetLiveMatchID: %v", err)
	} else if ok {
		t.Fatalf("GetLiveMatchID: expected a match untouched for 7 hours to be excluded")
	}
}

func TestGetLiveMatchIDActiveSince(t *testing.T) {
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
	// Arena was quit 30 minutes into this match: the row stays in progress and
	// recent enough for GetLiveMatchID, but nothing has happened since.
	abandonedID, err := store.UpsertMatchStart(ctx, tx, "match-abandoned", "Traditional_Ladder", 1, utcTimestamp(time.Now().Add(-30*time.Minute)))
	if err != nil {
		t.Fatalf("UpsertMatchStart(match-abandoned): %v", err)
	}
	if _, err := store.ReplaceMatchReplayFrame(
		ctx, tx, "match-abandoned",
		1, 10, 0, 3,
		"GameStateType_Diff", "GameStage_Start", "Phase_Main1", "", "",
		utcTimestamp(time.Now().Add(-29*time.Minute)), "gre_full",
		nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(match-abandoned): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	notBefore := time.Now().Add(-10 * time.Minute)
	if id, ok, err := store.GetLiveMatchIDActiveSince(ctx, notBefore); err != nil {
		t.Fatalf("GetLiveMatchIDActiveSince: %v", err)
	} else if ok {
		t.Fatalf("GetLiveMatchIDActiveSince = %d, want no match for an abandoned game", id)
	}
	if _, ok, err := store.GetLiveMatchID(ctx); err != nil {
		t.Fatalf("GetLiveMatchID: %v", err)
	} else if !ok {
		t.Fatalf("GetLiveMatchID: expected the abandoned match to remain within the recency window")
	}

	// A fresh frame on the same match makes it active again (Arena reconnected
	// into the game, or the player simply resumed playing).
	tx2, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.ReplaceMatchReplayFrame(
		ctx, tx2, "match-abandoned",
		1, 11, 10, 4,
		"GameStateType_Diff", "GameStage_Start", "Phase_Main1", "", "",
		utcTimestamp(time.Now().Add(-time.Minute)), "gre_full",
		nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(resumed): %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if id, ok, err := store.GetLiveMatchIDActiveSince(ctx, notBefore); err != nil {
		t.Fatalf("GetLiveMatchIDActiveSince (resumed): %v", err)
	} else if !ok || id != abandonedID {
		t.Fatalf("GetLiveMatchIDActiveSince (resumed) = %d, %v, want %d, true", id, ok, abandonedID)
	}
}

// A match that just started has no replay frames yet (mulligans and sideboard
// submissions precede the first game state), so its start time is the activity.
func TestGetLiveMatchIDActiveSinceUsesStartBeforeFirstFrame(t *testing.T) {
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
	startedID, err := store.UpsertMatchStart(ctx, tx, "match-starting", "Traditional_Ladder", 1, utcTimestamp(time.Now().Add(-30*time.Second)))
	if err != nil {
		t.Fatalf("UpsertMatchStart(match-starting): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if id, ok, err := store.GetLiveMatchIDActiveSince(ctx, time.Now().Add(-10*time.Minute)); err != nil {
		t.Fatalf("GetLiveMatchIDActiveSince: %v", err)
	} else if !ok || id != startedID {
		t.Fatalf("GetLiveMatchIDActiveSince = %d, %v, want %d, true", id, ok, startedID)
	}
}
