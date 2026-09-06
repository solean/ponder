package db

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/solean/ponder/internal/model"
)

func newReplayArchiveTestStore(t *testing.T) *Store {
	t.Helper()
	database := openTempSQLiteDB(t)
	if err := Init(context.Background(), database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return NewStore(database)
}

func mustBeginTx(t *testing.T, store *Store) *sql.Tx {
	t.Helper()
	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	return tx
}

func mustCommit(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func insertReplayTestFrame(t *testing.T, store *Store, tx *sql.Tx, arenaMatchID string, gameNumber, gameStateID, turnNumber int64, lifeTotalsJSON string, objects []model.MatchReplayFrameObjectRow) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.ReplaceMatchReplayFrame(
		ctx, tx, arenaMatchID,
		gameNumber, gameStateID, gameStateID-1, turnNumber,
		"full", "playing", "main1", "", "", "2026-03-12T19:07:00Z", "gre",
		[]byte(lifeTotalsJSON),
		[]byte(`[{"actionType":"play"}]`),
		[]byte(`{"annotations":[]}`),
		objects,
	); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(game=%d state=%d): %v", gameNumber, gameStateID, err)
	}
}

func replayTestObjects(turn int64) []model.MatchReplayFrameObjectRow {
	objects := []model.MatchReplayFrameObjectRow{
		{
			InstanceID:       101,
			CardID:           5001,
			OwnerSeatID:      int64Ptr(1),
			ControllerSeatID: int64Ptr(1),
			ZoneID:           int64Ptr(28),
			ZoneType:         "battlefield",
			ZonePosition:     int64Ptr(1),
			Visibility:       "public",
			Power:            int64Ptr(2),
			Toughness:        int64Ptr(3),
			IsTapped:         turn%2 == 0,
		},
		{
			InstanceID:       202,
			CardID:           5002,
			OwnerSeatID:      int64Ptr(2),
			ControllerSeatID: int64Ptr(2),
			ZoneID:           int64Ptr(28),
			ZoneType:         "battlefield",
			ZonePosition:     int64Ptr(2),
			Visibility:       "public",
			IsToken:          true,
		},
		{
			InstanceID:       404,
			OwnerSeatID:      int64Ptr(2),
			ControllerSeatID: int64Ptr(2),
			ZoneID:           int64Ptr(35),
			ZoneType:         "hand",
			ZonePosition:     int64Ptr(1),
			Visibility:       "private",
		},
	}
	if turn >= 2 {
		objects = append(objects, model.MatchReplayFrameObjectRow{
			InstanceID:       303,
			CardID:           5003,
			OwnerSeatID:      int64Ptr(1),
			ControllerSeatID: int64Ptr(1),
			ZoneID:           int64Ptr(31),
			ZoneType:         "graveyard",
			ZonePosition:     int64Ptr(1),
			Visibility:       "public",
		})
	}
	return objects
}

func setupReplayTestMatch(t *testing.T, store *Store, arenaMatchID string, startedAt string) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertCardNames(ctx, map[int64]string{
		5001: "Test Creature",
		5002: "Test Token",
		5003: "Test Sorcery",
	}); err != nil {
		t.Fatalf("UpsertCardNames: %v", err)
	}
	tx := mustBeginTx(t, store)
	if _, err := store.UpsertMatchStart(ctx, tx, arenaMatchID, "Traditional_Ladder", 1, startedAt); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	for turn := int64(1); turn <= 3; turn++ {
		insertReplayTestFrame(t, store, tx, arenaMatchID, 1, turn*10, turn, `{"1":20,"2":19}`, replayTestObjects(turn))
	}
	insertReplayTestFrame(t, store, tx, arenaMatchID, 2, 5, 1, `{"1":20,"2":20}`, replayTestObjects(1))
	mustCommit(t, tx)
}

func matchRowID(t *testing.T, store *Store, arenaMatchID string) int64 {
	t.Helper()
	var id int64
	if err := store.db.QueryRowContext(context.Background(), `SELECT id FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&id); err != nil {
		t.Fatalf("lookup match id: %v", err)
	}
	return id
}

func countRows(t *testing.T, store *Store, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := store.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

func archiveTestMatch(t *testing.T, store *Store, arenaMatchID string) {
	t.Helper()
	tx := mustBeginTx(t, store)
	archived, err := store.ArchiveMatchReplay(context.Background(), tx, arenaMatchID)
	if err != nil {
		t.Fatalf("ArchiveMatchReplay: %v", err)
	}
	if !archived {
		t.Fatalf("ArchiveMatchReplay archived nothing")
	}
	mustCommit(t, tx)
}

func TestArchiveMatchReplayRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	setupReplayTestMatch(t, store, "match-archive", "2026-03-12T19:06:52Z")
	matchID := matchRowID(t, store, "match-archive")

	before, err := store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchReplayFrames before archive: %v", err)
	}
	if len(before) != 4 {
		t.Fatalf("len(before) = %d, want 4", len(before))
	}
	for _, frame := range before {
		hiddenCards := 0
		for _, object := range frame.Objects {
			if object.ZoneType == "hand" && object.PlayerSide == "opponent" {
				hiddenCards++
				if object.CardID != 0 || object.CardName != "" || object.DetailsJSON != "" {
					t.Fatalf("opponent hand exposes card identity: %#v", object)
				}
			}
		}
		if hiddenCards != 1 {
			t.Fatalf("opponent hand count = %d, want 1 before archiving", hiddenCards)
		}
	}

	archiveTestMatch(t, store, "match-archive")

	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_frames WHERE match_id = ?`, matchID); n != 0 {
		t.Fatalf("match_replay_frames rows after archive = %d, want 0", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_frame_objects o JOIN match_replay_frames f ON f.id = o.frame_id WHERE f.match_id = ?`, matchID); n != 0 {
		t.Fatalf("match_replay_frame_objects rows after archive = %d, want 0", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_archives WHERE match_id = ?`, matchID); n != 1 {
		t.Fatalf("match_replay_archives rows = %d, want 1", n)
	}

	after, err := store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchReplayFrames after archive: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("replay frames changed after archive:\nbefore: %+v\nafter: %+v", before, after)
	}
}

func TestArchiveMatchReplayMergesLiveRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	setupReplayTestMatch(t, store, "match-live-merge", "2026-03-12T19:06:52Z")
	matchID := matchRowID(t, store, "match-live-merge")

	archiveTestMatch(t, store, "match-live-merge")

	// New frames arriving after archiving (e.g. re-parse or late game states)
	// must show up merged with the archived frames.
	tx := mustBeginTx(t, store)
	insertReplayTestFrame(t, store, tx, "match-live-merge", 2, 6, 2, `{"1":20,"2":15}`, replayTestObjects(2))
	mustCommit(t, tx)

	frames, err := store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchReplayFrames: %v", err)
	}
	if len(frames) != 5 {
		t.Fatalf("len(frames) = %d, want 5 (4 archived + 1 live)", len(frames))
	}
	last := frames[len(frames)-1]
	if last.GameNumber == nil || *last.GameNumber != 2 || last.GameStateID == nil || *last.GameStateID != 6 {
		t.Fatalf("last frame = %+v, want game 2 state 6", last)
	}
	if len(last.Changes) == 0 {
		t.Fatalf("expected changes computed across archive/live boundary")
	}

	// Re-archiving must fold the live row into the existing archive without
	// losing the previously archived frames.
	archiveTestMatch(t, store, "match-live-merge")
	merged, err := store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchReplayFrames after re-archive: %v", err)
	}
	if !reflect.DeepEqual(frames, merged) {
		t.Fatalf("frames changed after re-archive:\nbefore: %+v\nafter: %+v", frames, merged)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_frames WHERE match_id = ?`, matchID); n != 0 {
		t.Fatalf("live rows after re-archive = %d, want 0", n)
	}
}

func TestCompactMatchReplaysOnlyTouchesFinishedMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)

	setupReplayTestMatch(t, store, "match-finished", "2026-03-12T19:06:52Z")
	tx := mustBeginTx(t, store)
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-finished", 1, 1, 9, 420, "Concede", "2026-03-12T19:13:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd: %v", err)
	}
	mustCommit(t, tx)

	// A match that started recently and has no result is considered live.
	liveStartedAt := time.Now().UTC().Format(time.RFC3339)
	setupReplayTestMatch(t, store, "match-live", liveStartedAt)

	archived, err := store.CompactMatchReplays(ctx)
	if err != nil {
		t.Fatalf("CompactMatchReplays: %v", err)
	}
	if archived != 1 {
		t.Fatalf("archived = %d, want 1", archived)
	}

	finishedID := matchRowID(t, store, "match-finished")
	liveID := matchRowID(t, store, "match-live")
	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_archives WHERE match_id = ?`, finishedID); n != 1 {
		t.Fatalf("finished match archives = %d, want 1", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_archives WHERE match_id = ?`, liveID); n != 0 {
		t.Fatalf("live match archives = %d, want 0", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM match_replay_frames WHERE match_id = ?`, liveID); n == 0 {
		t.Fatalf("live match frames were removed by compaction")
	}

	// Second pass is a no-op.
	archived, err = store.CompactMatchReplays(ctx)
	if err != nil {
		t.Fatalf("CompactMatchReplays second pass: %v", err)
	}
	if archived != 0 {
		t.Fatalf("second pass archived = %d, want 0", archived)
	}
}

func TestMatchReplayStatusDetectsEmptyFrameReplacement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	const arenaID = "match-status-replacement"
	tx := mustBeginTx(t, store)
	if _, err := store.UpsertMatchStart(ctx, tx, arenaID, "Ladder", 1, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	insertReplayTestFrame(t, store, tx, arenaID, 1, 10, 1, `{"1":20,"2":20}`, nil)
	mustCommit(t, tx)
	matchID := matchRowID(t, store, arenaID)
	// Establish an older snapshot without depending on clock resolution.
	if _, err := store.db.ExecContext(ctx, `UPDATE match_replay_frames SET created_at = ? WHERE match_id = ?`, "2026-01-01T00:00:00Z", matchID); err != nil {
		t.Fatalf("age replay snapshot: %v", err)
	}
	before, err := store.GetMatchReplayStatus(ctx, matchID)
	if err != nil {
		t.Fatalf("GetMatchReplayStatus before replacement: %v", err)
	}
	framesBefore, err := store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchReplayFrames before replacement: %v", err)
	}

	tx = mustBeginTx(t, store)
	insertReplayTestFrame(t, store, tx, arenaID, 1, 10, 1, `{"1":15,"2":20}`, nil)
	mustCommit(t, tx)
	after, err := store.GetMatchReplayStatus(ctx, matchID)
	if err != nil {
		t.Fatalf("GetMatchReplayStatus after replacement: %v", err)
	}
	if after.Revision == before.Revision {
		t.Fatal("replacing an empty-object snapshot did not change its revision")
	}
	framesAfter, err := store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchReplayFrames after replacement: %v", err)
	}
	if len(framesBefore) != 1 || len(framesAfter) != 1 || framesBefore[0].ID != framesAfter[0].ID {
		t.Fatalf("replacement changed frame identity: before=%+v after=%+v", framesBefore, framesAfter)
	}
	if framesAfter[0].SelfLifeTotal == nil || *framesAfter[0].SelfLifeTotal != 15 || len(framesAfter[0].Objects) != 0 {
		t.Fatalf("replacement snapshot = %+v, want 15 life and no objects", framesAfter[0])
	}
}

func TestMatchReplayStatusTracksArchiveAndLateRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	const arenaID = "match-status-archive"
	setupReplayTestMatch(t, store, arenaID, "2026-03-12T19:06:52Z")
	matchID := matchRowID(t, store, arenaID)
	readStatus := func() model.MatchReplayStatus {
		t.Helper()
		status, err := store.GetMatchReplayStatus(ctx, matchID)
		if err != nil {
			t.Fatalf("GetMatchReplayStatus: %v", err)
		}
		return status
	}
	before := readStatus()
	if before.Complete {
		t.Fatal("unfinished match reported complete")
	}
	if repeated := readStatus(); repeated != before {
		t.Fatalf("unchanged status was not stable: before=%+v repeated=%+v", before, repeated)
	}
	requireChanged := func(stage string) {
		t.Helper()
		after := readStatus()
		if after.Revision == before.Revision {
			t.Fatalf("%s did not change replay revision", stage)
		}
		before = after
	}
	tx := mustBeginTx(t, store)
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, arenaID, 1, 1, 9, 420, "Concede", "2026-03-12T19:13:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd: %v", err)
	}
	mustCommit(t, tx)
	requireChanged("completion")
	if !before.Complete {
		t.Fatal("ended match reported incomplete")
	}

	archiveTestMatch(t, store, arenaID)
	requireChanged("archiving")
	tx = mustBeginTx(t, store)
	insertReplayTestFrame(t, store, tx, arenaID, 2, 6, 2, `{"1":20,"2":15}`, nil)
	mustCommit(t, tx)
	requireChanged("late frame")

	// An older late row must be observable even when MAX(created_at) is unchanged.
	tx = mustBeginTx(t, store)
	insertReplayTestFrame(t, store, tx, arenaID, 2, 7, 3, `{"1":20,"2":10}`, nil)
	if _, err := tx.ExecContext(ctx, `UPDATE match_replay_frames SET created_at = ? WHERE match_id = ? AND game_state_id = 7`, "2026-01-01T00:00:00Z", matchID); err != nil {
		t.Fatalf("age late row: %v", err)
	}
	mustCommit(t, tx)
	requireChanged("older late frame")
	if _, err := store.db.ExecContext(ctx, `DELETE FROM match_replay_frames WHERE match_id = ? AND game_state_id = 7`, matchID); err != nil {
		t.Fatalf("delete older late row: %v", err)
	}
	requireChanged("older row deletion")
	archiveTestMatch(t, store, arenaID)
	requireChanged("re-archiving")

	// Status must remain available without decoding even a damaged archive,
	// and an archive-only correction must invalidate an otherwise idle match.
	if _, err := store.db.ExecContext(ctx, `UPDATE match_replay_archives SET payload_zstd = ?, updated_at = ? WHERE match_id = ?`, []byte("invalid archive"), "2026-02-01T00:00:00Z", matchID); err != nil {
		t.Fatalf("replace archive: %v", err)
	}
	requireChanged("archive-only correction")
	if repeated := readStatus(); repeated != before {
		t.Fatalf("archive status was not stable: before=%+v repeated=%+v", before, repeated)
	}
}
