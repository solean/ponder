package db

import (
	"context"
	"testing"
	"time"
)

func TestOverviewIncludesPlayerName(t *testing.T) {
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

	if err := store.SavePlayerName(ctx, tx, "SelfRenamed"); err != nil {
		t.Fatalf("SavePlayerName: %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "match-win", "Traditional_Ladder", 1, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-win): %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-win", 1, 1, 9, 420, "Concede", "2026-03-12T19:13:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd(match-win): %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "match-loss", "Traditional_Ladder", 1, "2026-03-12T20:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-loss): %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-loss", 1, 2, 11, 540, "Game", "2026-03-12T20:15:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd(match-loss): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	overview, err := store.Overview(ctx, 10)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if overview.PlayerName != "SelfRenamed" {
		t.Fatalf("PlayerName = %q, want SelfRenamed", overview.PlayerName)
	}
	if overview.TotalMatches != 2 || overview.Wins != 1 || overview.Losses != 1 {
		t.Fatalf("overview counters = %+v, want total=2 wins=1 losses=1", overview)
	}
	if len(overview.Recent) != 2 {
		t.Fatalf("len(Recent) = %d, want 2", len(overview.Recent))
	}
}

func TestOverviewWinRateIgnoresUnknownResults(t *testing.T) {
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

	if _, err := store.UpsertMatchStart(ctx, tx, "match-win", "Traditional_Ladder", 1, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-win): %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-win", 1, 1, 9, 420, "Concede", "2026-03-12T19:13:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd(match-win): %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "match-loss", "Traditional_Ladder", 1, "2026-03-12T20:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-loss): %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-loss", 1, 2, 11, 540, "Game", "2026-03-12T20:15:52Z"); err != nil {
		t.Fatalf("UpdateMatchEnd(match-loss): %v", err)
	}
	// No UpdateMatchEnd: this match stays at result "unknown".
	if _, err := store.UpsertMatchStart(ctx, tx, "match-unknown", "Traditional_Ladder", 1, "2026-03-12T21:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-unknown): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	overview, err := store.Overview(ctx, 10)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if overview.TotalMatches != 3 || overview.Wins != 1 || overview.Losses != 1 {
		t.Fatalf("overview counters = %+v, want total=3 wins=1 losses=1", overview)
	}
	if overview.WinRate != 0.5 {
		t.Fatalf("WinRate = %v, want 0.5 (unknown results excluded)", overview.WinRate)
	}
}

func TestMatchListDerivesBestOfAndPlayDraw(t *testing.T) {
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

	if _, err := store.UpsertMatchStart(ctx, tx, "match-bo3", "Some_Event", 2, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-bo3): %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "match-bo3", 1, 101, 5001, 1, 1, "main1", "battlefield", "2026-03-12T19:07:00Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(match-bo3 game 1): %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "match-bo3", 2, 102, 5002, 1, 1, "main1", "battlefield", "2026-03-12T19:17:00Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(match-bo3 game 2): %v", err)
	}

	if _, err := store.UpsertMatchStart(ctx, tx, "match-bo1", "PremierDraft_ABC", 1, "2026-03-12T20:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(match-bo1): %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "match-bo1", 1, 201, 6001, 1, 2, "main1", "battlefield", "2026-03-12T20:07:00Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(match-bo1 game 1): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListMatches(ctx, 10, "", "")
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(ListMatches) = %d, want 2", len(rows))
	}

	byArenaID := make(map[string]struct {
		id       int64
		bestOf   string
		playDraw string
	}, len(rows))
	for _, row := range rows {
		byArenaID[row.ArenaMatchID] = struct {
			id       int64
			bestOf   string
			playDraw string
		}{
			id:       row.ID,
			bestOf:   row.BestOf,
			playDraw: row.PlayDraw,
		}
	}

	if got := byArenaID["match-bo3"]; got.bestOf != "bo3" || got.playDraw != "draw" {
		t.Fatalf("match-bo3 derived values = %+v, want bestOf=bo3 playDraw=draw", got)
	}
	if got := byArenaID["match-bo1"]; got.bestOf != "bo1" || got.playDraw != "draw" {
		t.Fatalf("match-bo1 derived values = %+v, want bestOf=bo1 playDraw=draw", got)
	}

	detail, err := store.GetMatchDetail(ctx, byArenaID["match-bo3"].id)
	if err != nil {
		t.Fatalf("GetMatchDetail(match-bo3): %v", err)
	}
	if detail.Match.BestOf != "bo3" || detail.Match.PlayDraw != "draw" {
		t.Fatalf("match detail derived values = %+v, want bestOf=bo3 playDraw=draw", detail.Match)
	}
}

func TestUpsertMatchStartBackfillsEarlierObservation(t *testing.T) {
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
	if _, _, _, err := store.UpdateMatchEnd(
		ctx, tx, "recovered-match", 1, 1, 0, 0, "Concede", "2026-09-02T14:16:53Z",
	); err != nil {
		t.Fatalf("UpdateMatchEnd: %v", err)
	}
	if _, err := store.UpsertMatchStart(
		ctx, tx, "recovered-match", "PremierDraft_HOB_20260811", 1, "2026-09-02T14:07:12Z",
	); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var startedAt, endedAt string
	if err := database.QueryRowContext(ctx, `
		SELECT started_at, ended_at
		FROM matches
		WHERE arena_match_id = 'recovered-match'
	`).Scan(&startedAt, &endedAt); err != nil {
		t.Fatalf("read recovered match timestamps: %v", err)
	}
	if startedAt != "2026-09-02T14:07:12Z" || endedAt != "2026-09-02T14:16:53Z" {
		t.Fatalf("recovered timestamps = %s–%s, want 14:07:12–14:16:53", startedAt, endedAt)
	}
}

func TestLinkMatchToLatestDeckByEventPrefersMostRecentlyObservedDeck(t *testing.T) {
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

	if _, err := store.UpsertDeck(
		ctx,
		tx,
		"deck-excruciator",
		"Traditional_Ladder",
		"Excruciator",
		"TraditionalStandard",
		"test",
		"2026-03-30T05:11:08.475585Z",
		nil,
	); err != nil {
		t.Fatalf("UpsertDeck(Excruciator): %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if _, err := store.UpsertDeck(
		ctx,
		tx,
		"deck-dimir",
		"Traditional_Ladder",
		"Dimir Mid 2026",
		"TraditionalStandard",
		"test",
		"2026-03-13T02:13:30.379740Z",
		nil,
	); err != nil {
		t.Fatalf("UpsertDeck(Dimir Mid 2026): %v", err)
	}

	startedAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := store.UpsertMatchStart(ctx, tx, "match-latest-deck", "Traditional_Ladder", 1, startedAt); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-latest-deck", "Traditional_Ladder", "room_state"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListMatches(ctx, 10, "", "")
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListMatches) = %d, want 1", len(rows))
	}
	if rows[0].DeckName == nil || *rows[0].DeckName != "Dimir Mid 2026" {
		t.Fatalf("DeckName = %v, want Dimir Mid 2026", rows[0].DeckName)
	}
	if rows[0].Format != "TraditionalStandard" {
		t.Fatalf("Format = %q, want TraditionalStandard", rows[0].Format)
	}

	detail, err := store.GetMatchDetail(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("GetMatchDetail: %v", err)
	}
	if detail.Match.Format != "TraditionalStandard" {
		t.Fatalf("detail Format = %q, want TraditionalStandard", detail.Match.Format)
	}
}

func TestLinkMatchToLatestDeckByEventUsesArenaTimestampForDrafts(t *testing.T) {
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

	const eventName = "PremierDraft_HOB_20260811"
	if _, err := store.UpsertDeck(
		ctx, tx, "old-draft-deck", eventName, "Draft Deck", "Draft", "event_set_deck",
		"2026-08-30T21:22:18Z", nil,
	); err != nil {
		t.Fatalf("UpsertDeck(old): %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE decks
		SET updated_at = '2026-08-31T03:30:38Z'
		WHERE arena_deck_id = 'old-draft-deck'
	`); err != nil {
		t.Fatalf("age old deck ingestion timestamp: %v", err)
	}
	if _, err := store.UpsertDeck(
		ctx, tx, "new-draft-deck", eventName, "Draft Deck", "Draft", "event_set_deck",
		"2026-09-02T01:05:29Z", nil,
	); err != nil {
		t.Fatalf("UpsertDeck(new): %v", err)
	}

	if _, err := store.UpsertMatchStart(
		ctx, tx, "recovered-draft-match", eventName, 1, "2026-09-02T14:07:12Z",
	); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(
		ctx, tx, "recovered-draft-match", eventName, "room_state",
	); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var arenaDeckID string
	if err := database.QueryRowContext(ctx, `
		SELECT d.arena_deck_id
		FROM match_decks md
		JOIN decks d ON d.id = md.deck_id
		JOIN matches m ON m.id = md.match_id
		WHERE m.arena_match_id = 'recovered-draft-match'
	`).Scan(&arenaDeckID); err != nil {
		t.Fatalf("read linked draft deck: %v", err)
	}
	if arenaDeckID != "new-draft-deck" {
		t.Fatalf("linked deck = %q, want new-draft-deck", arenaDeckID)
	}
}

func TestLinkMatchToLatestDeckByEventRoomStateOverridesPreMatchOnlyOnce(t *testing.T) {
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

	if _, err := store.UpsertDeck(
		ctx,
		tx,
		"deck-one",
		"Traditional_Ladder",
		"Deck One",
		"TraditionalStandard",
		"test",
		"2026-03-01T00:00:00Z",
		nil,
	); err != nil {
		t.Fatalf("UpsertDeck(deck-one): %v", err)
	}

	startedAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := store.UpsertMatchStart(ctx, tx, "match-room-state", "Traditional_Ladder", 1, startedAt); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-room-state", "Traditional_Ladder", "pre_match"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent(pre_match): %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if _, err := store.UpsertDeck(
		ctx,
		tx,
		"deck-two",
		"Traditional_Ladder",
		"Deck Two",
		"TraditionalStandard",
		"test",
		"2026-02-01T00:00:00Z",
		nil,
	); err != nil {
		t.Fatalf("UpsertDeck(deck-two): %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-room-state", "Traditional_Ladder", "room_state"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent(room_state override): %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if _, err := store.UpsertDeck(
		ctx,
		tx,
		"deck-three",
		"Traditional_Ladder",
		"Deck Three",
		"TraditionalStandard",
		"test",
		"2026-01-01T00:00:00Z",
		nil,
	); err != nil {
		t.Fatalf("UpsertDeck(deck-three): %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-room-state", "Traditional_Ladder", "room_state"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent(room_state replay): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListMatches(ctx, 10, "", "")
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListMatches) = %d, want 1", len(rows))
	}
	if rows[0].DeckName == nil || *rows[0].DeckName != "Deck Two" {
		t.Fatalf("DeckName = %v, want Deck Two", rows[0].DeckName)
	}

	var links int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_decks WHERE match_id = ?`, rows[0].ID).Scan(&links); err != nil {
		t.Fatalf("count match_decks: %v", err)
	}
	if links != 1 {
		t.Fatalf("match_decks rows = %d, want 1", links)
	}
}

func TestLinkMatchToDeckByArenaDeckIDOverridesEventNameGuess(t *testing.T) {
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

	if _, err := store.UpsertDeck(ctx, tx, "deck-izzet", "Traditional_Ladder", "Izzet Prowess", "TraditionalStandard", "test", "2026-07-01T00:00:00Z", nil); err != nil {
		t.Fatalf("UpsertDeck(deck-izzet): %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Most recently observed deck for the event: the event-name heuristic
	// links to this one.
	if _, err := store.UpsertDeck(ctx, tx, "deck-dimir", "Traditional_Ladder", "Dimir Mid 2026", "TraditionalStandard", "test", "2026-07-02T00:00:00Z", nil); err != nil {
		t.Fatalf("UpsertDeck(deck-dimir): %v", err)
	}

	startedAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := store.UpsertMatchStart(ctx, tx, "match-exact-deck", "Traditional_Ladder", 1, startedAt); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-exact-deck", "Traditional_Ladder", "room_state"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent(room_state): %v", err)
	}

	// An unknown arena deck id is not handled, so callers can fall back.
	linked, err := store.LinkMatchToDeckByArenaDeckID(ctx, tx, "match-exact-deck", "deck-missing", "event_deck")
	if err != nil {
		t.Fatalf("LinkMatchToDeckByArenaDeckID(deck-missing): %v", err)
	}
	if linked {
		t.Fatalf("LinkMatchToDeckByArenaDeckID(deck-missing) = true, want false")
	}

	// The exact deck id reported by Arena overrides the event-name guess.
	linked, err = store.LinkMatchToDeckByArenaDeckID(ctx, tx, "match-exact-deck", "deck-izzet", "event_deck")
	if err != nil {
		t.Fatalf("LinkMatchToDeckByArenaDeckID(deck-izzet): %v", err)
	}
	if !linked {
		t.Fatalf("LinkMatchToDeckByArenaDeckID(deck-izzet) = false, want true")
	}

	// A later event-name guess must not override the exact link.
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-exact-deck", "Traditional_Ladder", "room_state"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent(room_state replay): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListMatches(ctx, 10, "", "")
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListMatches) = %d, want 1", len(rows))
	}
	if rows[0].DeckName == nil || *rows[0].DeckName != "Izzet Prowess" {
		t.Fatalf("DeckName = %v, want Izzet Prowess", rows[0].DeckName)
	}

	var links int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_decks WHERE match_id = ?`, rows[0].ID).Scan(&links); err != nil {
		t.Fatalf("count match_decks: %v", err)
	}
	if links != 1 {
		t.Fatalf("match_decks rows = %d, want 1", links)
	}
}

func TestCountMatchesAppliesEventAndResultFilters(t *testing.T) {
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

	seed := []struct {
		arenaID       string
		eventName     string
		winningTeamID int64
	}{
		{"ladder-win-1", "Ladder", 1},
		{"ladder-win-2", "Ladder", 1},
		{"ladder-loss", "Ladder", 2},
		{"draft-win", "PremierDraft_ABC", 1},
	}
	for _, match := range seed {
		if _, err := store.UpsertMatchStart(ctx, tx, match.arenaID, match.eventName, 1, "2026-03-12T19:06:52Z"); err != nil {
			t.Fatalf("UpsertMatchStart(%s): %v", match.arenaID, err)
		}
		if _, _, _, err := store.UpdateMatchEnd(ctx, tx, match.arenaID, 1, match.winningTeamID, 9, 420, "Game", "2026-03-12T19:13:52Z"); err != nil {
			t.Fatalf("UpdateMatchEnd(%s): %v", match.arenaID, err)
		}
	}
	// Never ended, so it keeps result "unknown" and only shows up unfiltered.
	if _, err := store.UpsertMatchStart(ctx, tx, "ladder-open", "Ladder", 1, "2026-03-12T21:06:52Z"); err != nil {
		t.Fatalf("UpsertMatchStart(ladder-open): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	cases := []struct {
		name      string
		eventName string
		result    string
		want      int64
	}{
		{"no filter counts every match", "", "", 5},
		{"event filter", "Ladder", "", 4},
		{"result filter", "", "win", 3},
		{"event and result filter", "Ladder", "win", 2},
		{"unmatched event", "Nonexistent", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			total, err := store.CountMatches(ctx, tc.eventName, tc.result)
			if err != nil {
				t.Fatalf("CountMatches(%q, %q): %v", tc.eventName, tc.result, err)
			}
			if total != tc.want {
				t.Fatalf("CountMatches(%q, %q) = %d, want %d", tc.eventName, tc.result, total, tc.want)
			}
		})
	}
}
