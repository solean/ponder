package db

import (
	"context"
	"testing"
	"time"
)

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
