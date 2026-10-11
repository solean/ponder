package db

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestDraftAnalyticsScopesPoolFactsWithoutDeckOrSessionRequirements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, firstDeck, firstMatch := setupDeckAnalyticsFixture(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE matches SET event_name = 'QuickDraft_TMT_20260313', result = 'win' WHERE id = ?`, firstMatch)
	// A second deck linked to the same match must not duplicate any fact.
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondDeck, err := store.UpsertDeck(ctx, tx, "second-draft-deck", "QuickDraft_TMT_20260313", "Second", "Draft", "test", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO match_decks (match_id, deck_id, snapshot_reason, created_at) VALUES (?, ?, 'test', '')`, firstMatch, secondDeck)
	matchesBySet := map[string]map[int64]int{"TMT": {firstMatch: 2}, "FIN": {}, "": {}}
	for i, fixture := range []struct {
		event, result           string
		deck, mulligans, copies int64
	}{
		{"PremierDraft_TMT_20260313", "win", secondDeck, 0, 1},
		{"FIN_Quick_Draft", "loss", 0, 2, 3}, // No linked deck or captured pick session.
		{"Draft", "draw", firstDeck, 0, 1},
		{"Sealed_TMT_20260313", "win", firstDeck, 9, 9},
		{"Ladder", "win", secondDeck, 9, 9}, // Draft deck reused in constructed.
	} {
		tx, err := store.BeginTx(ctx)
		if err != nil {
			t.Fatal(err)
		}
		match, err := store.UpsertMatchStart(ctx, tx, fmt.Sprintf("draft-scope-%d", i), fixture.event, 1, "2026-07-03T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE matches SET result = ? WHERE id = ?`, fixture.result, match)
		if fixture.deck != 0 {
			exec(`INSERT INTO match_decks (match_id, deck_id, snapshot_reason, created_at) VALUES (?, ?, 'test', '')`, match, fixture.deck)
		}
		exec(`INSERT INTO games (match_id, game_number, result, play_draw, turn_count, mulligan_count, kept_hand_size, derived_at)
			VALUES (?, 1, ?, 'draw', 6, ?, 7, '')`, match, fixture.result, fixture.mulligans)
		exec(`INSERT INTO game_card_stats (game_id, match_id, card_id, opening_kept_copies, played_copies, first_played_turn)
			SELECT id, match_id, 101, ?, 1, 3 FROM games WHERE match_id = ?`, fixture.copies, match)
		if isDraftDeck("", fixture.event) {
			matchesBySet[model.LimitedSetCode(fixture.event)][match] = 1
		}
	}
	if err := store.UpsertCardTypeLines(ctx, map[int64]string{101: "Basic Land", 102: "Creature", 103: "Creature", 104: "Creature", 105: "Creature", 106: "Creature", 301: "Creature", 302: "Creature", 303: "Creature", 304: "Creature", 305: "Creature", 306: "Creature"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                       string
		set                        *string
		games, wins, losses, draws int64
		mulliganRate, copies       float64
	}{
		{"all", nil, 5, 2, 2, 1, 0.4, 1.75},
		{"TMT", stringPointer("TMT"), 3, 2, 1, 0, 1.0 / 3, 1.5},
		{"FIN", stringPointer("FIN"), 1, 0, 1, 0, 1, 3},
		{"unknown", stringPointer(""), 1, 0, 0, 1, 0, 1},
		{"missing", stringPointer("XYZ"), 0, 0, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := store.LoadDraftAnalyticsScope(ctx, tc.set)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(scope.Sets, []string{"", "FIN", "TMT"}) {
				t.Fatalf("sets = %v", scope.Sets)
			}
			out, err := store.GetDraftAnalytics(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			want := model.RecordAgg{Games: tc.games, Wins: tc.wins, Losses: tc.losses, Draws: tc.draws}
			if out.GameRecord != want || out.Coverage.GameCount != tc.games || out.DeckID != 0 || out.DeckVersionID != nil {
				t.Fatalf("analytics = %+v, want record %+v", out, want)
			}
			expectedMatches := int64(0)
			for set, matches := range matchesBySet {
				if tc.set == nil || *tc.set == set {
					expectedMatches += int64(len(matches))
				}
			}
			if out.Coverage.Matches != expectedMatches || out.MatchRecord.Games != expectedMatches {
				t.Fatalf("match coverage = %+v, record %+v, want %d", out.Coverage, out.MatchRecord, expectedMatches)
			}
			if tc.set == nil && (out.Shape.AvgWinningTurn == nil || *out.Shape.AvgWinningTurn != 2.5 ||
				out.Shape.AvgLosingTurn == nil || *out.Shape.AvgLosingTurn != 2) {
				t.Fatalf("pooled turn averages = %+v", out.Shape)
			}
			if tc.games > 0 {
				if out.MulliganRate == nil || math.Abs(*out.MulliganRate-tc.mulliganRate) > 1e-9 {
					t.Fatalf("mulligan rate = %v, want %v", out.MulliganRate, tc.mulliganRate)
				}
				card := findCardPerformance(t, out.Cards, 101)
				if card.AvgCopiesSeen == nil || math.Abs(*card.AvgCopiesSeen-tc.copies) > 1e-9 || card.Played.Wins != tc.wins {
					t.Fatalf("card facts = %+v, want copies %v and wins %d", card, tc.copies, tc.wins)
				}
				if out.LandCountUnknownHands != 0 || len(out.LandCounts) == 0 {
					t.Fatalf("land distribution = %+v, unknown %d", out.LandCounts, out.LandCountUnknownHands)
				}
			} else if out.MulliganRate != nil || len(out.Cards) != 0 || out.Cards == nil || out.HandSizes == nil || out.Shape.TurnCurve == nil {
				t.Fatalf("empty analytics = %+v", out)
			}
			for _, q := range []DeckAnalyticsGamesQuery{{}, {CardID: 101, Facet: "played"}, {KeptHandSize: pointerInt64(7)}, {GameFilter: "post"}, {PlayDraw: "draw"}, {TurnCount: pointerInt64(3)}} {
				refs, err := store.ListDraftAnalyticsGames(ctx, scope, q)
				if err != nil {
					t.Fatal(err)
				}
				seen := make(map[string]bool)
				for _, ref := range refs {
					allowed := false
					for set, matches := range matchesBySet {
						if (tc.set == nil || *tc.set == set) && matches[ref.MatchID] >= int(ref.GameNumber) {
							allowed = true
						}
					}
					key := fmt.Sprintf("%d/%d", ref.MatchID, ref.GameNumber)
					if !allowed || seen[key] {
						t.Fatalf("leaked or duplicate drilldown game %+v", ref)
					}
					seen[key] = true
				}
				if q == (DeckAnalyticsGamesQuery{}) && int64(len(refs)) != tc.games {
					t.Fatalf("drilldown games = %d, want %d", len(refs), tc.games)
				}
				if q.CardID == 101 {
					want := int64(0)
					for _, card := range out.Cards {
						if card.CardID == 101 {
							want = card.Played.Games
						}
					}
					if int64(len(refs)) != want {
						t.Fatalf("played drilldown = %d, aggregate = %d", len(refs), want)
					}
				}
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestDraftAnalyticsEmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	for _, set := range []*string{nil, stringPointer(""), stringPointer("TMT")} {
		scope, err := store.LoadDraftAnalyticsScope(ctx, set)
		if err != nil {
			t.Fatal(err)
		}
		out, err := store.GetDraftAnalytics(ctx, scope)
		if err != nil || out.Coverage.Matches != 0 || out.GameRecord.Games != 0 || scope.Sets == nil || len(scope.Sets) != 0 {
			t.Fatalf("empty scope = %+v, %+v, %v", scope, out, err)
		}
		refs, err := store.ListDraftAnalyticsGames(ctx, scope, DeckAnalyticsGamesQuery{})
		if err != nil || refs == nil || len(refs) != 0 {
			t.Fatalf("empty refs = %+v, %v", refs, err)
		}
	}
}
