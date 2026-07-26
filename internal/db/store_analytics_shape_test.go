package db

import (
	"context"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func testShapeFrame(gameStateID, turnNumber int64, selfLife, oppLife int64, handCards map[int64]int64) model.MatchReplayFrameRow {
	frame := model.MatchReplayFrameRow{
		GameNumber:        pointerInt64(1),
		GameStateID:       pointerInt64(gameStateID),
		GameStage:         "GameStage_Play",
		TurnNumber:        pointerInt64(turnNumber),
		SelfLifeTotal:     pointerInt64(selfLife),
		OpponentLifeTotal: pointerInt64(oppLife),
		RecordedAt:        "2026-07-18T00:00:00Z",
	}
	for instanceID, cardID := range handCards {
		frame.Objects = append(frame.Objects, model.MatchReplayFrameObjectRow{
			InstanceID: instanceID,
			CardID:     cardID,
			PlayerSide: "self",
			ZoneType:   "hand",
		})
	}
	return frame
}

func TestDeriveGameTurnStatsClassifiesLandsSpellsAndHands(t *testing.T) {
	t.Parallel()

	const (
		landCard    = int64(901) // known land
		spellCard   = int64(902) // known nonland
		unknownCard = int64(903) // no cached type line
	)
	landByCard := map[int64]bool{landCard: true, spellCard: false}

	frames := []model.MatchReplayFrameRow{
		// Turn 1: holds a land, a spell, and an unknown card.
		testShapeFrame(1, 1, 20, 20, map[int64]int64{1: landCard, 2: spellCard, 3: unknownCard}),
		// Turn 2 (last frame wins): only known nonland cards left in hand.
		testShapeFrame(2, 2, 20, 18, map[int64]int64{2: spellCard}),
		testShapeFrame(3, 2, 17, 18, map[int64]int64{2: spellCard}),
		// Turn 3: only the unknown card, so land-in-hand is ambiguous.
		testShapeFrame(4, 3, 17, 15, map[int64]int64{3: unknownCard}),
	}
	plays := []selfCardPlay{
		{GameNumber: 1, TurnNumber: 1, CardID: landCard, Zone: "battlefield"},
		{GameNumber: 1, TurnNumber: 2, CardID: spellCard, Zone: "stack"},
		// A modal card cast as a spell must not count as a land drop.
		{GameNumber: 1, TurnNumber: 2, CardID: landCard, Zone: "stack"},
		// Unknown type appearing battlefield-first counts as a land drop.
		{GameNumber: 1, TurnNumber: 3, CardID: unknownCard, Zone: "battlefield"},
		// A known nonland put straight onto the battlefield counts as neither.
		{GameNumber: 1, TurnNumber: 3, CardID: spellCard, Zone: "battlefield"},
	}

	stats := deriveGameTurnStats(frames, plays, "play", landByCard)
	if len(stats) != 3 {
		t.Fatalf("turn stats = %d rows, want 3", len(stats))
	}

	turn1, turn2, turn3 := stats[0], stats[1], stats[2]
	if turn1.LandsPlayed != 1 || turn1.SpellsCast != 0 {
		t.Fatalf("turn 1 lands=%d spells=%d, want 1 and 0", turn1.LandsPlayed, turn1.SpellsCast)
	}
	if turn1.LandInHand == nil || !*turn1.LandInHand {
		t.Fatalf("turn 1 land in hand = %#v, want true", turn1.LandInHand)
	}
	if turn1.IsPlayerTurn == nil || !*turn1.IsPlayerTurn {
		t.Fatalf("turn 1 is player turn = %#v, want true (on the play)", turn1.IsPlayerTurn)
	}

	if turn2.LandsPlayed != 0 || turn2.SpellsCast != 2 {
		t.Fatalf("turn 2 lands=%d spells=%d, want 0 and 2", turn2.LandsPlayed, turn2.SpellsCast)
	}
	if turn2.LandInHand == nil || *turn2.LandInHand {
		t.Fatalf("turn 2 land in hand = %#v, want false", turn2.LandInHand)
	}
	if turn2.SelfLife == nil || *turn2.SelfLife != 17 {
		t.Fatalf("turn 2 self life = %#v, want 17 from the turn's last frame", turn2.SelfLife)
	}
	if turn2.IsPlayerTurn == nil || *turn2.IsPlayerTurn {
		t.Fatalf("turn 2 is player turn = %#v, want false", turn2.IsPlayerTurn)
	}

	if turn3.LandsPlayed != 1 || turn3.SpellsCast != 0 {
		t.Fatalf("turn 3 lands=%d spells=%d, want 1 and 0", turn3.LandsPlayed, turn3.SpellsCast)
	}
	if turn3.LandInHand != nil {
		t.Fatalf("turn 3 land in hand = %#v, want nil (unknown type line)", turn3.LandInHand)
	}
	if turn3.SelfHandSize == nil || *turn3.SelfHandSize != 1 {
		t.Fatalf("turn 3 hand size = %#v, want 1", turn3.SelfHandSize)
	}
}

func TestDeriveOpponentCardCopiesUsesSimultaneousMaximum(t *testing.T) {
	t.Parallel()

	const (
		shiko   = int64(95749) // recast repeatedly: many instance ids, 1 at a time
		bat     = int64(200)   // two copies coexist once
		myCard  = int64(300)   // self card, never counted
		tokenID = int64(400)   // opponent token, never counted
	)
	const spell = int64(500) // only ever seen dying to the graveyard
	oppObject := func(instanceID, cardID int64, zone string, token bool) model.MatchReplayFrameObjectRow {
		return model.MatchReplayFrameObjectRow{
			InstanceID: instanceID,
			CardID:     cardID,
			PlayerSide: "opponent",
			ZoneType:   zone,
			IsToken:    token,
		}
	}
	frames := []model.MatchReplayFrameRow{
		{Objects: []model.MatchReplayFrameObjectRow{
			oppObject(1, shiko, "battlefield", false),
			{InstanceID: 9, CardID: myCard, PlayerSide: "self", ZoneType: "battlefield"},
		}},
		// Same Shiko recast under a new instance id — still only one at a time.
		{Objects: []model.MatchReplayFrameObjectRow{oppObject(2, shiko, "battlefield", false)}},
		// A recast Shiko leaves stale instances scattered across transit zones;
		// none of these count because they are not clean zones.
		{Objects: []model.MatchReplayFrameObjectRow{
			oppObject(3, shiko, "battlefield", false),
			oppObject(4, shiko, "graveyard", false),
			oppObject(5, shiko, "limbo", false),
			oppObject(6, shiko, "exile", false),
			// Two Bats on the battlefield at once establishes at least two copies.
			oppObject(11, bat, "battlefield", false),
			oppObject(12, bat, "battlefield", false),
			oppObject(99, tokenID, "battlefield", true),
			// A spell only ever seen in the graveyard still floors to one.
			oppObject(20, spell, "graveyard", false),
		}},
	}

	copies := deriveOpponentCardCopies(frames)
	if copies[shiko] != 1 {
		t.Fatalf("shiko copies = %d, want 1 (only one battlefield instance at a time)", copies[shiko])
	}
	if copies[spell] != 1 {
		t.Fatalf("graveyard-only spell copies = %d, want 1 (floor)", copies[spell])
	}
	if copies[bat] != 2 {
		t.Fatalf("bat copies = %d, want 2 (two coexisted)", copies[bat])
	}
	if _, ok := copies[myCard]; ok {
		t.Fatalf("self card must not be counted: %v", copies)
	}
	if _, ok := copies[tokenID]; ok {
		t.Fatalf("opponent token must not be counted: %v", copies)
	}
}

func TestDeriveGameFlagsSkipsFinalTurnAndUnknownTurns(t *testing.T) {
	t.Parallel()

	held := pointerBool(true)
	own := pointerBool(true)
	opp := pointerBool(false)
	stats := []model.GameTurnStatRow{
		{TurnNumber: 1, IsPlayerTurn: own, LandsPlayed: 1, LandInHand: held},
		// Missed drop: own turn, no land played, land in hand.
		{TurnNumber: 3, IsPlayerTurn: own, LandsPlayed: 0, LandInHand: held},
		// Opponent turn never flags.
		{TurnNumber: 4, IsPlayerTurn: opp, LandsPlayed: 0, LandInHand: held},
		// Ambiguous hand never flags.
		{TurnNumber: 5, IsPlayerTurn: own, LandsPlayed: 0, LandInHand: nil},
		// Final turn never flags even when a land was held.
		{TurnNumber: 7, IsPlayerTurn: own, LandsPlayed: 0, LandInHand: held},
	}

	flags := deriveGameFlags(stats)
	if len(flags) != 1 {
		t.Fatalf("flags = %#v, want exactly one missed land drop", flags)
	}
	if flags[0].Flag != flagMissedLandDrop || flags[0].TurnNumber == nil || *flags[0].TurnNumber != 3 {
		t.Fatalf("flag = %#v, want missed_land_drop on turn 3", flags[0])
	}
	if flags[0].Confidence != "heuristic" {
		t.Fatalf("flag confidence = %q, want heuristic", flags[0].Confidence)
	}
}

func TestRefreshMatchAnalyticsStoresTurnStatsAndMinLife(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)

	const (
		landCard  = int64(901)
		spellCard = int64(902)
	)
	if err := store.UpsertCardTypeLines(ctx, map[int64]string{
		landCard:  "Basic Land — Forest",
		spellCard: "Creature — Bear",
	}); err != nil {
		t.Fatalf("UpsertCardTypeLines: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	matchID, err := store.UpsertMatchStart(ctx, tx, "shape-match", "Ladder", 1, "2026-07-18T00:00:00Z")
	if err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	hand := []model.MatchReplayFrameObjectRow{
		{InstanceID: 1, CardID: landCard, OwnerSeatID: pointerInt64(1), PlayerSide: "self", ZoneType: "hand"},
		{InstanceID: 2, CardID: spellCard, OwnerSeatID: pointerInt64(1), PlayerSide: "self", ZoneType: "hand"},
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "shape-match", 1, 1, 0, 1,
		"GameStateType_Full", "GameStage_Play", "main1", "", "", "2026-07-18T00:00:01Z", "test",
		[]byte(`{"1":20,"2":20}`), nil, nil, hand); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(turn 1): %v", err)
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "shape-match", 1, 2, 1, 2,
		"GameStateType_Full", "GameStage_Play", "main1", "", "", "2026-07-18T00:00:02Z", "test",
		[]byte(`{"1":4,"2":13}`), nil, nil, hand); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(turn 2): %v", err)
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "shape-match", 1, 3, 2, 3,
		"GameStateType_Full", "GameStage_Play", "main1", "", "", "2026-07-18T00:00:03Z", "test",
		[]byte(`{"1":9,"2":13}`), nil, nil, hand); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(turn 3): %v", err)
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "shape-match", 1, 4, 3, 4,
		"GameStateType_Full", "GameStage_Play", "main1", "self", "Concede", "2026-07-18T00:00:04Z", "test",
		[]byte(`{"1":9,"2":13}`), nil, nil, hand); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame(turn 4): %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "shape-match", 1, 10, landCard, 1, 1,
		"main1", "battlefield", "2026-07-18T00:00:01Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(land): %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "shape-match", 1, 11, spellCard, 1, 3,
		"main1", "stack", "2026-07-18T00:00:03Z", "test"); err != nil {
		t.Fatalf("UpsertMatchCardPlay(spell): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := store.RefreshMatchAnalytics(ctx, matchID); err != nil {
		t.Fatalf("RefreshMatchAnalytics: %v", err)
	}

	games, err := store.ListMatchGames(ctx, matchID)
	if err != nil {
		t.Fatalf("ListMatchGames: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("games = %d, want 1", len(games))
	}
	game := games[0]
	if game.TurnCount == nil || *game.TurnCount != 2 {
		t.Fatalf("display turn count = %#v, want 2 full turns", game.TurnCount)
	}
	if game.MinSelfLife == nil || *game.MinSelfLife != 4 {
		t.Fatalf("min self life = %#v, want 4", game.MinSelfLife)
	}
	if game.MinOpponentLife == nil || *game.MinOpponentLife != 13 {
		t.Fatalf("min opponent life = %#v, want 13", game.MinOpponentLife)
	}
	if len(game.TurnStats) != 4 {
		t.Fatalf("turn stats = %d rows, want 4", len(game.TurnStats))
	}
	if game.TurnStats[0].LandsPlayed != 1 || game.TurnStats[2].SpellsCast != 1 {
		t.Fatalf("turn stats = %#v, want land drop on turn 1 and spell on turn 3", game.TurnStats)
	}
	// The turn-1 land play puts the player on the play, so turn 3 is an own
	// turn; it ended holding a land with none played and is not the final
	// turn, so it must flag for review.
	if len(game.Flags) != 1 || game.Flags[0].Flag != flagMissedLandDrop ||
		game.Flags[0].TurnNumber == nil || *game.Flags[0].TurnNumber != 3 {
		t.Fatalf("flags = %#v, want missed_land_drop on turn 3", game.Flags)
	}

	coverage, err := store.GetMatchAnalyticsCoverage(ctx, matchID)
	if err != nil {
		t.Fatalf("GetMatchAnalyticsCoverage: %v", err)
	}
	if coverage.GamesWithTurnStats != 1 {
		t.Fatalf("games with turn stats = %d, want 1", coverage.GamesWithTurnStats)
	}
}

func TestDeckAnalyticsShapeAggregatesMissedDropsAndCurves(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)

	const (
		landCard  = int64(901)
		spellCard = int64(902)
	)
	if err := store.UpsertCardTypeLines(ctx, map[int64]string{
		landCard:  "Basic Land — Forest",
		spellCard: "Creature — Bear",
	}); err != nil {
		t.Fatalf("UpsertCardTypeLines: %v", err)
	}

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	deckID, err := store.UpsertDeck(ctx, tx, "shape-deck", "Ladder", "Shape", "Standard", "test",
		"2026-07-01T00:00:00Z", []DeckCard{{Section: "main", CardID: landCard, Quantity: 20}})
	if err != nil {
		t.Fatalf("UpsertDeck: %v", err)
	}

	// Both games run four Arena player-turns (two full turns) with the player on the play (own turns 1 and
	// 3). Match 1 (win) drops a land on both own turns; match 2 (loss) skips
	// the turn-3 drop while holding a land, which is a non-final turn.
	type matchSeed struct {
		arenaID   string
		result    string
		landTurns []int64
	}
	seeds := []matchSeed{
		{arenaID: "shape-m1", result: "win", landTurns: []int64{1, 3}},
		{arenaID: "shape-m2", result: "loss", landTurns: []int64{1}},
	}
	for index, seed := range seeds {
		matchID, err := store.UpsertMatchStart(ctx, tx, seed.arenaID, "Ladder", 1, "2026-07-18T00:00:00Z")
		if err != nil {
			t.Fatalf("UpsertMatchStart(%s): %v", seed.arenaID, err)
		}
		winningTeam := int64(1)
		if seed.result == "loss" {
			winningTeam = 2
		}
		if _, _, _, err := store.UpdateMatchEnd(ctx, tx, seed.arenaID, 1, winningTeam, 3, 300, "", "2026-07-18T00:10:00Z"); err != nil {
			t.Fatalf("UpdateMatchEnd(%s): %v", seed.arenaID, err)
		}
		if ok, err := store.LinkMatchToDeckByArenaDeckID(ctx, tx, seed.arenaID, "shape-deck", "event_deck"); err != nil || !ok {
			t.Fatalf("LinkMatchToDeckByArenaDeckID(%s) = %v, %v", seed.arenaID, ok, err)
		}
		hand := []model.MatchReplayFrameObjectRow{
			{InstanceID: 1, CardID: landCard, OwnerSeatID: pointerInt64(1), PlayerSide: "self", ZoneType: "hand"},
		}
		for turn := int64(1); turn <= 4; turn++ {
			if _, err := store.ReplaceMatchReplayFrame(ctx, tx, seed.arenaID, 1, turn, turn-1, turn,
				"GameStateType_Full", "GameStage_Play", "main1", "", "", "2026-07-18T00:00:01Z", "test",
				[]byte(`{"1":20,"2":20}`), nil, nil, hand); err != nil {
				t.Fatalf("ReplaceMatchReplayFrame(%s turn %d): %v", seed.arenaID, turn, err)
			}
		}
		for playIndex, turn := range seed.landTurns {
			if err := store.UpsertMatchCardPlay(ctx, tx, seed.arenaID, 1, int64(100+playIndex), landCard, 1, turn,
				"main1", "battlefield", "2026-07-18T00:00:01Z", "test"); err != nil {
				t.Fatalf("UpsertMatchCardPlay(%s): %v", seed.arenaID, err)
			}
		}
		for playIndex, turn := range []int64{2, 4} {
			if err := store.UpsertMatchCardPlay(ctx, tx, seed.arenaID, 1, int64(200+playIndex), spellCard, 1, turn,
				"main1", "stack", "2026-07-18T00:00:02Z", "test"); err != nil {
				t.Fatalf("UpsertMatchCardPlay(%s spell): %v", seed.arenaID, err)
			}
		}
		_ = index
		_ = matchID
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := store.RefreshPendingMatchAnalytics(ctx); err != nil {
		t.Fatalf("RefreshPendingMatchAnalytics: %v", err)
	}

	analytics, err := store.GetDeckAnalytics(ctx, deckID, 0)
	if err != nil {
		t.Fatalf("GetDeckAnalytics: %v", err)
	}
	shape := analytics.Shape
	if shape.CleanDropGames.Wins != 1 || shape.CleanDropGames.Games != 1 {
		t.Fatalf("clean drop games = %#v, want the winning game only", shape.CleanDropGames)
	}
	if shape.MissedDropGames.Losses != 1 || shape.MissedDropGames.Games != 1 {
		t.Fatalf("missed drop games = %#v, want the losing game only", shape.MissedDropGames)
	}
	if analytics.Coverage.GamesWithTurnStats != 2 || analytics.Coverage.GamesWithLandJudged != 2 {
		t.Fatalf("coverage = %#v, want 2 games with turn stats and 2 judged", analytics.Coverage)
	}
	if len(shape.GameLengths) == 0 || shape.GameLengths[0].Key != 2 {
		t.Fatalf("game lengths = %#v, want a bucket at 2 full turns", shape.GameLengths)
	}
	if shape.AvgWinningTurn == nil || *shape.AvgWinningTurn != 2 ||
		shape.AvgLosingTurn == nil || *shape.AvgLosingTurn != 2 {
		t.Fatalf("average ending turns = win %#v loss %#v, want 2 and 2",
			shape.AvgWinningTurn, shape.AvgLosingTurn)
	}
	if len(shape.TurnCurve) != 2 {
		t.Fatalf("turn curve = %d points, want 2 full turns", len(shape.TurnCurve))
	}
	last := shape.TurnCurve[1]
	if last.Turn != 2 {
		t.Fatalf("last turn curve point = %d, want full turn 2", last.Turn)
	}
	if last.AvgLandsWins == nil || *last.AvgLandsWins != 2 {
		t.Fatalf("avg lands in wins at turn 2 = %#v, want 2", last.AvgLandsWins)
	}
	if last.AvgLandsLosses == nil || *last.AvgLandsLosses != 1 {
		t.Fatalf("avg lands in losses at turn 2 = %#v, want 1", last.AvgLandsLosses)
	}
	if last.AvgSpellsWins == nil || *last.AvgSpellsWins != 1 ||
		last.AvgSpellsLosses == nil || *last.AvgSpellsLosses != 1 {
		t.Fatalf("avg spells on full turn 2 = win %#v loss %#v, want 1 and 1",
			last.AvgSpellsWins, last.AvgSpellsLosses)
	}

	missed, err := store.ListDeckAnalyticsGames(ctx, DeckAnalyticsGamesQuery{DeckID: deckID, LandDrops: "missed"})
	if err != nil {
		t.Fatalf("ListDeckAnalyticsGames(missed): %v", err)
	}
	if len(missed) != 1 || missed[0].Result != "loss" {
		t.Fatalf("missed drill-down = %#v, want the losing game", missed)
	}
	clean, err := store.ListDeckAnalyticsGames(ctx, DeckAnalyticsGamesQuery{DeckID: deckID, LandDrops: "clean"})
	if err != nil {
		t.Fatalf("ListDeckAnalyticsGames(clean): %v", err)
	}
	if len(clean) != 1 || clean[0].Result != "win" {
		t.Fatalf("clean drill-down = %#v, want the winning game", clean)
	}
}
