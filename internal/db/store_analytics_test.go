package db

import (
	"context"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func testHandFrame(gameStateID int64, turnNumber *int64, stage string, cards map[int64]int64) model.MatchReplayFrameRow {
	frame := model.MatchReplayFrameRow{
		GameNumber:  pointerInt64(1),
		GameStateID: pointerInt64(gameStateID),
		GameStage:   stage,
		TurnNumber:  turnNumber,
		RecordedAt:  "2026-07-17T00:00:00Z",
	}
	for instanceID, cardID := range cards {
		frame.Objects = append(frame.Objects, model.MatchReplayFrameObjectRow{
			InstanceID: instanceID,
			CardID:     cardID,
			PlayerSide: "self",
			ZoneType:   "hand",
		})
	}
	return frame
}

func TestDeriveOpeningHandsTracksLondonMulliganAndBottomedCard(t *testing.T) {
	t.Parallel()

	firstOffer := map[int64]int64{1: 101, 2: 102, 3: 103, 4: 104, 5: 105, 6: 106, 7: 107}
	secondOffer := map[int64]int64{11: 201, 12: 202, 13: 203, 14: 204, 15: 205, 16: 206, 17: 207}
	kept := map[int64]int64{11: 201, 12: 202, 13: 203, 14: 204, 15: 205, 16: 206}
	turnOne := int64(1)
	frames := []model.MatchReplayFrameRow{
		testHandFrame(1, nil, "GameStage_Start", firstOffer),
		testHandFrame(2, nil, "", firstOffer), // repeated state is not another attempt
		testHandFrame(3, nil, "", secondOffer),
		testHandFrame(4, nil, "", kept),
		testHandFrame(5, &turnOne, "GameStage_Play", kept),
	}

	opening := deriveOpeningHands(frames)
	hands, mulligans, keptSize := opening.Hands, opening.MulliganCount, opening.KeptHandSize
	if len(hands) != 2 {
		t.Fatalf("opening hand attempts = %d, want 2", len(hands))
	}
	if mulligans == nil || *mulligans != 1 {
		t.Fatalf("mulligan count = %#v, want 1", mulligans)
	}
	if keptSize == nil || *keptSize != 6 {
		t.Fatalf("kept hand size = %#v, want 6", keptSize)
	}
	if hands[0].Decision != "mulligan" || hands[1].Decision != "keep" {
		t.Fatalf("decisions = %q, %q, want mulligan then keep", hands[0].Decision, hands[1].Decision)
	}

	keptCards := int64(0)
	bottomedCards := int64(0)
	for _, card := range hands[1].Cards {
		if card.Kept {
			keptCards += card.Quantity
		} else {
			bottomedCards += card.Quantity
		}
	}
	if keptCards != 6 || bottomedCards != 1 {
		t.Fatalf("final hand kept=%d bottomed=%d, want 6 and 1", keptCards, bottomedCards)
	}
}

func TestDeckVersionsAreImmutableAndMatchLinksUseHistoricalVersion(t *testing.T) {
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
	deckID, err := store.UpsertDeck(ctx, tx, "deck-1", "Ladder", "Test", "Standard", "test",
		"2026-07-01T00:00:00Z", []DeckCard{{Section: "main", CardID: 101, Quantity: 4}})
	if err != nil {
		t.Fatalf("UpsertDeck(v1): %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "match-1", "Ladder", 1, "2026-07-02T00:00:00Z"); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if _, err := store.UpsertDeck(ctx, tx, "deck-1", "Ladder", "Test", "Standard", "test",
		"2026-07-03T00:00:00Z", []DeckCard{{Section: "main", CardID: 202, Quantity: 4}}); err != nil {
		t.Fatalf("UpsertDeck(v2): %v", err)
	}
	linked, err := store.LinkMatchToDeckByArenaDeckID(ctx, tx, "match-1", "deck-1", "event_deck")
	if err != nil || !linked {
		t.Fatalf("LinkMatchToDeckByArenaDeckID = %v, %v", linked, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	versions, err := store.ListDeckVersions(ctx, deckID)
	if err != nil {
		t.Fatalf("ListDeckVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("deck versions = %d, want 2", len(versions))
	}
	if versions[0].VersionNumber != 2 || versions[0].Cards[0].CardID != 202 {
		t.Fatalf("latest version = %#v, want version 2 with card 202", versions[0])
	}
	if versions[1].VersionNumber != 1 || versions[1].Cards[0].CardID != 101 {
		t.Fatalf("original version = %#v, want version 1 with card 101", versions[1])
	}

	var linkedVersion int64
	if err := database.QueryRow(`
		SELECT dv.version_number
		FROM match_decks md
		JOIN deck_versions dv ON dv.id = md.deck_version_id
		WHERE md.match_id = 1
	`).Scan(&linkedVersion); err != nil {
		t.Fatalf("load linked version: %v", err)
	}
	if linkedVersion != 1 {
		t.Fatalf("match linked version = %d, want historical version 1", linkedVersion)
	}
}

func TestRefreshMatchAnalyticsPreservesGameIdentityAndZeroMulligans(t *testing.T) {
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
	matchID, err := store.UpsertMatchStart(ctx, tx, "analytics-match", "Ladder", 1, "2026-07-17T00:00:00Z")
	if err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	handObjects := make([]model.MatchReplayFrameObjectRow, 0, 7)
	for index := int64(1); index <= 7; index++ {
		handObjects = append(handObjects, model.MatchReplayFrameObjectRow{
			InstanceID:  index,
			CardID:      100 + index,
			OwnerSeatID: pointerInt64(1),
			PlayerSide:  "self",
			ZoneType:    "hand",
		})
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "analytics-match", 1, 1, 0, 0,
		"GameStateType_Full", "GameStage_Start", "", "", "", "2026-07-17T00:00:01Z", "test",
		nil, nil, nil, handObjects); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(start): %v", err)
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "analytics-match", 1, 2, 1, 1,
		"GameStateType_Diff", "GameStage_Play", "main1", "self", "Concede", "2026-07-17T00:00:02Z", "test",
		nil, nil, nil, handObjects); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(play): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := store.RefreshMatchAnalytics(ctx, matchID); err != nil {
		t.Fatalf("RefreshMatchAnalytics(first): %v", err)
	}
	var firstGameID int64
	var mulliganCount *int64
	if err := database.QueryRow(`SELECT id, mulligan_count FROM games WHERE match_id = ?`, matchID).
		Scan(&firstGameID, &mulliganCount); err != nil {
		t.Fatalf("load first derived game: %v", err)
	}
	if mulliganCount == nil || *mulliganCount != 0 {
		t.Fatalf("mulligan count = %#v, want explicit zero", mulliganCount)
	}

	if err := store.RefreshMatchAnalytics(ctx, matchID); err != nil {
		t.Fatalf("RefreshMatchAnalytics(second): %v", err)
	}
	var secondGameID int64
	if err := database.QueryRow(`SELECT id FROM games WHERE match_id = ?`, matchID).Scan(&secondGameID); err != nil {
		t.Fatalf("load second derived game: %v", err)
	}
	if secondGameID != firstGameID {
		t.Fatalf("game id changed from %d to %d across refresh", firstGameID, secondGameID)
	}
}
