package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestProjectLiveDeckSubtractsKnownCards(t *testing.T) {
	t.Parallel()

	cards := []model.DeckCardRow{
		{Section: "main", CardID: 101, Quantity: 2, CardName: "Alpha"},
		{Section: "main", CardID: 102, Quantity: 3, CardName: "Beta"},
		{Section: "sideboard", CardID: 103, Quantity: 1, CardName: "Sideboard"},
	}
	deck, total, library := projectLiveDeck(cards, map[int64]int64{101: 1, 102: 4}, true)
	if total != 5 || library == nil || *library != 1 {
		t.Fatalf("projected totals = %d, %v; want 5, 1", total, library)
	}
	if len(deck) != 2 || deck[0].Remaining == nil || *deck[0].Remaining != 1 ||
		deck[1].Remaining == nil || *deck[1].Remaining != 0 {
		t.Fatalf("projected deck = %#v, want remaining counts 1 and 0", deck)
	}
}

func TestLiveEndpointProjectsTurnButStoreRetainsArenaTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	store := db.NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "live-turn-projection", "Ladder", 1,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("upsert match: %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "live-turn-projection", 1, 101, 5001, 1, 4,
		"main1", "battlefield", time.Now().UTC().Format(time.RFC3339Nano), "test"); err != nil {
		t.Fatalf("upsert card play: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rows, err := store.ListMatches(ctx, 1, "", "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("list matches = %#v, %v", rows, err)
	}
	_, rawTurn, err := store.GetLiveProgress(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("get raw live progress: %v", err)
	}
	if rawTurn != 4 {
		t.Fatalf("stored live turn = %d, want raw Arena turn 4", rawTurn)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/live", nil)
	rec := httptest.NewRecorder()
	NewServer(store, "", nil).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response struct {
		Live *model.LiveMatch `json:"live"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Live == nil {
		t.Fatal("live response is nil")
	}
	if response.Live.TurnNumber != 2 {
		t.Fatalf("displayed live turn = %d, want full turn 2", response.Live.TurnNumber)
	}
}

func TestLiveEndpointUsesSubmittedDeckAndLatestKnownZones(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	store := db.NewStore(database)
	if err := store.UpsertCardNames(ctx, map[int64]string{
		101: "Alpha",
		102: "Beta",
		999: "Old Linked Card",
	}); err != nil {
		t.Fatalf("upsert card names: %v", err)
	}

	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := store.UpsertDeck(ctx, tx, "linked-deck", "Ladder", "Old deck", "standard", "test", observedAt,
		[]db.DeckCard{{Section: "main", CardID: 999, Quantity: 4}}); err != nil {
		t.Fatalf("upsert linked deck: %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "live-submitted-deck", "Ladder", 1, observedAt); err != nil {
		t.Fatalf("upsert match: %v", err)
	}
	if linked, err := store.LinkMatchToDeckByArenaDeckID(
		ctx, tx, "live-submitted-deck", "linked-deck", "event_deck",
	); err != nil || !linked {
		t.Fatalf("link match deck = %v, %v", linked, err)
	}
	if err := store.ReplaceMatchGameDeckSnapshot(
		ctx,
		tx,
		"live-submitted-deck",
		2,
		observedAt,
		"gre_submit_deck",
		[]int64{101, 101, 102, 102, 102},
		nil,
	); err != nil {
		t.Fatalf("replace submitted deck snapshot: %v", err)
	}

	selfSeat, opponentSeat := int64(1), int64(2)
	objects := []model.MatchReplayFrameObjectRow{
		{InstanceID: 1, CardID: 101, OwnerSeatID: &selfSeat, ZoneType: "hand"},
		{InstanceID: 2, CardID: 102, OwnerSeatID: &selfSeat, ZoneType: "battlefield"},
		{InstanceID: 3, CardID: 102, OwnerSeatID: &selfSeat, ControllerSeatID: &opponentSeat, ZoneType: "battlefield"},
		{InstanceID: 4, CardID: 101, OwnerSeatID: &opponentSeat, ZoneType: "battlefield"},
		{InstanceID: 5, CardID: 101, OwnerSeatID: &selfSeat, ZoneType: "battlefield", IsToken: true},
	}
	if _, err := store.ReplaceMatchReplayFrame(
		ctx,
		tx,
		"live-submitted-deck",
		2,
		10,
		0,
		5,
		"full",
		"play",
		"main1",
		"",
		"",
		observedAt,
		"test",
		nil,
		nil,
		nil,
		objects,
	); err != nil {
		t.Fatalf("replace replay frame: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/live", nil)
	rec := httptest.NewRecorder()
	NewServer(store, "", nil).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response struct {
		Live *model.LiveMatch `json:"live"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Live == nil {
		t.Fatal("live response is nil")
	}
	if response.Live.DeckSource != "submitted" || response.Live.GameNumber != 2 {
		t.Fatalf("live deck source/game = %q/%d, want submitted/2", response.Live.DeckSource, response.Live.GameNumber)
	}
	if response.Live.DeckTotal != 5 || response.Live.LibraryCount == nil || *response.Live.LibraryCount != 2 {
		t.Fatalf("live totals = %d, %v; want 5, 2", response.Live.DeckTotal, response.Live.LibraryCount)
	}
	if len(response.Live.Deck) != 2 {
		t.Fatalf("live deck = %#v, want only two submitted mainboard cards", response.Live.Deck)
	}
	remaining := make(map[int64]int64, len(response.Live.Deck))
	for _, card := range response.Live.Deck {
		if card.Remaining == nil {
			t.Fatalf("remaining count missing for card %#v", card)
		}
		remaining[card.CardID] = *card.Remaining
	}
	if remaining[101] != 1 || remaining[102] != 1 {
		t.Fatalf("remaining counts = %#v, want 101:1 and 102:1", remaining)
	}
	if _, found := remaining[999]; found {
		t.Fatal("linked-deck card leaked into submitted game deck")
	}
}
