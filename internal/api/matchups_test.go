package api

import (
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func floatPointer(value float64) *float64 {
	return &value
}

func aggroFacts() map[int64]opponentCardFacts {
	facts := make(map[int64]opponentCardFacts)
	// Twelve distinct cheap creatures plus a burn spell and mountains.
	for cardID := int64(1); cardID <= 12; cardID++ {
		facts[cardID] = opponentCardFacts{
			Colors:    []string{"R"},
			ManaValue: floatPointer(2),
			TypeLine:  "Creature — Goblin",
		}
	}
	facts[20] = opponentCardFacts{Colors: []string{"R"}, ManaValue: floatPointer(1), TypeLine: "Instant"}
	facts[30] = opponentCardFacts{Colors: []string{}, TypeLine: "Basic Land — Mountain"}
	return facts
}

func TestClassifyOpponentAggro(t *testing.T) {
	t.Parallel()

	quantities := make(map[int64]int64)
	for cardID := int64(1); cardID <= 12; cardID++ {
		quantities[cardID] = 2
	}
	quantities[20] = 3
	quantities[30] = 8

	out := classifyOpponent(quantities, aggroFacts(), false)
	if out.Archetype != "aggro" {
		t.Fatalf("archetype = %q, want aggro (avg mv %v, creature share %v)",
			out.Archetype, out.AvgManaValue, out.CreatureShare)
	}
	if !out.ColorsKnown || !reflect.DeepEqual(out.Colors, []string{"R"}) {
		t.Fatalf("colors = %v (known=%v), want mono red", out.Colors, out.ColorsKnown)
	}
	if out.Confidence != "high" {
		t.Fatalf("confidence = %q, want high with 35 of 60 cards observed", out.Confidence)
	}
	if out.PctObserved <= 0.5 {
		t.Fatalf("pct observed = %v, want > 0.5", out.PctObserved)
	}
}

func TestClassifyOpponentControlAndUnknown(t *testing.T) {
	t.Parallel()

	facts := map[int64]opponentCardFacts{
		1: {Colors: []string{"U"}, ManaValue: floatPointer(2), TypeLine: "Instant"},
		2: {Colors: []string{"U"}, ManaValue: floatPointer(4), TypeLine: "Instant"},
		3: {Colors: []string{"B"}, ManaValue: floatPointer(3), TypeLine: "Sorcery"},
		4: {Colors: []string{"B"}, ManaValue: floatPointer(6), TypeLine: "Creature — Demon"},
		5: {Colors: []string{"U", "B"}, ManaValue: floatPointer(5), TypeLine: "Enchantment"},
	}
	quantities := map[int64]int64{1: 4, 2: 3, 3: 3, 4: 1, 5: 2}

	out := classifyOpponent(quantities, facts, false)
	if out.Archetype != "control" {
		t.Fatalf("archetype = %q, want control", out.Archetype)
	}

	sparse := classifyOpponent(map[int64]int64{1: 1, 2: 1}, facts, false)
	if sparse.Archetype != "unknown" || sparse.Confidence != "low" {
		t.Fatalf("sparse classification = %q/%q, want unknown/low", sparse.Archetype, sparse.Confidence)
	}
}

func TestLimitedSetCode(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"QuickDraft_TMT_20260313":   "TMT",
		"PremierDraft_TMT_20260303": "TMT",
		"FIN_Quick_Draft":           "FIN",
		"QuickDraft_Y25_20250101":   "Y25",
		"Ladder":                    "",
		"Traditional_Ladder":        "",
		"":                          "",
	}
	for eventName, want := range cases {
		if got := limitedSetCode(eventName); got != want {
			t.Errorf("limitedSetCode(%q) = %q, want %q", eventName, got, want)
		}
	}
}

func TestBuildLimitedMatchupsResponsePoolsBySetAndColorPair(t *testing.T) {
	t.Parallel()

	facts := aggroFacts()
	observed := map[int64]int64{}
	for cardID := int64(1); cardID <= 12; cardID++ {
		observed[cardID] = 1
	}
	matchRows := []db.MatchupMatchRow{
		// Two TMT drafts with different decks pool into one set; the
		// constructed ladder match is excluded entirely.
		{MatchID: 1, DeckID: 7, DeckName: "TMT Draft A", EventName: "QuickDraft_TMT_20260313", Result: "win"},
		{MatchID: 2, DeckID: 8, DeckName: "TMT Draft B", EventName: "PremierDraft_TMT_20260303", Result: "loss"},
		{MatchID: 3, DeckID: 9, DeckName: "Dimir", EventName: "Ladder", Result: "win"},
		{MatchID: 4, DeckID: 10, DeckName: "FIN Draft", EventName: "QuickDraft_FIN_20250619", Result: "win"},
	}
	observedByMatch := map[int64]map[int64]int64{1: observed, 2: observed, 3: observed, 4: observed}
	summaries := map[int64]model.MatchGameSummary{
		1: {Games: model.RecordAgg{Games: 2, Wins: 2}},
		2: {Games: model.RecordAgg{Games: 3, Wins: 1, Losses: 2}},
	}

	// The two TMT matches were played with different own-deck colors; the FIN
	// match has no resolvable decklist.
	ownColors := map[int64]ownDeckColors{
		1: {Colors: []string{"B", "G"}, Known: true},
		2: {Colors: []string{"U", "R"}, Known: true},
	}

	out := buildLimitedMatchupsResponse(matchRows, observedByMatch, facts,
		map[int64]string{}, summaries, map[int64]string{}, ownColors)

	if len(out.Sets) != 2 {
		t.Fatalf("sets = %+v, want TMT and FIN", out.Sets)
	}
	tmt := out.Sets[0]
	if tmt.SetCode != "TMT" {
		t.Fatalf("first set = %q, want TMT (newest first)", tmt.SetCode)
	}
	if tmt.DeckCount != 2 {
		t.Fatalf("TMT deck count = %d, want 2", tmt.DeckCount)
	}
	if tmt.Matches.Wins != 1 || tmt.Matches.Losses != 1 {
		t.Fatalf("TMT record = %+v, want 1-1", tmt.Matches)
	}
	if len(tmt.Rows) != 1 {
		t.Fatalf("TMT rows = %+v, want one mono-red color group", tmt.Rows)
	}
	row := tmt.Rows[0]
	if row.ColorsKey != "R" || row.Archetype != "" {
		t.Fatalf("row colors/archetype = %q/%q, want R with blank archetype", row.ColorsKey, row.Archetype)
	}
	if len(row.MatchRefs) != 2 {
		t.Fatalf("row refs = %d, want both TMT matches pooled across decks", len(row.MatchRefs))
	}
	if row.Games.Games != 5 || row.Games.Wins != 3 {
		t.Fatalf("row games = %+v, want 3-2 over 5", row.Games)
	}
	// Speed labels stay visible per match even though they are not an axis.
	if row.MatchRefs[0].Archetype != "aggro" {
		t.Fatalf("ref archetype = %q, want derived aggro retained", row.MatchRefs[0].Archetype)
	}

	if len(tmt.ColorGroups) != 2 {
		t.Fatalf("TMT color groups = %+v, want BG and UR", tmt.ColorGroups)
	}
	for _, group := range tmt.ColorGroups {
		if !group.ColorsKnown {
			t.Fatalf("group %q colors unknown, want known", group.ColorsKey)
		}
		if group.DeckCount != 1 || group.Matches.Games != 1 {
			t.Fatalf("group %q = %+v, want one deck and one match", group.ColorsKey, group.Matches)
		}
		if len(group.Rows) != 1 || group.Rows[0].ColorsKey != "R" {
			t.Fatalf("group %q rows = %+v, want the mono-red opponent group", group.ColorsKey, group.Rows)
		}
	}
	switch {
	case tmt.ColorGroups[0].ColorsKey == "BG" && tmt.ColorGroups[1].ColorsKey == "UR":
	case tmt.ColorGroups[0].ColorsKey == "UR" && tmt.ColorGroups[1].ColorsKey == "BG":
	default:
		t.Fatalf("group keys = %q/%q, want BG and UR", tmt.ColorGroups[0].ColorsKey, tmt.ColorGroups[1].ColorsKey)
	}

	fin := out.Sets[1]
	if len(fin.ColorGroups) != 1 || fin.ColorGroups[0].ColorsKnown {
		t.Fatalf("FIN color groups = %+v, want a single unknown-colors group", fin.ColorGroups)
	}
}

func TestBuildMatchupsResponseGroupsAndOverrides(t *testing.T) {
	t.Parallel()

	facts := aggroFacts()
	observed := map[int64]int64{}
	for cardID := int64(1); cardID <= 12; cardID++ {
		observed[cardID] = 2
	}
	matchRows := []db.MatchupMatchRow{
		{MatchID: 1, DeckID: 7, DeckName: "Dimir", Result: "win", Opponent: "A"},
		{MatchID: 2, DeckID: 7, DeckName: "Dimir", Result: "loss", Opponent: "B"},
		{MatchID: 3, DeckID: 7, DeckName: "Dimir", Result: "loss", Opponent: "C"},
	}
	observedByMatch := map[int64]map[int64]int64{1: observed, 2: observed, 3: observed}
	summaries := map[int64]model.MatchGameSummary{
		1: {Games: model.RecordAgg{Games: 2, Wins: 2}},
		2: {Games: model.RecordAgg{Games: 3, Wins: 1, Losses: 2}},
		3: {Games: model.RecordAgg{Games: 2, Losses: 2}},
	}
	// Match 3's opponent is manually relabeled combo, splitting it out of the
	// derived aggro row.
	overrides := map[int64]string{3: "combo"}

	out := buildMatchupsResponse(matchRows, observedByMatch, facts,
		map[int64]string{1: "Goblin"}, summaries, overrides)

	if len(out.Decks) != 1 || out.Decks[0].DeckID != 7 {
		t.Fatalf("decks = %+v, want a single deck 7", out.Decks)
	}
	deck := out.Decks[0]
	if deck.Matches.Games != 3 || deck.Matches.Wins != 1 || deck.Matches.Losses != 2 {
		t.Fatalf("deck record = %+v, want 1-2", deck.Matches)
	}
	if len(deck.Rows) != 2 {
		t.Fatalf("rows = %d, want derived aggro row plus manual combo row", len(deck.Rows))
	}

	var aggroRow, comboRow *model.MatchupRow
	for index := range deck.Rows {
		switch deck.Rows[index].Archetype {
		case "aggro":
			aggroRow = &deck.Rows[index]
		case "combo":
			comboRow = &deck.Rows[index]
		}
	}
	if aggroRow == nil || comboRow == nil {
		t.Fatalf("rows = %+v, want aggro and combo", deck.Rows)
	}
	if aggroRow.Matches.Wins != 1 || aggroRow.Matches.Losses != 1 {
		t.Fatalf("aggro row record = %+v, want 1-1", aggroRow.Matches)
	}
	if aggroRow.Games.Games != 5 || aggroRow.Games.Wins != 3 {
		t.Fatalf("aggro row games = %+v, want 3-2 over 5", aggroRow.Games)
	}
	if comboRow.MatchRefs[0].ArchetypeSource != "manual" {
		t.Fatalf("combo ref source = %q, want manual", comboRow.MatchRefs[0].ArchetypeSource)
	}
	if len(aggroRow.TopObservedCards) == 0 || aggroRow.TopObservedCards[0].Matches != 2 {
		t.Fatalf("top observed cards = %+v, want cards seen in both aggro matches", aggroRow.TopObservedCards)
	}
}
