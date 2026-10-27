package db

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/solean/ponder/internal/model"
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

func liveCountsTestObject(instanceID, cardID, seatID int64, zoneType string) model.MatchReplayFrameObjectRow {
	return model.MatchReplayFrameObjectRow{
		InstanceID:       instanceID,
		CardID:           cardID,
		OwnerSeatID:      int64Ptr(seatID),
		ControllerSeatID: int64Ptr(seatID),
		ZoneType:         zoneType,
		Visibility:       "public",
	}
}

func TestGetMatchDetailCountsLiveOpponentCopiesFromFrames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	const (
		recast   = int64(7001)
		pair     = int64(7002)
		stale    = int64(7003)
		instant  = int64(7004)
		arenaID  = "match-live-opponent"
		oppSeat  = int64(2)
		selfSeat = int64(1)
	)
	tx := mustBeginTx(t, store)
	if _, err := store.UpsertMatchStart(ctx, tx, arenaID, "Traditional_Ladder", selfSeat, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	frames := [][]model.MatchReplayFrameObjectRow{
		{
			liveCountsTestObject(10, recast, oppSeat, "battlefield"),
			liveCountsTestObject(30, stale, oppSeat, "battlefield"),
			liveCountsTestObject(40, instant, oppSeat, "stack"),
		},
		// The recast card dies and comes back: every move leaves a new
		// instance id and a stale one in Limbo, but it is still one card.
		{
			liveCountsTestObject(10, recast, oppSeat, "limbo"),
			liveCountsTestObject(11, recast, oppSeat, "graveyard"),
			liveCountsTestObject(20, pair, oppSeat, "battlefield"),
			liveCountsTestObject(21, pair, oppSeat, "hand"),
			liveCountsTestObject(30, stale, oppSeat, "battlefield"),
			liveCountsTestObject(31, stale, oppSeat, "battlefield"),
			liveCountsTestObject(40, instant, oppSeat, "limbo"),
			liveCountsTestObject(41, instant, oppSeat, "graveyard"),
		},
		{
			liveCountsTestObject(11, recast, oppSeat, "limbo"),
			liveCountsTestObject(12, recast, oppSeat, "battlefield"),
		},
	}
	for i, objects := range frames {
		insertReplayTestFrame(t, store, tx, arenaID, 1, int64(i+1), 1, `{"1":20,"2":20}`, objects)
		for _, object := range objects {
			if err := store.UpsertMatchOpponentCardInstance(ctx, tx, arenaID, 1, object.InstanceID, object.CardID, "2026-03-12T19:07:00Z", "gre_public_replay"); err != nil {
				t.Fatalf("UpsertMatchOpponentCardInstance: %v", err)
			}
		}
	}
	mustCommit(t, tx)
	matchID := matchRowID(t, store, arenaID)
	// Analytics derived earlier in the match are stale and lower than what
	// the frames now show.
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO match_opponent_card_counts (match_id, card_id, max_copies, derived_at)
		VALUES (?, ?, 1, '2026-03-12T19:06:59Z')
	`, matchID, stale); err != nil {
		t.Fatalf("insert stale opponent count: %v", err)
	}

	detail, err := store.GetMatchDetail(ctx, matchID)
	if err != nil {
		t.Fatalf("GetMatchDetail: %v", err)
	}
	got := map[int64]int64{}
	for _, card := range detail.OpponentObservedCards {
		got[card.CardID] = card.Quantity
	}
	want := map[int64]int64{recast: 1, pair: 2, stale: 2, instant: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opponent observed quantities = %v, want %v", got, want)
	}
	batched, err := store.ListMatchOpponentCardQuantities(ctx, []int64{matchID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(batched[matchID], want) {
		t.Fatalf("batched live quantities = %v, want %v", batched[matchID], want)
	}
}

func TestGetLiveKnownSelfCardCountsSkipsLimbo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	const arenaID = "match-live-self"
	tx := mustBeginTx(t, store)
	if _, err := store.UpsertMatchStart(ctx, tx, arenaID, "Traditional_Ladder", 1, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	insertReplayTestFrame(t, store, tx, arenaID, 1, 1, 1, `{"1":20,"2":20}`, []model.MatchReplayFrameObjectRow{
		// A resolved spell: its stack instance lingers in Limbo next to the
		// graveyard instance that replaced it.
		liveCountsTestObject(50, 8001, 1, "limbo"),
		liveCountsTestObject(51, 8001, 1, "graveyard"),
		liveCountsTestObject(52, 8002, 1, "hand"),
		liveCountsTestObject(53, 8003, 1, "library"),
	})
	mustCommit(t, tx)

	counts, available, err := store.GetLiveKnownSelfCardCounts(ctx, matchRowID(t, store, arenaID), 1)
	if err != nil {
		t.Fatalf("GetLiveKnownSelfCardCounts: %v", err)
	}
	if !available {
		t.Fatalf("GetLiveKnownSelfCardCounts: expected state available")
	}
	want := map[int64]int64{8001: 1, 8002: 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("known self counts = %v, want %v", counts, want)
	}
}
