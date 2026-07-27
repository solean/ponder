package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestReplaceMatchGameDeckSnapshotReplacesExactCounts(t *testing.T) {
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
	if _, err := store.UpsertMatchStart(ctx, tx, "snapshot-match", "Traditional_Ladder", 1, "2026-07-26T08:00:00Z"); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "snapshot-match", 2,
		"2026-07-26T01:00:00-07:00", "initial", []int64{101, 101, 102}, []int64{201, 201}); err != nil {
		t.Fatalf("ReplaceMatchGameDeckSnapshot(initial): %v", err)
	}

	mainCards := []int64{101, 103, 103, 0, -1}
	sideboardCards := []int64{102, 202, 202}
	for call := 0; call < 2; call++ {
		if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "snapshot-match", 2,
			"2026-07-26T02:00:00-07:00", " gre_connect ", mainCards, sideboardCards); err != nil {
			t.Fatalf("ReplaceMatchGameDeckSnapshot(replacement %d): %v", call+1, err)
		}
	}
	if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "snapshot-match", 2,
		"2026-07-26T03:00:00-07:00", "malformed", []int64{0, -1}, []int64{999}); err != nil {
		t.Fatalf("ReplaceMatchGameDeckSnapshot(malformed): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var (
		snapshotID    int64
		snapshotCount int64
		observedAt    string
		source        string
	)
	if err := database.QueryRowContext(ctx, `
		SELECT MIN(id), COUNT(*), COALESCE(MIN(observed_at), ''), COALESCE(MIN(source), '')
		FROM match_game_deck_snapshots
	`).Scan(&snapshotID, &snapshotCount, &observedAt, &source); err != nil {
		t.Fatalf("read game deck snapshot: %v", err)
	}
	if snapshotCount != 1 {
		t.Fatalf("snapshot count = %d, want 1", snapshotCount)
	}
	if observedAt != "2026-07-26T09:00:00Z" {
		t.Fatalf("observed_at = %q, want normalized UTC timestamp", observedAt)
	}
	if source != "gre_connect" {
		t.Fatalf("source = %q, want gre_connect", source)
	}

	type cardKey struct {
		section string
		cardID  int64
	}
	got := make(map[cardKey]int64)
	rows, err := database.QueryContext(ctx, `
		SELECT section, card_id, quantity
		FROM match_game_deck_snapshot_cards
		WHERE snapshot_id = ?
	`, snapshotID)
	if err != nil {
		t.Fatalf("list snapshot cards: %v", err)
	}
	for rows.Next() {
		var key cardKey
		var quantity int64
		if err := rows.Scan(&key.section, &key.cardID, &quantity); err != nil {
			rows.Close()
			t.Fatalf("scan snapshot card: %v", err)
		}
		got[key] = quantity
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate snapshot cards: %v", err)
	}
	rows.Close()

	want := map[cardKey]int64{
		{section: gameDeckSectionMain, cardID: 101}:      1,
		{section: gameDeckSectionMain, cardID: 103}:      2,
		{section: gameDeckSectionSideboard, cardID: 102}: 1,
		{section: gameDeckSectionSideboard, cardID: 202}: 2,
	}
	if len(got) != len(want) {
		t.Fatalf("snapshot cards = %#v, want %#v", got, want)
	}
	for key, quantity := range want {
		if got[key] != quantity {
			t.Fatalf("snapshot card %#v quantity = %d, want %d", key, got[key], quantity)
		}
	}
}

func TestListMatchGamesDiffsAdjacentCapturedSnapshots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)
	if err := store.UpsertCardNames(ctx, map[int64]string{
		101: "Alpha",
		102: "Beta",
		103: "Stable",
		201: "Gamma",
		202: "Delta",
		999: "Later Edit",
	}); err != nil {
		t.Fatalf("UpsertCardNames: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	startingCards := []DeckCard{
		{Section: gameDeckSectionMain, CardID: 101, Quantity: 2},
		{Section: gameDeckSectionMain, CardID: 102, Quantity: 1},
		{Section: gameDeckSectionMain, CardID: 103, Quantity: 1},
		{Section: gameDeckSectionSideboard, CardID: 201, Quantity: 2},
		{Section: gameDeckSectionSideboard, CardID: 202, Quantity: 1},
	}
	if _, err := store.UpsertDeck(ctx, tx, "sideboard-deck", "Traditional_Ladder", "Sideboard Deck", "Standard",
		"test", "2026-07-26T08:00:00Z", startingCards); err != nil {
		t.Fatalf("UpsertDeck(starting): %v", err)
	}
	matchID, err := store.UpsertMatchStart(ctx, tx, "sideboard-match", "Traditional_Ladder", 1, "2026-07-26T08:30:00Z")
	if err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	linked, err := store.LinkMatchToDeckByArenaDeckID(ctx, tx, "sideboard-match", "sideboard-deck", "event_deck")
	if err != nil || !linked {
		t.Fatalf("LinkMatchToDeckByArenaDeckID = %v, %v", linked, err)
	}
	insertSideboardTestGames(t, ctx, tx, matchID, 3)

	gameOneMain := []int64{101, 101, 102, 103}
	gameOneSideboard := []int64{201, 201, 202}
	gameTwoMain := []int64{101, 103, 201, 201}
	gameTwoSideboard := []int64{101, 102, 202}
	if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "sideboard-match", 1,
		"2026-07-26T08:30:00Z", "gre_connect", gameOneMain, gameOneSideboard); err != nil {
		t.Fatalf("ReplaceMatchGameDeckSnapshot(game 1): %v", err)
	}
	if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "sideboard-match", 2,
		"2026-07-26T09:00:00Z", "gre_connect", gameTwoMain, gameTwoSideboard); err != nil {
		t.Fatalf("ReplaceMatchGameDeckSnapshot(game 2): %v", err)
	}
	if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "sideboard-match", 3,
		"2026-07-26T09:30:00Z", "gre_connect", gameTwoMain, gameTwoSideboard); err != nil {
		t.Fatalf("ReplaceMatchGameDeckSnapshot(game 3): %v", err)
	}

	// Editing the linked deck after the match must not change the captured G1
	// baseline used for the sideboard diff.
	if _, err := store.UpsertDeck(ctx, tx, "sideboard-deck", "Traditional_Ladder", "Sideboard Deck", "Standard",
		"test", "2026-07-26T10:00:00Z", []DeckCard{{Section: gameDeckSectionMain, CardID: 999, Quantity: 4}}); err != nil {
		t.Fatalf("UpsertDeck(later edit): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	games, err := store.ListMatchGames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchGames: %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("games = %d, want 3", len(games))
	}
	if games[0].SideboardChanges != nil {
		t.Fatalf("game 1 sideboard changes = %#v, want nil", games[0].SideboardChanges)
	}

	gameTwoChanges := games[1].SideboardChanges
	if gameTwoChanges == nil {
		t.Fatal("game 2 sideboard changes are nil, want captured diff")
	}
	assertSideboardCards(t, gameTwoChanges.CardsIn, []sideboardCardExpectation{
		{cardID: 201, quantity: 2, cardName: "Gamma"},
	})
	assertSideboardCards(t, gameTwoChanges.CardsOut, []sideboardCardExpectation{
		{cardID: 101, quantity: 1, cardName: "Alpha"},
		{cardID: 102, quantity: 1, cardName: "Beta"},
	})
	if gameTwoChanges.ObservedAt != "2026-07-26T09:00:00Z" || gameTwoChanges.Source != "gre_connect" {
		t.Fatalf("game 2 metadata = %#v, want captured observation metadata", gameTwoChanges)
	}

	gameThreeChanges := games[2].SideboardChanges
	if gameThreeChanges == nil {
		t.Fatal("game 3 sideboard changes are nil, want captured no-change result")
	}
	if gameThreeChanges.CardsIn == nil || gameThreeChanges.CardsOut == nil {
		t.Fatalf("game 3 empty changes = %#v, want non-nil empty lists", gameThreeChanges)
	}
	if len(gameThreeChanges.CardsIn) != 0 || len(gameThreeChanges.CardsOut) != 0 {
		t.Fatalf("game 3 changes = %#v, want no changes", gameThreeChanges)
	}
}

func TestListMatchGamesDoesNotInferBaselineFromMutableCurrentDeck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)
	if err := store.UpsertCardNames(ctx, map[int64]string{
		301: "Baseline",
		302: "Cut",
		401: "Added",
	}); err != nil {
		t.Fatalf("UpsertCardNames: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.UpsertDeck(ctx, tx, "unversioned-deck", "Traditional_Ladder", "Unversioned", "Standard",
		"test", "2026-07-26T08:00:00Z", []DeckCard{
			{Section: gameDeckSectionMain, CardID: 301, Quantity: 2},
			{Section: gameDeckSectionMain, CardID: 302, Quantity: 1},
			{Section: gameDeckSectionSideboard, CardID: 401, Quantity: 1},
		}); err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}
	matchID, err := store.UpsertMatchStart(ctx, tx, "unversioned-match", "Traditional_Ladder", 1, "2026-07-26T08:30:00Z")
	if err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	linked, err := store.LinkMatchToDeckByArenaDeckID(ctx, tx, "unversioned-match", "unversioned-deck", "event_deck")
	if err != nil || !linked {
		t.Fatalf("LinkMatchToDeckByArenaDeckID = %v, %v", linked, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE match_decks SET deck_version_id = NULL WHERE match_id = ?`, matchID); err != nil {
		t.Fatalf("clear linked deck version: %v", err)
	}
	insertSideboardTestGames(t, ctx, tx, matchID, 3)
	if err := store.ReplaceMatchGameDeckSnapshot(ctx, tx, "unversioned-match", 2,
		"2026-07-26T09:00:00Z", "gre_connect", []int64{301, 301, 401}, []int64{302}); err != nil {
		t.Fatalf("ReplaceMatchGameDeckSnapshot(game 2): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	games, err := store.ListMatchGames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchGames: %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("games = %d, want 3", len(games))
	}
	if games[1].SideboardChanges != nil {
		t.Fatalf("game 2 sideboard changes = %#v, want unavailable without a captured game 1 baseline", games[1].SideboardChanges)
	}
	if games[2].SideboardChanges != nil {
		t.Fatalf("game 3 sideboard changes = %#v, want unavailable without a game 3 snapshot", games[2].SideboardChanges)
	}
}

func TestPrepareSideboardSnapshotsBackfillResetsIngestStateOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mustExec(t, database, `DELETE FROM app_metadata WHERE key = 'sideboard_snapshots_backfill_v1'`)
	mustExec(t, database, `INSERT INTO ingest_state (log_path, byte_offset, line_no, updated_at)
		VALUES
			('Player-prev.log', 4321, 65, '2026-07-26T08:00:00Z'),
			('Player.log', 1234, 56, '2026-07-26T08:00:00Z')`)

	if err := prepareSideboardSnapshotsBackfill(ctx, database); err != nil {
		t.Fatalf("prepareSideboardSnapshotsBackfill(first): %v", err)
	}
	assertSideboardBackfillState(t, database, 0, 0, 1)
	var nonResetCursors int64
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ingest_state WHERE byte_offset != 0 OR line_no != 0
	`).Scan(&nonResetCursors); err != nil {
		t.Fatalf("count non-reset ingest cursors: %v", err)
	}
	if nonResetCursors != 0 {
		t.Fatalf("non-reset ingest cursors = %d, want 0", nonResetCursors)
	}

	mustExec(t, database, `UPDATE ingest_state SET byte_offset = 999, line_no = 77 WHERE log_path = 'Player.log'`)
	if err := prepareSideboardSnapshotsBackfill(ctx, database); err != nil {
		t.Fatalf("prepareSideboardSnapshotsBackfill(second): %v", err)
	}
	assertSideboardBackfillState(t, database, 999, 77, 1)
}

func insertSideboardTestGames(t *testing.T, ctx context.Context, tx *sql.Tx, matchID, count int64) {
	t.Helper()
	for gameNumber := int64(1); gameNumber <= count; gameNumber++ {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO games (match_id, game_number, result, derived_at)
			VALUES (?, ?, 'unknown', '2026-07-26T10:00:00Z')
		`, matchID, gameNumber); err != nil {
			t.Fatalf("insert game %d: %v", gameNumber, err)
		}
	}
}

type sideboardCardExpectation struct {
	cardID   int64
	quantity int64
	cardName string
}

func assertSideboardCards(t *testing.T, got []model.SideboardCardRow, want []sideboardCardExpectation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sideboard cards = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index].CardID != want[index].cardID || got[index].Quantity != want[index].quantity ||
			got[index].CardName != want[index].cardName {
			t.Fatalf("sideboard card %d = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func assertSideboardBackfillState(t *testing.T, database *sql.DB, wantOffset, wantLineNo, wantMarkers int64) {
	t.Helper()
	ctx := context.Background()
	var offset, lineNo int64
	if err := database.QueryRowContext(ctx, `
		SELECT byte_offset, line_no FROM ingest_state WHERE log_path = 'Player.log'
	`).Scan(&offset, &lineNo); err != nil {
		t.Fatalf("read ingest state: %v", err)
	}
	if offset != wantOffset || lineNo != wantLineNo {
		t.Fatalf("ingest state = (%d, %d), want (%d, %d)", offset, lineNo, wantOffset, wantLineNo)
	}
	var markerCount int64
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM app_metadata WHERE key = ?
	`, sideboardSnapshotsBackfillMetadataKey).Scan(&markerCount); err != nil {
		t.Fatalf("count sideboard backfill markers: %v", err)
	}
	if markerCount != wantMarkers {
		t.Fatalf("sideboard backfill markers = %d, want %d", markerCount, wantMarkers)
	}
}
