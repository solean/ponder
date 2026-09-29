package db

import (
	"context"
	"testing"
)

func TestListDraftSessionsIncludesMatchedDeckResults(t *testing.T) {
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

	sessionID, err := store.EnsureDraftSession(ctx, tx, "PremierDraft_TMT_20260303", ptrString("draft-results"), false, "2026-04-04T00:33:13.720644Z")
	if err != nil {
		t.Fatalf("EnsureDraftSession: %v", err)
	}
	if err := store.CompleteDraftSession(ctx, tx, "PremierDraft_TMT_20260303", ptrString("draft-results"), false, "2026-04-04T00:46:25.744975Z"); err != nil {
		t.Fatalf("CompleteDraftSession: %v", err)
	}
	if err := store.InsertDraftPick(ctx, tx, sessionID, 1, 1, []int64{1001}, nil, "2026-04-04T00:33:13.720644Z"); err != nil {
		t.Fatalf("InsertDraftPick: %v", err)
	}

	deckID, err := store.UpsertDeck(ctx, tx, "draft-deck-results", "PremierDraft_TMT_20260303", "Draft Deck", "Draft", "test", "2026-04-04T00:49:51.310561Z", nil)
	if err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}

	matchStarts := []struct {
		id     string
		result string
		start  string
		end    string
	}{
		{"match-1", "win", "2026-04-04T00:50:21.247Z", "2026-04-04T00:58:26.522Z"},
		{"match-2", "win", "2026-04-04T00:59:03.113Z", "2026-04-04T01:11:00.737Z"},
		{"match-3", "loss", "2026-04-04T01:12:12.386Z", "2026-04-04T01:17:44.007Z"},
	}
	for _, match := range matchStarts {
		if _, err := store.UpsertMatchStart(ctx, tx, match.id, "PremierDraft_TMT_20260303", 1, match.start); err != nil {
			t.Fatalf("UpsertMatchStart(%s): %v", match.id, err)
		}
		if _, _, _, err := store.UpdateMatchEnd(ctx, tx, match.id, 1, map[string]int64{"win": 1, "loss": 2}[match.result], 0, 0, "", match.end); err != nil {
			t.Fatalf("UpdateMatchEnd(%s): %v", match.id, err)
		}
		if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, match.id, "PremierDraft_TMT_20260303", "test"); err != nil {
			t.Fatalf("LinkMatchToLatestDeckByEvent(%s): %v", match.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("ListDraftSessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListDraftSessions) = %d, want 1", len(rows))
	}
	if rows[0].Wins == nil || *rows[0].Wins != 2 {
		t.Fatalf("Wins = %v, want 2", rows[0].Wins)
	}
	if rows[0].Losses == nil || *rows[0].Losses != 1 {
		t.Fatalf("Losses = %v, want 1", rows[0].Losses)
	}
	if rows[0].DeckID == nil || *rows[0].DeckID != deckID {
		t.Fatalf("DeckID = %v, want %d", rows[0].DeckID, deckID)
	}
}

func TestListDraftSessionsGivesReentriesTheirOwnDeck(t *testing.T) {
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

	const event = "PremierDraft_HOB_20260811"
	drafts := []struct {
		draftID, started, completed, deckArenaID, deckTS string
	}{
		{"draft-a", "2026-09-01T10:00:00Z", "2026-09-01T10:20:00Z", "deck-a", "2026-09-01T10:25:00Z"},
		{"draft-b", "2026-09-03T10:00:00Z", "2026-09-03T10:20:00Z", "deck-b", "2026-09-03T10:25:00Z"},
		// Abandoned before building: must not borrow draft-c's deck.
		{"draft-abandoned", "2026-09-05T10:00:00Z", "2026-09-05T10:20:00Z", "", ""},
		{"draft-c", "2026-09-07T10:00:00Z", "2026-09-07T10:20:00Z", "deck-c", "2026-09-07T10:25:00Z"},
	}
	deckIDs := map[string]int64{}
	for _, draft := range drafts {
		if _, err := store.EnsureDraftSession(ctx, tx, event, ptrString(draft.draftID), false, draft.started); err != nil {
			t.Fatalf("EnsureDraftSession(%s): %v", draft.draftID, err)
		}
		if err := store.CompleteDraftSession(ctx, tx, event, ptrString(draft.draftID), false, draft.completed); err != nil {
			t.Fatalf("CompleteDraftSession(%s): %v", draft.draftID, err)
		}
		if draft.deckArenaID == "" {
			continue
		}
		deckID, err := store.UpsertDeck(ctx, tx, draft.deckArenaID, event, "Draft Deck", "Draft", "test", draft.deckTS, nil)
		if err != nil {
			t.Fatalf("UpsertDeck(%s): %v", draft.deckArenaID, err)
		}
		deckIDs[draft.draftID] = deckID
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("ListDraftSessions: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("len(ListDraftSessions) = %d, want 4", len(rows))
	}
	for _, row := range rows {
		want, hasDeck := deckIDs[*row.DraftID]
		if !hasDeck {
			if row.DeckID != nil {
				t.Fatalf("session %s DeckID = %d, want none", *row.DraftID, *row.DeckID)
			}
			continue
		}
		if row.DeckID == nil || *row.DeckID != want {
			t.Fatalf("session %s DeckID = %v, want %d", *row.DraftID, row.DeckID, want)
		}
	}
}
