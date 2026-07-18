package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// deckScopeClause returns the SQL filter that limits games/matches to one
// deck (and optionally one immutable deck version) via match_decks, plus the
// matching args. The clause assumes match_decks is joined with alias md.
func deckScopeClause(deckID, deckVersionID int64) (string, []any) {
	if deckVersionID > 0 {
		return "md.deck_id = ? AND md.deck_version_id = ?", []any{deckID, deckVersionID}
	}
	return "md.deck_id = ?", []any{deckID}
}

// resultRecordColumns emits win/loss/draw tallies for rows matching cond.
// Unknown results never enter these columns; callers count them separately.
func resultRecordColumns(cond string) string {
	return fmt.Sprintf(`
		SUM(CASE WHEN %[1]s AND g.result = 'win' THEN 1 ELSE 0 END),
		SUM(CASE WHEN %[1]s AND g.result = 'loss' THEN 1 ELSE 0 END),
		SUM(CASE WHEN %[1]s AND g.result = 'draw' THEN 1 ELSE 0 END)`, cond)
}

type recordScanner struct {
	wins, losses, draws sql.NullInt64
}

func (r *recordScanner) dests() []any {
	return []any{&r.wins, &r.losses, &r.draws}
}

func (r *recordScanner) agg() model.RecordAgg {
	return model.RecordAgg{
		Games:  r.wins.Int64 + r.losses.Int64 + r.draws.Int64,
		Wins:   r.wins.Int64,
		Losses: r.losses.Int64,
		Draws:  r.draws.Int64,
	}
}

func nullableFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	out := value.Float64
	return &out
}

// GetDeckAnalytics aggregates the derived per-game and per-card facts for one
// deck, optionally restricted to a single immutable deck version.
func (s *Store) GetDeckAnalytics(ctx context.Context, deckID, deckVersionID int64) (model.DeckAnalytics, error) {
	out := model.DeckAnalytics{
		DeckID:         deckID,
		HandSizes:      []model.AnalyticsBucket{},
		MulliganCounts: []model.AnalyticsBucket{},
		LandCounts:     []model.AnalyticsBucket{},
		Cards:          []model.DeckCardPerformance{},
	}
	if deckVersionID > 0 {
		out.DeckVersionID = pointerInt64(deckVersionID)
	}
	scope, scopeArgs := deckScopeClause(deckID, deckVersionID)

	if err := s.loadDeckMatchRecord(ctx, &out, scope, scopeArgs); err != nil {
		return out, err
	}
	if err := s.loadDeckGameRecord(ctx, &out, scope, scopeArgs); err != nil {
		return out, err
	}
	var err error
	out.HandSizes, err = s.loadDeckGameBuckets(ctx, "g.kept_hand_size", scope, scopeArgs)
	if err != nil {
		return out, err
	}
	out.MulliganCounts, err = s.loadDeckGameBuckets(ctx, "g.mulligan_count", scope, scopeArgs)
	if err != nil {
		return out, err
	}
	if err := s.loadDeckLandBuckets(ctx, &out, scope, scopeArgs); err != nil {
		return out, err
	}
	out.Cards, err = s.loadDeckCardPerformance(ctx, scope, scopeArgs)
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) loadDeckMatchRecord(ctx context.Context, out *model.DeckAnalytics, scope string, scopeArgs []any) error {
	var record recordScanner
	var matches, matchesWithVersion sql.NullInt64
	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			SUM(CASE WHEN md.deck_version_id IS NOT NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN m.result = 'win' THEN 1 ELSE 0 END),
			SUM(CASE WHEN m.result = 'loss' THEN 1 ELSE 0 END),
			SUM(CASE WHEN m.result = 'draw' THEN 1 ELSE 0 END)
		FROM matches m
		JOIN match_decks md ON md.match_id = m.id
		WHERE %s
	`, scope)
	if err := s.db.QueryRowContext(ctx, query, scopeArgs...).Scan(
		&matches, &matchesWithVersion, &record.wins, &record.losses, &record.draws,
	); err != nil {
		return fmt.Errorf("load deck match record: %w", err)
	}
	out.Coverage.Matches = matches.Int64
	out.Coverage.MatchesWithVersion = matchesWithVersion.Int64
	out.MatchRecord = record.agg()
	return nil
}

func (s *Store) loadDeckGameRecord(ctx context.Context, out *model.DeckAnalytics, scope string, scopeArgs []any) error {
	var total recordScanner
	var gameOne, postBoard, onPlay, onDraw recordScanner
	var gameCount, unknownGames sql.NullInt64
	var withResult, withOpeningHand, withPlayDraw, withCardStats sql.NullInt64
	var averageMulligans sql.NullFloat64

	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			SUM(CASE WHEN g.result NOT IN ('win', 'loss', 'draw') THEN 1 ELSE 0 END),
			%s,
			%s,
			%s,
			%s,
			%s,
			AVG(CASE WHEN g.mulligan_count IS NOT NULL THEN CAST(g.mulligan_count AS REAL) END),
			SUM(CASE WHEN g.result IN ('win', 'loss', 'draw') THEN 1 ELSE 0 END),
			SUM(CASE WHEN g.kept_hand_size IS NOT NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN COALESCE(g.play_draw, '') != '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN EXISTS (SELECT 1 FROM game_card_stats s WHERE s.game_id = g.id) THEN 1 ELSE 0 END)
		FROM games g
		JOIN match_decks md ON md.match_id = g.match_id
		WHERE %s
	`,
		resultRecordColumns("1=1"),
		resultRecordColumns("g.game_number = 1"),
		resultRecordColumns("g.game_number > 1"),
		resultRecordColumns("g.play_draw = 'play'"),
		resultRecordColumns("g.play_draw = 'draw'"),
		scope)

	dests := []any{&gameCount, &unknownGames}
	dests = append(dests, total.dests()...)
	dests = append(dests, gameOne.dests()...)
	dests = append(dests, postBoard.dests()...)
	dests = append(dests, onPlay.dests()...)
	dests = append(dests, onDraw.dests()...)
	dests = append(dests, &averageMulligans, &withResult, &withOpeningHand, &withPlayDraw, &withCardStats)

	if err := s.db.QueryRowContext(ctx, query, scopeArgs...).Scan(dests...); err != nil {
		return fmt.Errorf("load deck game record: %w", err)
	}

	out.GameRecord = total.agg()
	out.UnknownResultGames = unknownGames.Int64
	out.GameOne = gameOne.agg()
	out.PostBoard = postBoard.agg()
	out.OnPlay = onPlay.agg()
	out.OnDraw = onDraw.agg()
	out.AverageMulligans = nullableFloat(averageMulligans)
	out.Coverage.GameCount = gameCount.Int64
	out.Coverage.GamesWithResult = withResult.Int64
	out.Coverage.GamesWithOpeningHand = withOpeningHand.Int64
	out.Coverage.GamesWithPlayDraw = withPlayDraw.Int64
	out.Coverage.GamesWithCardStats = withCardStats.Int64
	return nil
}

// loadDeckGameBuckets groups games by one integer column of the games table
// (kept_hand_size or mulligan_count) and tallies results inside each group.
func (s *Store) loadDeckGameBuckets(ctx context.Context, column, scope string, scopeArgs []any) ([]model.AnalyticsBucket, error) {
	query := fmt.Sprintf(`
		SELECT
			%[1]s,
			%[2]s,
			SUM(CASE WHEN g.result NOT IN ('win', 'loss', 'draw') THEN 1 ELSE 0 END)
		FROM games g
		JOIN match_decks md ON md.match_id = g.match_id
		WHERE %[3]s AND %[1]s IS NOT NULL
		GROUP BY %[1]s
		ORDER BY %[1]s DESC
	`, column, resultRecordColumns("1=1"), scope)

	rows, err := s.db.QueryContext(ctx, query, scopeArgs...)
	if err != nil {
		return nil, fmt.Errorf("load deck game buckets (%s): %w", column, err)
	}
	defer rows.Close()

	buckets := make([]model.AnalyticsBucket, 0)
	for rows.Next() {
		var key int64
		var record recordScanner
		var unknown sql.NullInt64
		dests := append([]any{&key}, record.dests()...)
		dests = append(dests, &unknown)
		if err := rows.Scan(dests...); err != nil {
			return nil, fmt.Errorf("scan deck game bucket (%s): %w", column, err)
		}
		buckets = append(buckets, model.AnalyticsBucket{
			Key:            key,
			Record:         record.agg(),
			UnknownResults: unknown.Int64,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck game buckets (%s): %w", column, err)
	}
	return buckets, nil
}

// loadDeckLandBuckets counts lands in each kept opening hand using the cached
// Scryfall type lines. Hands containing a card whose type line has not been
// resolved yet are reported separately instead of guessed at.
func (s *Store) loadDeckLandBuckets(ctx context.Context, out *model.DeckAnalytics, scope string, scopeArgs []any) error {
	query := fmt.Sprintf(`
		SELECT
			g.result,
			SUM(CASE WHEN ct.type_line IS NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN LOWER(COALESCE(ct.type_line, '')) LIKE '%%land%%' THEN s.opening_kept_copies ELSE 0 END)
		FROM games g
		JOIN match_decks md ON md.match_id = g.match_id
		JOIN game_card_stats s ON s.game_id = g.id AND s.opening_kept_copies > 0
		LEFT JOIN card_types ct ON ct.arena_id = s.card_id
		WHERE %s
		GROUP BY g.id, g.result
	`, scope)

	rows, err := s.db.QueryContext(ctx, query, scopeArgs...)
	if err != nil {
		return fmt.Errorf("load deck land buckets: %w", err)
	}
	defer rows.Close()

	type landTally struct {
		record  model.RecordAgg
		unknown int64
	}
	byLandCount := make(map[int64]*landTally)
	for rows.Next() {
		var result string
		var unknownTypeRows, landCopies int64
		if err := rows.Scan(&result, &unknownTypeRows, &landCopies); err != nil {
			return fmt.Errorf("scan deck land bucket: %w", err)
		}
		if unknownTypeRows > 0 {
			out.LandCountUnknownHands++
			continue
		}
		tally, ok := byLandCount[landCopies]
		if !ok {
			tally = &landTally{}
			byLandCount[landCopies] = tally
		}
		switch result {
		case "win":
			tally.record.Wins++
			tally.record.Games++
		case "loss":
			tally.record.Losses++
			tally.record.Games++
		case "draw":
			tally.record.Draws++
			tally.record.Games++
		default:
			tally.unknown++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate deck land buckets: %w", err)
	}

	landCounts := make([]int64, 0, len(byLandCount))
	for landCount := range byLandCount {
		landCounts = append(landCounts, landCount)
	}
	sort.Slice(landCounts, func(i, j int) bool { return landCounts[i] < landCounts[j] })
	for _, landCount := range landCounts {
		tally := byLandCount[landCount]
		out.LandCounts = append(out.LandCounts, model.AnalyticsBucket{
			Key:            landCount,
			Record:         tally.record,
			UnknownResults: tally.unknown,
		})
	}
	return nil
}

func (s *Store) loadDeckCardPerformance(ctx context.Context, scope string, scopeArgs []any) ([]model.DeckCardPerformance, error) {
	const involved = "(s.opening_kept_copies > 0 OR s.drawn_copies > 0 OR s.played_copies > 0)"
	const inHand = "(s.opening_kept_copies > 0 OR s.drawn_copies > 0)"

	query := fmt.Sprintf(`
		SELECT
			s.card_id,
			COALESCE(cc.name, ''),
			SUM(CASE WHEN %[1]s THEN 1 ELSE 0 END),
			SUM(CASE WHEN %[1]s AND g.result NOT IN ('win', 'loss', 'draw') THEN 1 ELSE 0 END),
			%[3]s,
			%[4]s,
			%[5]s,
			%[6]s,
			%[7]s,
			%[8]s,
			%[9]s,
			%[10]s,
			%[11]s,
			SUM(CASE WHEN s.end_in_hand_copies > 0 THEN 1 ELSE 0 END),
			SUM(CASE WHEN s.mulligan_copies > 0 THEN 1 ELSE 0 END),
			SUM(s.mulligan_copies),
			AVG(CASE WHEN %[1]s AND s.first_seen_turn IS NOT NULL THEN CAST(s.first_seen_turn AS REAL) END),
			AVG(CASE WHEN s.first_played_turn IS NOT NULL THEN CAST(s.first_played_turn AS REAL) END),
			AVG(CASE WHEN %[2]s THEN CAST(s.opening_kept_copies + s.drawn_copies AS REAL) END),
			AVG(CASE WHEN s.played_copies > 0 THEN CAST(s.played_copies AS REAL) END)
		FROM game_card_stats s
		JOIN games g ON g.id = s.game_id
		JOIN match_decks md ON md.match_id = s.match_id
		LEFT JOIN card_catalog cc ON cc.arena_id = s.card_id
		WHERE %[12]s
		GROUP BY s.card_id
		ORDER BY 3 DESC, s.card_id ASC
	`,
		involved,
		inHand,
		resultRecordColumns("s.opening_kept_copies > 0"),
		resultRecordColumns("s.drawn_copies > 0"),
		resultRecordColumns(inHand),
		resultRecordColumns("s.played_copies > 0"),
		resultRecordColumns(inHand+" AND s.played_copies = 0"),
		resultRecordColumns(involved+" AND g.game_number = 1"),
		resultRecordColumns(involved+" AND g.game_number > 1"),
		resultRecordColumns(involved+" AND g.play_draw = 'play'"),
		resultRecordColumns(involved+" AND g.play_draw = 'draw'"),
		scope)

	rows, err := s.db.QueryContext(ctx, query, scopeArgs...)
	if err != nil {
		return nil, fmt.Errorf("load deck card performance: %w", err)
	}
	defer rows.Close()

	cards := make([]model.DeckCardPerformance, 0)
	for rows.Next() {
		var card model.DeckCardPerformance
		var openingHand, drawn, inHandRec, played, notPlayed recordScanner
		var gameOne, postBoard, onPlay, onDraw recordScanner
		var gamesSeen, unknownGames, endedInHand, mulliganGames, mulliganCopies sql.NullInt64
		var avgFirstSeen, avgFirstPlayed, avgCopiesSeen, avgCopiesPlayed sql.NullFloat64

		dests := []any{&card.CardID, &card.CardName, &gamesSeen, &unknownGames}
		dests = append(dests, openingHand.dests()...)
		dests = append(dests, drawn.dests()...)
		dests = append(dests, inHandRec.dests()...)
		dests = append(dests, played.dests()...)
		dests = append(dests, notPlayed.dests()...)
		dests = append(dests, gameOne.dests()...)
		dests = append(dests, postBoard.dests()...)
		dests = append(dests, onPlay.dests()...)
		dests = append(dests, onDraw.dests()...)
		dests = append(dests, &endedInHand, &mulliganGames, &mulliganCopies,
			&avgFirstSeen, &avgFirstPlayed, &avgCopiesSeen, &avgCopiesPlayed)

		if err := rows.Scan(dests...); err != nil {
			return nil, fmt.Errorf("scan deck card performance: %w", err)
		}

		card.GamesSeen = gamesSeen.Int64
		card.UnknownResultGames = unknownGames.Int64
		card.OpeningHand = openingHand.agg()
		card.Drawn = drawn.agg()
		card.InHand = inHandRec.agg()
		card.Played = played.agg()
		card.NotPlayed = notPlayed.agg()
		card.GameOne = gameOne.agg()
		card.PostBoard = postBoard.agg()
		card.OnPlay = onPlay.agg()
		card.OnDraw = onDraw.agg()
		card.EndedInHandGames = endedInHand.Int64
		card.MulliganGames = mulliganGames.Int64
		card.MulliganCopies = mulliganCopies.Int64
		card.AvgFirstSeenTurn = nullableFloat(avgFirstSeen)
		card.AvgFirstPlayedTurn = nullableFloat(avgFirstPlayed)
		card.AvgCopiesSeen = nullableFloat(avgCopiesSeen)
		card.AvgCopiesPlayed = nullableFloat(avgCopiesPlayed)
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck card performance: %w", err)
	}
	return cards, nil
}

// ListDeckKeptHandCardIDs returns the distinct cards that appeared in any kept
// opening hand for the deck, so callers can resolve their type lines before
// computing land distributions.
func (s *Store) ListDeckKeptHandCardIDs(ctx context.Context, deckID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT s.card_id
		FROM game_card_stats s
		JOIN match_decks md ON md.match_id = s.match_id
		WHERE md.deck_id = ? AND s.opening_kept_copies > 0
	`, deckID)
	if err != nil {
		return nil, fmt.Errorf("list deck kept-hand cards: %w", err)
	}
	defer rows.Close()

	cardIDs := make([]int64, 0)
	for rows.Next() {
		var cardID int64
		if err := rows.Scan(&cardID); err != nil {
			return nil, fmt.Errorf("scan deck kept-hand card: %w", err)
		}
		cardIDs = append(cardIDs, cardID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck kept-hand cards: %w", err)
	}
	return cardIDs, nil
}

// DeckAnalyticsGamesQuery selects the games behind one aggregated statistic.
// CardID with a facet narrows to games where that card did the named thing;
// the remaining filters mirror the dashboard's bucket dimensions.
type DeckAnalyticsGamesQuery struct {
	DeckID        int64
	DeckVersionID int64
	CardID        int64
	Facet         string
	KeptHandSize  *int64
	MulliganCount *int64
	GameFilter    string // "one", "post", or ""
	PlayDraw      string // "play", "draw", or ""
	Limit         int64
}

func cardFacetCondition(facet string) (string, error) {
	switch facet {
	case "", "any":
		return "(s.opening_kept_copies > 0 OR s.drawn_copies > 0 OR s.played_copies > 0)", nil
	case "opening":
		return "s.opening_kept_copies > 0", nil
	case "drawn":
		return "s.drawn_copies > 0", nil
	case "played":
		return "s.played_copies > 0", nil
	case "notplayed":
		return "(s.opening_kept_copies > 0 OR s.drawn_copies > 0) AND s.played_copies = 0", nil
	case "stranded":
		return "s.end_in_hand_copies > 0", nil
	case "mulled":
		return "s.mulligan_copies > 0", nil
	default:
		return "", fmt.Errorf("unknown card facet %q", facet)
	}
}

func (s *Store) ListDeckAnalyticsGames(ctx context.Context, q DeckAnalyticsGamesQuery) ([]model.DeckAnalyticsGameRef, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 200
	}
	scope, scopeArgs := deckScopeClause(q.DeckID, q.DeckVersionID)

	// Placeholder order in the final SQL is: JOIN args, then WHERE args (scope
	// first), then LIMIT.
	statColumns := "0, 0, 0, 0, NULL"
	joinStats := ""
	joinArgs := make([]any, 0, 1)
	conditions := []string{scope}
	conditionArgs := make([]any, 0, 4)
	if q.CardID > 0 {
		facetCond, err := cardFacetCondition(q.Facet)
		if err != nil {
			return nil, err
		}
		statColumns = "s.opening_kept_copies, s.drawn_copies, s.played_copies, s.end_in_hand_copies, s.first_played_turn"
		joinStats = "JOIN game_card_stats s ON s.game_id = g.id AND s.card_id = ?"
		joinArgs = append(joinArgs, q.CardID)
		conditions = append(conditions, facetCond)
	} else if strings.TrimSpace(q.Facet) != "" && q.Facet != "any" {
		return nil, fmt.Errorf("card facet %q requires a card id", q.Facet)
	}
	if q.KeptHandSize != nil {
		conditions = append(conditions, "g.kept_hand_size = ?")
		conditionArgs = append(conditionArgs, *q.KeptHandSize)
	}
	if q.MulliganCount != nil {
		conditions = append(conditions, "g.mulligan_count = ?")
		conditionArgs = append(conditionArgs, *q.MulliganCount)
	}
	switch q.GameFilter {
	case "one":
		conditions = append(conditions, "g.game_number = 1")
	case "post":
		conditions = append(conditions, "g.game_number > 1")
	case "":
	default:
		return nil, fmt.Errorf("unknown game filter %q", q.GameFilter)
	}
	switch q.PlayDraw {
	case "play", "draw":
		conditions = append(conditions, "g.play_draw = ?")
		conditionArgs = append(conditionArgs, q.PlayDraw)
	case "":
	default:
		return nil, fmt.Errorf("unknown play/draw filter %q", q.PlayDraw)
	}

	args := make([]any, 0, len(joinArgs)+len(scopeArgs)+len(conditionArgs)+1)
	args = append(args, joinArgs...)
	args = append(args, scopeArgs...)
	args = append(args, conditionArgs...)
	args = append(args, q.Limit)

	query := fmt.Sprintf(`
		SELECT
			g.match_id, g.game_number, g.result, COALESCE(g.play_draw, ''),
			COALESCE(m.started_at, ''), COALESCE(m.opponent_name, ''), COALESCE(m.event_name, ''),
			g.kept_hand_size, g.mulligan_count,
			%s
		FROM games g
		JOIN matches m ON m.id = g.match_id
		JOIN match_decks md ON md.match_id = g.match_id
		%s
		WHERE %s
		ORDER BY COALESCE(m.started_at, m.ended_at, m.updated_at) DESC, g.game_number ASC
		LIMIT ?
	`, statColumns, joinStats, strings.Join(conditions, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list deck analytics games: %w", err)
	}
	defer rows.Close()

	refs := make([]model.DeckAnalyticsGameRef, 0)
	for rows.Next() {
		var ref model.DeckAnalyticsGameRef
		if err := rows.Scan(
			&ref.MatchID, &ref.GameNumber, &ref.Result, &ref.PlayDraw,
			&ref.StartedAt, &ref.Opponent, &ref.EventName,
			&ref.KeptHandSize, &ref.MulliganCount,
			&ref.OpeningKeptCopies, &ref.DrawnCopies, &ref.PlayedCopies,
			&ref.EndInHandCopies, &ref.FirstPlayedTurn,
		); err != nil {
			return nil, fmt.Errorf("scan deck analytics game: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck analytics games: %w", err)
	}
	return refs, nil
}
