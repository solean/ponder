package db

import (
	"context"
	"testing"
)

func TestListDecksByScopeIncludesDeckActivityTimestamps(t *testing.T) {
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

	lastUpdated := "2026-04-04T00:49:51.310561Z"
	if _, err := store.UpsertDeck(
		ctx,
		tx,
		"draft-deck-1",
		"PremierDraft_TMT_20260303",
		"Draft Deck",
		"Draft",
		"test",
		lastUpdated,
		nil,
	); err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}

	firstPlayedAt := "2026-04-04T00:50:21.247Z"
	if _, err := store.UpsertMatchStart(ctx, tx, "match-1", "PremierDraft_TMT_20260303", 1, firstPlayedAt); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "match-1", "PremierDraft_TMT_20260303", "test"); err != nil {
		t.Fatalf("LinkMatchToLatestDeckByEvent: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListDecksByScope(ctx, "draft")
	if err != nil {
		t.Fatalf("ListDecksByScope: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListDecksByScope) = %d, want 1", len(rows))
	}

	row := rows[0]
	if row.FirstPlayedAt != firstPlayedAt {
		t.Fatalf("FirstPlayedAt = %q, want %q", row.FirstPlayedAt, firstPlayedAt)
	}
	if row.LastPlayedAt != firstPlayedAt {
		t.Fatalf("LastPlayedAt = %q, want %q", row.LastPlayedAt, firstPlayedAt)
	}
	if row.LastUpdatedAt != lastUpdated {
		t.Fatalf("LastUpdatedAt = %q, want %q", row.LastUpdatedAt, lastUpdated)
	}
	if len(row.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(row.Results))
	}
	if got := row.Results[0]; got.EventName != "PremierDraft_TMT_20260303" || got.PlayedAt != firstPlayedAt {
		t.Fatalf("Results[0] = %+v, want event and played-at from match-1", got)
	}
}

func TestListDecksByScopeOrdersResultsNewestFirst(t *testing.T) {
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

	deckID, err := store.UpsertDeck(ctx, tx, "constructed-1", "Ladder", "Izzet", "Standard", "test", "", []DeckCard{
		{Section: "main", CardID: 100, Quantity: 4},
		{Section: "sideboard", CardID: 200, Quantity: 2},
	})
	if err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}
	if _, err := store.UpsertDeck(ctx, tx, "unplayed-1", "Play", "Brewing", "Standard", "test", "", nil); err != nil {
		t.Fatalf("UpsertDeck unplayed: %v", err)
	}

	matches := []struct{ id, event, startedAt string }{
		{"m-old", "Ladder", "2026-09-01T10:00:00Z"},
		{"m-new", "Traditional_Ladder", "2026-10-01T10:00:00Z"},
	}
	for _, m := range matches {
		if _, err := store.UpsertMatchStart(ctx, tx, m.id, m.event, 1, m.startedAt); err != nil {
			t.Fatalf("UpsertMatchStart %s: %v", m.id, err)
		}
		if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, m.id, "Ladder", "test"); err != nil {
			t.Fatalf("LinkMatchToLatestDeckByEvent %s: %v", m.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListDecksByScope(ctx, "constructed")
	if err != nil {
		t.Fatalf("ListDecksByScope: %v", err)
	}
	byName := map[string]int{}
	for i, row := range rows {
		byName[row.DeckName] = i
	}

	played := rows[byName["Izzet"]]
	if len(played.Results) != 2 {
		t.Fatalf("len(Results) = %d, want 2", len(played.Results))
	}
	if played.Results[0].EventName != "Traditional_Ladder" || played.Results[1].EventName != "Ladder" {
		t.Fatalf("Results = %+v, want newest (Traditional_Ladder) first", played.Results)
	}
	if played.LastPlayedAt != "2026-10-01T10:00:00Z" {
		t.Fatalf("LastPlayedAt = %q, want newest match start", played.LastPlayedAt)
	}

	unplayed := rows[byName["Brewing"]]
	if unplayed.Results == nil || len(unplayed.Results) != 0 {
		t.Fatalf("unplayed Results = %#v, want empty non-nil slice", unplayed.Results)
	}

	quantities, err := store.ListDeckMainCardQuantities(ctx, []int64{deckID})
	if err != nil {
		t.Fatalf("ListDeckMainCardQuantities: %v", err)
	}
	if got := quantities[deckID]; len(got) != 1 || got[100] != 4 {
		t.Fatalf("main quantities = %v, want only card 100 x4", got)
	}
}
