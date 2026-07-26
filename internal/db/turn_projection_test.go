package db

import (
	"context"
	"testing"
)

func requireTurnCount(t *testing.T, got *int64, want int64, label string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s turn count = %v, want %d", label, got, want)
	}
}

func TestMatchReadsProjectPerGameFullTurnTotalsWithoutMutatingRawData(t *testing.T) {
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
	if _, err := store.UpsertDeck(ctx, tx, "turn-deck", "Ladder", "Turn Projection", "Standard",
		"test", "2026-07-25T00:00:00Z", nil); err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}

	seedMatch := func(arenaID, startedAt string, rawMatchTurns int64) int64 {
		t.Helper()
		matchID, seedErr := store.UpsertMatchStart(ctx, tx, arenaID, "Ladder", 1, startedAt)
		if seedErr != nil {
			t.Fatalf("UpsertMatchStart(%s): %v", arenaID, seedErr)
		}
		if _, _, _, seedErr = store.UpdateMatchEnd(ctx, tx, arenaID, 1, 1, rawMatchTurns, 300,
			"Game", startedAt); seedErr != nil {
			t.Fatalf("UpdateMatchEnd(%s): %v", arenaID, seedErr)
		}
		linked, seedErr := store.LinkMatchToDeckByArenaDeckID(ctx, tx, arenaID, "turn-deck", "event_deck")
		if seedErr != nil || !linked {
			t.Fatalf("LinkMatchToDeckByArenaDeckID(%s) = %v, %v", arenaID, linked, seedErr)
		}
		return matchID
	}

	derivedMatchID := seedMatch("turn-derived", "2026-07-25T01:00:00Z", 99)
	cardPlayMatchID := seedMatch("turn-card-plays", "2026-07-25T02:00:00Z", 99)
	storedMatchID := seedMatch("turn-stored", "2026-07-25T03:00:00Z", 19)

	gameOneResult, err := tx.ExecContext(ctx, `
		INSERT INTO games (match_id, game_number, result, turn_count, derived_at)
		VALUES (?, 1, 'win', 15, '2026-07-25T01:05:00Z')
	`, derivedMatchID)
	if err != nil {
		t.Fatalf("insert derived game 1: %v", err)
	}
	gameOneID, err := gameOneResult.LastInsertId()
	if err != nil {
		t.Fatalf("derived game 1 id: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO games (match_id, game_number, result, turn_count, derived_at)
		VALUES (?, 2, 'win', NULL, '2026-07-25T01:10:00Z')
	`, derivedMatchID); err != nil {
		t.Fatalf("insert derived game 2: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO game_turn_stats (
			game_id, match_id, turn_number, is_player_turn, lands_played, spells_cast,
			land_in_hand, source, confidence
		) VALUES
			(?, ?, 13, 1, 0, 0, 1, 'test', 'derived'),
			(?, ?, 15, 1, 0, 0, 1, 'test', 'derived')
	`, gameOneID, derivedMatchID, gameOneID, derivedMatchID); err != nil {
		t.Fatalf("insert raw turn stats: %v", err)
	}

	// The derived game-1 value (15 -> 8) must beat its conflicting card-play
	// maximum (31 -> 16). Game 2 has no derived count, so it falls back to its
	// card-play maximum (11 -> 6). The displayed match total is therefore 14.
	for _, play := range []struct {
		arenaID, label string
		game, instance int64
		turn           int64
	}{
		{arenaID: "turn-derived", label: "derived game 1 conflict", game: 1, instance: 101, turn: 31},
		{arenaID: "turn-derived", label: "derived game 2 fallback", game: 2, instance: 102, turn: 11},
		{arenaID: "turn-card-plays", label: "card-play game 1", game: 1, instance: 201, turn: 3},
		{arenaID: "turn-card-plays", label: "card-play game 2", game: 2, instance: 202, turn: 3},
	} {
		if err := store.UpsertMatchCardPlay(ctx, tx, play.arenaID, play.game, play.instance,
			9000+play.instance, 1, play.turn, "main1", "battlefield", "2026-07-25T01:01:00Z", "test"); err != nil {
			t.Fatalf("UpsertMatchCardPlay(%s): %v", play.label, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	wantByArenaID := map[string]int64{
		"turn-derived":    14, // 8 + 6, converted per game before summing.
		"turn-card-plays": 4,  // 2 + 2, not ceil((3 + 3) / 2) = 3.
		"turn-stored":     10, // Legacy match-only raw turn 19.
	}
	rows, err := store.ListMatches(ctx, 10, "", "")
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if len(rows) != len(wantByArenaID) {
		t.Fatalf("ListMatches returned %d rows, want %d", len(rows), len(wantByArenaID))
	}
	for _, row := range rows {
		requireTurnCount(t, row.TurnCount, wantByArenaID[row.ArenaMatchID], "ListMatches "+row.ArenaMatchID)
	}

	for arenaID, matchID := range map[string]int64{
		"turn-derived":    derivedMatchID,
		"turn-card-plays": cardPlayMatchID,
		"turn-stored":     storedMatchID,
	} {
		detail, err := store.GetMatchDetail(ctx, matchID)
		if err != nil {
			t.Fatalf("GetMatchDetail(%s): %v", arenaID, err)
		}
		requireTurnCount(t, detail.Match.TurnCount, wantByArenaID[arenaID], "GetMatchDetail "+arenaID)
	}

	derivedDetail, err := store.GetMatchDetail(ctx, derivedMatchID)
	if err != nil {
		t.Fatalf("GetMatchDetail(turn-derived): %v", err)
	}
	if len(derivedDetail.Games) != 2 {
		t.Fatalf("derived match games = %d, want 2", len(derivedDetail.Games))
	}
	requireTurnCount(t, derivedDetail.Games[0].TurnCount, 8, "derived game 1")
	if derivedDetail.Games[1].TurnCount != nil {
		t.Fatalf("derived game 2 turn count = %v, want nil", derivedDetail.Games[1].TurnCount)
	}
	if len(derivedDetail.Games[0].TurnStats) != 2 ||
		derivedDetail.Games[0].TurnStats[0].TurnNumber != 13 ||
		derivedDetail.Games[0].TurnStats[1].TurnNumber != 15 {
		t.Fatalf("turn stats were not kept raw: %#v", derivedDetail.Games[0].TurnStats)
	}
	if len(derivedDetail.Games[0].Flags) != 1 || derivedDetail.Games[0].Flags[0].TurnNumber == nil ||
		*derivedDetail.Games[0].Flags[0].TurnNumber != 13 {
		t.Fatalf("raw-dependent flags = %#v, want raw Arena turn 13", derivedDetail.Games[0].Flags)
	}
	if len(derivedDetail.CardPlays) != 2 || derivedDetail.CardPlays[0].TurnNumber == nil ||
		*derivedDetail.CardPlays[0].TurnNumber != 31 || derivedDetail.CardPlays[1].TurnNumber == nil ||
		*derivedDetail.CardPlays[1].TurnNumber != 11 {
		t.Fatalf("card plays were not kept raw: %#v", derivedDetail.CardPlays)
	}

	deck, err := store.GetDeckDetail(ctx, 1, 10)
	if err != nil {
		t.Fatalf("GetDeckDetail: %v", err)
	}
	if len(deck.Matches) != len(wantByArenaID) {
		t.Fatalf("deck matches = %d, want %d", len(deck.Matches), len(wantByArenaID))
	}
	for _, match := range deck.Matches {
		requireTurnCount(t, match.TurnCount, wantByArenaID[match.ArenaMatchID], "GetDeckDetail "+match.ArenaMatchID)
	}

	var rawMatchTurn, rawGameTurn int64
	if err := database.QueryRowContext(ctx, `SELECT turn_count FROM matches WHERE id = ?`, derivedMatchID).Scan(&rawMatchTurn); err != nil {
		t.Fatalf("read raw match turn: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT turn_count FROM games WHERE id = ?`, gameOneID).Scan(&rawGameTurn); err != nil {
		t.Fatalf("read raw game turn: %v", err)
	}
	if rawMatchTurn != 99 || rawGameTurn != 15 {
		t.Fatalf("raw turns mutated: match=%d game=%d, want 99 and 15", rawMatchTurn, rawGameTurn)
	}
}
