package db

import (
	"context"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func testHandObjects(cards map[int64]int64) []model.MatchReplayFrameObjectRow {
	out := make([]model.MatchReplayFrameObjectRow, 0, len(cards))
	for instanceID, cardID := range cards {
		out = append(out, model.MatchReplayFrameObjectRow{
			InstanceID:  instanceID,
			CardID:      cardID,
			OwnerSeatID: pointerInt64(1),
			PlayerSide:  "self",
			ZoneType:    "hand",
		})
	}
	return out
}

// Builds one deck-linked BO3 match: game one is a 7-card keep that wins on the
// play, game two is a mulligan to six that loses. Card 101 has two copies
// kept, one played on turn one, one stranded; card 107 is drawn on turn two
// and never played.
func setupDeckAnalyticsFixture(t *testing.T) (*Store, int64, int64) {
	t.Helper()
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
	deckID, err := store.UpsertDeck(ctx, tx, "deck-analytics", "Ladder", "Analytics Test", "Standard",
		"test", "2026-07-01T00:00:00Z", []DeckCard{{Section: "main", CardID: 101, Quantity: 4}})
	if err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}
	matchID, err := store.UpsertMatchStart(ctx, tx, "match-analytics", "Ladder", 1, "2026-07-02T00:00:00Z")
	if err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if linked, err := store.LinkMatchToDeckByArenaDeckID(ctx, tx, "match-analytics", "deck-analytics", "event_deck"); err != nil || !linked {
		t.Fatalf("LinkMatchToDeckByArenaDeckID = %v, %v", linked, err)
	}

	gameOneKeep := map[int64]int64{1: 101, 2: 101, 3: 102, 4: 103, 5: 104, 6: 105, 7: 106}
	// Turn two: one copy of 101 (instance 1) has been played, card 107 drawn.
	afterPlayAndDraw := map[int64]int64{2: 101, 3: 102, 4: 103, 5: 104, 6: 105, 7: 106, 8: 107}
	gameOneEnd := map[int64]int64{2: 101, 8: 107}
	frames := []struct {
		gameNumber, gameStateID, turnNumber int64
		stage, winningSide                  string
		hand                                map[int64]int64
	}{
		{1, 1, 0, "GameStage_Start", "", gameOneKeep},
		{1, 2, 1, "GameStage_Play", "", gameOneKeep},
		{1, 3, 2, "GameStage_Play", "", afterPlayAndDraw},
		{1, 4, 3, "GameStage_Play", "self", gameOneEnd},
		{2, 10, 0, "GameStage_Start", "", map[int64]int64{21: 201, 22: 202, 23: 203, 24: 204, 25: 205, 26: 206, 27: 207}},
		{2, 11, 0, "GameStage_Start", "", map[int64]int64{31: 301, 32: 302, 33: 303, 34: 304, 35: 305, 36: 306, 37: 307}},
		{2, 12, 1, "GameStage_Play", "opponent", map[int64]int64{31: 301, 32: 302, 33: 303, 34: 304, 35: 305, 36: 306}},
	}
	for _, frame := range frames {
		if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "match-analytics", frame.gameNumber,
			frame.gameStateID, 0, frame.turnNumber, "GameStateType_Full", frame.stage, "",
			frame.winningSide, "", "2026-07-02T00:00:01Z", "test", nil, nil, nil,
			testHandObjects(frame.hand)); err != nil {
			t.Fatalf("ReplaceMatchReplayFrame(%d/%d): %v", frame.gameNumber, frame.gameStateID, err)
		}
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "match-analytics", 1, 1, 101, 1, 1,
		"main1", "battlefield", "2026-07-02T00:00:02Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(self): %v", err)
	}
	// Opponent plays must not contribute to the player's card stats.
	if err := store.UpsertMatchCardPlay(ctx, tx, "match-analytics", 1, 100, 999, 2, 2,
		"main1", "battlefield", "2026-07-02T00:00:03Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(opponent): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := store.RefreshMatchAnalytics(ctx, matchID); err != nil {
		t.Fatalf("RefreshMatchAnalytics: %v", err)
	}
	return store, deckID, matchID
}

func findCardPerformance(t *testing.T, cards []model.DeckCardPerformance, cardID int64) model.DeckCardPerformance {
	t.Helper()
	for _, card := range cards {
		if card.CardID == cardID {
			return card
		}
	}
	t.Fatalf("card %d missing from performance rows", cardID)
	return model.DeckCardPerformance{}
}

func TestDeckAnalyticsAggregatesCardAndHandFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, deckID, _ := setupDeckAnalyticsFixture(t)

	out, err := store.GetDeckAnalytics(ctx, deckID, 0)
	if err != nil {
		t.Fatalf("GetDeckAnalytics: %v", err)
	}

	if out.GameRecord.Games != 2 || out.GameRecord.Wins != 1 || out.GameRecord.Losses != 1 {
		t.Fatalf("game record = %+v, want 1-1 over 2 games", out.GameRecord)
	}
	if out.GameOne.Wins != 1 || out.GameOne.Losses != 0 || out.PostBoard.Losses != 1 {
		t.Fatalf("splits = game1 %+v post %+v, want game1 1-0 and post 0-1", out.GameOne, out.PostBoard)
	}
	if out.OnPlay.Wins != 1 {
		t.Fatalf("on-play record = %+v, want the game-one win", out.OnPlay)
	}
	if out.AverageMulligans == nil || *out.AverageMulligans != 0.5 {
		t.Fatalf("average mulligans = %#v, want 0.5", out.AverageMulligans)
	}
	if out.Coverage.GameCount != 2 || out.Coverage.GamesWithCardStats != 2 || out.Coverage.GamesWithOpeningHand != 2 {
		t.Fatalf("coverage = %+v, want 2 games with card stats and opening hands", out.Coverage)
	}

	sizeBuckets := map[int64]model.AnalyticsBucket{}
	for _, bucket := range out.HandSizes {
		sizeBuckets[bucket.Key] = bucket
	}
	if sizeBuckets[7].Record.Wins != 1 || sizeBuckets[6].Record.Losses != 1 {
		t.Fatalf("hand size buckets = %+v, want a 7-card win and a 6-card loss", out.HandSizes)
	}
	mullBuckets := map[int64]model.AnalyticsBucket{}
	for _, bucket := range out.MulliganCounts {
		mullBuckets[bucket.Key] = bucket
	}
	if mullBuckets[0].Record.Wins != 1 || mullBuckets[1].Record.Losses != 1 {
		t.Fatalf("mulligan buckets = %+v, want no-mull win and one-mull loss", out.MulliganCounts)
	}

	card101 := findCardPerformance(t, out.Cards, 101)
	if card101.OpeningHand.Games != 1 || card101.OpeningHand.Wins != 1 {
		t.Fatalf("card 101 opening hand = %+v, want one winning keep", card101.OpeningHand)
	}
	if card101.Played.Games != 1 || card101.Played.Wins != 1 {
		t.Fatalf("card 101 played = %+v, want one winning play", card101.Played)
	}
	if card101.EndedInHandGames != 1 {
		t.Fatalf("card 101 stranded games = %d, want 1", card101.EndedInHandGames)
	}
	if card101.AvgCopiesSeen == nil || *card101.AvgCopiesSeen != 2 {
		t.Fatalf("card 101 avg copies seen = %#v, want 2", card101.AvgCopiesSeen)
	}
	if card101.AvgFirstPlayedTurn == nil || *card101.AvgFirstPlayedTurn != 1 {
		t.Fatalf("card 101 avg first played turn = %#v, want 1", card101.AvgFirstPlayedTurn)
	}
	if card101.AvgFirstSeenTurn == nil || *card101.AvgFirstSeenTurn != 0 {
		t.Fatalf("card 101 avg first seen turn = %#v, want opening hand (0)", card101.AvgFirstSeenTurn)
	}

	card107 := findCardPerformance(t, out.Cards, 107)
	if card107.Drawn.Games != 1 || card107.Drawn.Wins != 1 {
		t.Fatalf("card 107 drawn = %+v, want one winning draw", card107.Drawn)
	}
	if card107.OpeningHand.Games != 0 || card107.NotPlayed.Games != 1 {
		t.Fatalf("card 107 = opening %+v notPlayed %+v, want drawn-only and never played", card107.OpeningHand, card107.NotPlayed)
	}
	if card107.AvgFirstSeenTurn == nil || *card107.AvgFirstSeenTurn != 2 {
		t.Fatalf("card 107 avg first seen turn = %#v, want 2", card107.AvgFirstSeenTurn)
	}

	card201 := findCardPerformance(t, out.Cards, 201)
	if card201.MulliganGames != 1 || card201.MulliganCopies != 1 || card201.GamesSeen != 0 {
		t.Fatalf("card 201 = %+v, want mulligan-only involvement", card201)
	}

	// Opponent's played card never becomes a player card stat.
	for _, card := range out.Cards {
		if card.CardID == 999 {
			t.Fatalf("opponent card 999 leaked into deck card performance")
		}
	}
}

func TestDeckAnalyticsLandBucketsUseCachedTypeLines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, deckID, _ := setupDeckAnalyticsFixture(t)

	out, err := store.GetDeckAnalytics(ctx, deckID, 0)
	if err != nil {
		t.Fatalf("GetDeckAnalytics(before types): %v", err)
	}
	if out.LandCountUnknownHands != 2 || len(out.LandCounts) != 0 {
		t.Fatalf("land buckets before types = %+v unknown=%d, want everything unknown",
			out.LandCounts, out.LandCountUnknownHands)
	}

	typeLines := map[int64]string{
		101: "Instant", 102: "Basic Land — Island", 103: "Basic Land — Island",
		104: "Creature — Bird", 105: "Sorcery", 106: "Land",
		301: "Basic Land — Swamp", 302: "Instant", 303: "Instant",
		304: "Instant", 305: "Instant", 306: "Instant",
	}
	if err := store.UpsertCardTypeLines(ctx, typeLines); err != nil {
		t.Fatalf("UpsertCardTypeLines: %v", err)
	}

	out, err = store.GetDeckAnalytics(ctx, deckID, 0)
	if err != nil {
		t.Fatalf("GetDeckAnalytics(after types): %v", err)
	}
	if out.LandCountUnknownHands != 0 {
		t.Fatalf("unknown land hands = %d after resolving types, want 0", out.LandCountUnknownHands)
	}
	landBuckets := map[int64]model.AnalyticsBucket{}
	for _, bucket := range out.LandCounts {
		landBuckets[bucket.Key] = bucket
	}
	if landBuckets[3].Record.Wins != 1 {
		t.Fatalf("land buckets = %+v, want the three-land keep recorded as a win", out.LandCounts)
	}
	if landBuckets[1].Record.Losses != 1 {
		t.Fatalf("land buckets = %+v, want the one-land keep recorded as a loss", out.LandCounts)
	}
}

func TestListDeckAnalyticsGamesFacetsAndFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, deckID, matchID := setupDeckAnalyticsFixture(t)

	played, err := store.ListDeckAnalyticsGames(ctx, DeckAnalyticsGamesQuery{
		DeckID: deckID, CardID: 101, Facet: "played",
	})
	if err != nil {
		t.Fatalf("ListDeckAnalyticsGames(played): %v", err)
	}
	if len(played) != 1 || played[0].MatchID != matchID || played[0].GameNumber != 1 {
		t.Fatalf("played drill-down = %+v, want game one of the fixture match", played)
	}
	if played[0].PlayedCopies != 1 || played[0].FirstPlayedTurn == nil || *played[0].FirstPlayedTurn != 1 {
		t.Fatalf("played drill-down copies = %+v, want one copy on turn one", played[0])
	}

	drawn, err := store.ListDeckAnalyticsGames(ctx, DeckAnalyticsGamesQuery{
		DeckID: deckID, CardID: 101, Facet: "drawn",
	})
	if err != nil {
		t.Fatalf("ListDeckAnalyticsGames(drawn): %v", err)
	}
	if len(drawn) != 0 {
		t.Fatalf("card 101 drawn drill-down = %+v, want empty (both copies were kept)", drawn)
	}

	sixCardKeeps, err := store.ListDeckAnalyticsGames(ctx, DeckAnalyticsGamesQuery{
		DeckID: deckID, KeptHandSize: pointerInt64(6),
	})
	if err != nil {
		t.Fatalf("ListDeckAnalyticsGames(keptSize): %v", err)
	}
	if len(sixCardKeeps) != 1 || sixCardKeeps[0].GameNumber != 2 || sixCardKeeps[0].Result != "loss" {
		t.Fatalf("six-card keeps = %+v, want the game-two loss", sixCardKeeps)
	}

	if _, err := store.ListDeckAnalyticsGames(ctx, DeckAnalyticsGamesQuery{
		DeckID: deckID, Facet: "played",
	}); err == nil {
		t.Fatalf("facet without card id should be rejected")
	}
}
