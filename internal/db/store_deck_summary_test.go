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
	if row.LastUpdatedAt != lastUpdated {
		t.Fatalf("LastUpdatedAt = %q, want %q", row.LastUpdatedAt, lastUpdated)
	}
}
