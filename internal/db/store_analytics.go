package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/solean/ponder/internal/model"
)

type derivedOpeningCard struct {
	CardID   int64
	Quantity int64
	Kept     bool
}

type derivedOpeningHand struct {
	AttemptNumber   int64
	Decision        string
	OfferedHandSize int64
	KeptHandSize    *int64
	ObservedAt      string
	Cards           []derivedOpeningCard
}

type derivedGame struct {
	GameNumber            int64
	Result                string
	WinReason             string
	PlayDraw              string
	StartedAt             string
	EndedAt               string
	TurnCount             *int64
	OpeningLifeTotal      *int64
	EndingLifeTotal       *int64
	MulliganCount         *int64
	KeptHandSize          *int64
	ResultSource          string
	ResultConfidence      string
	PlayDrawSource        string
	PlayDrawConfidence    string
	OpeningHandSource     string
	OpeningHandConfidence string
	OpeningHands          []derivedOpeningHand
}

type cardPlayGameFact struct {
	TurnCount *int64
	PlayDraw  string
}

type replayHandSnapshot struct {
	ObservedAt string
	ByInstance map[int64]int64
}

func pointerInt64(value int64) *int64 {
	copy := value
	return &copy
}

func nullableDerivedInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func replayFrameGameNumber(frame model.MatchReplayFrameRow) int64 {
	if frame.GameNumber != nil && *frame.GameNumber > 0 {
		return *frame.GameNumber
	}
	return 1
}

func replaySelfHand(frame model.MatchReplayFrameRow) replayHandSnapshot {
	hand := replayHandSnapshot{
		ObservedAt: frame.RecordedAt,
		ByInstance: make(map[int64]int64),
	}
	for _, object := range frame.Objects {
		if object.PlayerSide != "self" || !strings.EqualFold(strings.TrimSpace(object.ZoneType), "hand") {
			continue
		}
		if object.InstanceID <= 0 || object.CardID <= 0 {
			continue
		}
		hand.ByInstance[object.InstanceID] = object.CardID
	}
	return hand
}

func handSignature(hand replayHandSnapshot) string {
	ids := make([]int64, 0, len(hand.ByInstance))
	for instanceID := range hand.ByInstance {
		ids = append(ids, instanceID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var builder strings.Builder
	for _, id := range ids {
		builder.WriteString(strconv.FormatInt(id, 10))
		builder.WriteByte(',')
	}
	return builder.String()
}

func handIsSubset(candidate, offered replayHandSnapshot) bool {
	if len(candidate.ByInstance) > len(offered.ByInstance) {
		return false
	}
	for instanceID := range candidate.ByInstance {
		if _, ok := offered.ByInstance[instanceID]; !ok {
			return false
		}
	}
	return true
}

func aggregateOpeningCards(offered, kept replayHandSnapshot, decision string) []derivedOpeningCard {
	type tally struct {
		quantity int64
		kept     int64
	}
	byCard := make(map[int64]tally)
	for _, cardID := range offered.ByInstance {
		row := byCard[cardID]
		row.quantity++
		byCard[cardID] = row
	}
	if decision == "keep" {
		for _, cardID := range kept.ByInstance {
			row := byCard[cardID]
			row.kept++
			byCard[cardID] = row
		}
	}

	cardIDs := make([]int64, 0, len(byCard))
	for cardID := range byCard {
		cardIDs = append(cardIDs, cardID)
	}
	sort.Slice(cardIDs, func(i, j int) bool { return cardIDs[i] < cardIDs[j] })
	out := make([]derivedOpeningCard, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		row := byCard[cardID]
		out = append(out, derivedOpeningCard{
			CardID:   cardID,
			Quantity: row.quantity,
			Kept:     row.kept > 0,
		})
	}
	return out
}

func deriveOpeningHands(frames []model.MatchReplayFrameRow) ([]derivedOpeningHand, *int64, *int64) {
	prePlay := make([]replayHandSnapshot, 0)
	var firstPlayHand *replayHandSnapshot
	for _, frame := range frames {
		hand := replaySelfHand(frame)
		isPlay := (frame.TurnNumber != nil && *frame.TurnNumber > 0) ||
			strings.Contains(strings.ToLower(frame.GameStage), "play") ||
			strings.Contains(strings.ToLower(frame.GameStage), "gameover")
		if isPlay {
			isOpeningTurn := frame.TurnNumber == nil || *frame.TurnNumber <= 1
			if firstPlayHand == nil && isOpeningTurn && len(hand.ByInstance) > 0 {
				copy := hand
				firstPlayHand = &copy
			}
			continue
		}
		if len(hand.ByInstance) > 0 {
			prePlay = append(prePlay, hand)
		}
	}

	// Some logs begin at the first playable frame. Preserve that exact hand as
	// a low-information single keep rather than claiming there was no hand.
	if len(prePlay) == 0 && firstPlayHand != nil && len(firstPlayHand.ByInstance) <= 7 {
		prePlay = append(prePlay, *firstPlayHand)
	}
	if len(prePlay) == 0 {
		return nil, nil, nil
	}

	maxSize := 0
	for _, hand := range prePlay {
		if len(hand.ByInstance) > maxSize {
			maxSize = len(hand.ByInstance)
		}
	}
	if maxSize <= 0 {
		return nil, nil, nil
	}

	offers := make([]replayHandSnapshot, 0)
	lastSignature := ""
	for _, hand := range prePlay {
		if len(hand.ByInstance) != maxSize {
			continue
		}
		signature := handSignature(hand)
		if signature == lastSignature {
			continue
		}
		offers = append(offers, hand)
		lastSignature = signature
	}
	if len(offers) == 0 {
		return nil, nil, nil
	}

	finalOffer := offers[len(offers)-1]
	kept := finalOffer
	// London mulligans expose a seven-card offer followed by the cards put on
	// the bottom. Prefer the last observed subset, including the first play
	// frame when it is still a subset of the final offer.
	for _, hand := range prePlay {
		if handIsSubset(hand, finalOffer) && len(hand.ByInstance) <= len(kept.ByInstance) {
			kept = hand
		}
	}
	if firstPlayHand != nil && handIsSubset(*firstPlayHand, finalOffer) {
		kept = *firstPlayHand
	}

	out := make([]derivedOpeningHand, 0, len(offers))
	for index, offer := range offers {
		isFinal := index == len(offers)-1
		decision := "mulligan"
		keptForAttempt := replayHandSnapshot{ByInstance: map[int64]int64{}}
		var keptSize *int64
		if isFinal {
			decision = "keep"
			keptForAttempt = kept
			keptSize = pointerInt64(int64(len(kept.ByInstance)))
		}
		out = append(out, derivedOpeningHand{
			AttemptNumber:   int64(index + 1),
			Decision:        decision,
			OfferedHandSize: int64(len(offer.ByInstance)),
			KeptHandSize:    keptSize,
			ObservedAt:      offer.ObservedAt,
			Cards:           aggregateOpeningCards(offer, keptForAttempt, decision),
		})
	}

	return out, pointerInt64(int64(len(offers) - 1)), pointerInt64(int64(len(kept.ByInstance)))
}

func deriveReplayGames(frames []model.MatchReplayFrameRow) []derivedGame {
	byGame := make(map[int64][]model.MatchReplayFrameRow)
	for _, frame := range frames {
		gameNumber := replayFrameGameNumber(frame)
		byGame[gameNumber] = append(byGame[gameNumber], frame)
	}
	gameNumbers := make([]int64, 0, len(byGame))
	for gameNumber := range byGame {
		gameNumbers = append(gameNumbers, gameNumber)
	}
	sort.Slice(gameNumbers, func(i, j int) bool { return gameNumbers[i] < gameNumbers[j] })

	out := make([]derivedGame, 0, len(gameNumbers))
	for _, gameNumber := range gameNumbers {
		gameFrames := byGame[gameNumber]
		game := derivedGame{
			GameNumber:            gameNumber,
			Result:                "unknown",
			ResultConfidence:      "unknown",
			PlayDrawConfidence:    "unknown",
			OpeningHandConfidence: "unknown",
		}
		for _, frame := range gameFrames {
			if game.StartedAt == "" && frame.RecordedAt != "" {
				game.StartedAt = frame.RecordedAt
			}
			if frame.RecordedAt != "" {
				game.EndedAt = frame.RecordedAt
			}
			if frame.TurnNumber != nil && (game.TurnCount == nil || *frame.TurnNumber > *game.TurnCount) {
				game.TurnCount = pointerInt64(*frame.TurnNumber)
			}
			if game.OpeningLifeTotal == nil && frame.SelfLifeTotal != nil {
				game.OpeningLifeTotal = pointerInt64(*frame.SelfLifeTotal)
			}
			if frame.SelfLifeTotal != nil {
				game.EndingLifeTotal = pointerInt64(*frame.SelfLifeTotal)
			}
			switch frame.WinningPlayerSide {
			case "self":
				game.Result = "win"
				game.ResultSource = "gre_game_result"
				game.ResultConfidence = "exact"
			case "opponent":
				game.Result = "loss"
				game.ResultSource = "gre_game_result"
				game.ResultConfidence = "exact"
			}
			if frame.WinReason != "" {
				game.WinReason = frame.WinReason
			}
		}
		game.OpeningHands, game.MulliganCount, game.KeptHandSize = deriveOpeningHands(gameFrames)
		if len(game.OpeningHands) > 0 {
			game.OpeningHandSource = "replay_private_hand"
			game.OpeningHandConfidence = "derived"
		}
		out = append(out, game)
	}
	return out
}

func (s *Store) loadCardPlayGameFacts(ctx context.Context, matchID int64) (map[int64]cardPlayGameFact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			cp.game_number,
			MAX(cp.turn_number),
			COALESCE((
				SELECT CASE
					WHEN first.owner_seat_id = m.player_seat_id AND first.turn_number % 2 = 1 THEN 'play'
					WHEN first.owner_seat_id = m.player_seat_id AND first.turn_number % 2 = 0 THEN 'draw'
					WHEN first.owner_seat_id != m.player_seat_id AND first.turn_number % 2 = 1 THEN 'draw'
					WHEN first.owner_seat_id != m.player_seat_id AND first.turn_number % 2 = 0 THEN 'play'
					ELSE '' END
				FROM match_card_plays first
				WHERE first.match_id = cp.match_id
				  AND first.game_number = cp.game_number
				  AND first.owner_seat_id IS NOT NULL
				  AND first.turn_number IS NOT NULL
				ORDER BY first.turn_number, COALESCE(first.played_at, ''), first.id
				LIMIT 1
			), '')
		FROM match_card_plays cp
		JOIN matches m ON m.id = cp.match_id
		WHERE cp.match_id = ?
		GROUP BY cp.game_number
		ORDER BY cp.game_number
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("load game card-play facts: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]cardPlayGameFact)
	for rows.Next() {
		var gameNumber int64
		var maxTurn sql.NullInt64
		var fact cardPlayGameFact
		if err := rows.Scan(&gameNumber, &maxTurn, &fact.PlayDraw); err != nil {
			return nil, fmt.Errorf("scan game card-play facts: %w", err)
		}
		if maxTurn.Valid {
			fact.TurnCount = pointerInt64(maxTurn.Int64)
		}
		out[gameNumber] = fact
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game card-play facts: %w", err)
	}
	return out, nil
}

func mergeGameFacts(games []derivedGame, facts map[int64]cardPlayGameFact) []derivedGame {
	indexByNumber := make(map[int64]int, len(games))
	for index := range games {
		indexByNumber[games[index].GameNumber] = index
	}
	for gameNumber, fact := range facts {
		index, ok := indexByNumber[gameNumber]
		if !ok {
			games = append(games, derivedGame{
				GameNumber:            gameNumber,
				Result:                "unknown",
				ResultConfidence:      "unknown",
				PlayDrawConfidence:    "unknown",
				OpeningHandConfidence: "unknown",
			})
			index = len(games) - 1
			indexByNumber[gameNumber] = index
		}
		if games[index].TurnCount == nil {
			games[index].TurnCount = fact.TurnCount
		}
		if fact.PlayDraw != "" {
			games[index].PlayDraw = fact.PlayDraw
			games[index].PlayDrawSource = "first_observed_play"
			games[index].PlayDrawConfidence = "derived"
		}
	}
	sort.Slice(games, func(i, j int) bool { return games[i].GameNumber < games[j].GameNumber })
	return games
}

func (s *Store) RefreshMatchAnalytics(ctx context.Context, matchID int64) error {
	if matchID <= 0 {
		return nil
	}
	frames, err := s.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		// A malformed legacy archive should not prevent maintenance or the rest
		// of the match detail from loading. It simply has no usable replay
		// coverage; live relational frames, when present, are still attempted.
		if !strings.Contains(err.Error(), "decode match replay archive") {
			return err
		}
		frames, err = s.listLiveMatchReplayFrames(ctx, matchID)
		if err != nil {
			return err
		}
	}
	facts, err := s.loadCardPlayGameFacts(ctx, matchID)
	if err != nil {
		return err
	}
	games := mergeGameFacts(deriveReplayGames(frames), facts)

	var matchResult, matchReason string
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(result, 'unknown'), COALESCE(win_reason, '')
		FROM matches WHERE id = ?
	`, matchID).Scan(&matchResult, &matchReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load match summary for analytics: %w", err)
	}
	if len(games) == 1 && games[0].Result == "unknown" && (matchResult == "win" || matchResult == "loss" || matchResult == "draw") {
		games[0].Result = matchResult
		games[0].WinReason = matchReason
		games[0].ResultSource = "match_summary"
		games[0].ResultConfidence = "derived"
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := nowUTC()
	gamesWithResult := int64(0)
	gamesWithOpeningHand := int64(0)
	gamesWithPlayDraw := int64(0)
	for _, game := range games {
		result := game.Result
		if result == "" {
			result = "unknown"
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO games (
				match_id, game_number, result, win_reason, play_draw,
				started_at, ended_at, turn_count, opening_life_total, ending_life_total,
				mulligan_count, kept_hand_size, result_source, result_confidence,
				play_draw_source, play_draw_confidence, opening_hand_source,
				opening_hand_confidence, derived_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(match_id, game_number) DO UPDATE SET
				result = excluded.result,
				win_reason = excluded.win_reason,
				play_draw = excluded.play_draw,
				started_at = excluded.started_at,
				ended_at = excluded.ended_at,
				turn_count = excluded.turn_count,
				opening_life_total = excluded.opening_life_total,
				ending_life_total = excluded.ending_life_total,
				mulligan_count = excluded.mulligan_count,
				kept_hand_size = excluded.kept_hand_size,
				result_source = excluded.result_source,
				result_confidence = excluded.result_confidence,
				play_draw_source = excluded.play_draw_source,
				play_draw_confidence = excluded.play_draw_confidence,
				opening_hand_source = excluded.opening_hand_source,
				opening_hand_confidence = excluded.opening_hand_confidence,
				derived_at = excluded.derived_at
		`, matchID, game.GameNumber, result, nullIfEmpty(game.WinReason), nullIfEmpty(game.PlayDraw),
			nullIfEmpty(game.StartedAt), nullIfEmpty(game.EndedAt), nullableDerivedInt(game.TurnCount),
			nullableDerivedInt(game.OpeningLifeTotal), nullableDerivedInt(game.EndingLifeTotal),
			nullableDerivedInt(game.MulliganCount), nullableDerivedInt(game.KeptHandSize),
			nullIfEmpty(game.ResultSource), game.ResultConfidence, nullIfEmpty(game.PlayDrawSource),
			game.PlayDrawConfidence, nullIfEmpty(game.OpeningHandSource), game.OpeningHandConfidence, now)
		if err != nil {
			return fmt.Errorf("insert derived game: %w", err)
		}
		var gameID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM games WHERE match_id = ? AND game_number = ?
		`, matchID, game.GameNumber).Scan(&gameID); err != nil {
			return fmt.Errorf("lookup derived game id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM game_opening_hands WHERE game_id = ?`, gameID); err != nil {
			return fmt.Errorf("clear derived opening hands: %w", err)
		}
		for _, hand := range game.OpeningHands {
			handRes, err := tx.ExecContext(ctx, `
				INSERT INTO game_opening_hands (
					game_id, attempt_number, decision, offered_hand_size, kept_hand_size,
					observed_at, source, confidence
				) VALUES (?, ?, ?, ?, ?, ?, 'replay_private_hand', 'derived')
			`, gameID, hand.AttemptNumber, hand.Decision, hand.OfferedHandSize,
				nullableDerivedInt(hand.KeptHandSize), nullIfEmpty(hand.ObservedAt))
			if err != nil {
				return fmt.Errorf("insert opening hand: %w", err)
			}
			handID, err := handRes.LastInsertId()
			if err != nil {
				return fmt.Errorf("get opening hand id: %w", err)
			}
			for _, card := range hand.Cards {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO game_opening_hand_cards (opening_hand_id, card_id, quantity, kept)
					VALUES (?, ?, ?, ?)
				`, handID, card.CardID, card.Quantity, boolToInt(card.Kept)); err != nil {
					return fmt.Errorf("insert opening hand card: %w", err)
				}
			}
		}
		if result != "unknown" {
			gamesWithResult++
		}
		if len(game.OpeningHands) > 0 {
			gamesWithOpeningHand++
		}
		if game.PlayDraw != "" {
			gamesWithPlayDraw++
		}
	}

	if len(games) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM games WHERE match_id = ?`, matchID); err != nil {
			return fmt.Errorf("clear stale derived games: %w", err)
		}
	} else {
		placeholders := make([]string, 0, len(games))
		args := make([]any, 0, len(games)+1)
		args = append(args, matchID)
		for _, game := range games {
			placeholders = append(placeholders, "?")
			args = append(args, game.GameNumber)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM games
			WHERE match_id = ? AND game_number NOT IN (%s)
		`, strings.Join(placeholders, ",")), args...); err != nil {
			return fmt.Errorf("clear stale derived games: %w", err)
		}
	}

	var deckSnapshotCount, deckVersionCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(deck_version_id)
		FROM match_decks WHERE match_id = ?
	`, matchID).Scan(&deckSnapshotCount, &deckVersionCount); err != nil {
		return fmt.Errorf("load analytics deck coverage: %w", err)
	}
	overallConfidence := "unknown"
	if len(frames) > 0 || len(games) > 0 || deckSnapshotCount > 0 {
		overallConfidence = "partial"
	}
	if len(games) > 0 && gamesWithResult == int64(len(games)) &&
		gamesWithOpeningHand == int64(len(games)) && gamesWithPlayDraw == int64(len(games)) &&
		deckVersionCount > 0 {
		overallConfidence = "complete"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO match_analytics_coverage (
			match_id, replay_available, replay_frame_count, game_count,
			games_with_result, games_with_opening_hand, games_with_play_draw,
			deck_snapshot_available, deck_version_available, overall_confidence, derived_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id) DO UPDATE SET
			replay_available = excluded.replay_available,
			replay_frame_count = excluded.replay_frame_count,
			game_count = excluded.game_count,
			games_with_result = excluded.games_with_result,
			games_with_opening_hand = excluded.games_with_opening_hand,
			games_with_play_draw = excluded.games_with_play_draw,
			deck_snapshot_available = excluded.deck_snapshot_available,
			deck_version_available = excluded.deck_version_available,
			overall_confidence = excluded.overall_confidence,
			derived_at = excluded.derived_at
	`, matchID, boolToInt(len(frames) > 0), int64(len(frames)), int64(len(games)), gamesWithResult,
		gamesWithOpeningHand, gamesWithPlayDraw, boolToInt(deckSnapshotCount > 0),
		boolToInt(deckVersionCount > 0), overallConfidence, now); err != nil {
		return fmt.Errorf("upsert analytics coverage: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit match analytics: %w", err)
	}
	return nil
}

func (s *Store) RefreshPendingMatchAnalytics(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id
		FROM matches m
		LEFT JOIN match_analytics_coverage c ON c.match_id = m.id
		LEFT JOIN match_replay_archives a ON a.match_id = m.id
		WHERE c.match_id IS NULL
		   OR julianday(COALESCE(a.updated_at, m.updated_at)) > julianday(c.derived_at)
		   OR EXISTS (SELECT 1 FROM match_replay_frames f WHERE f.match_id = m.id)
		ORDER BY m.id
	`)
	if err != nil {
		return 0, fmt.Errorf("list pending match analytics: %w", err)
	}
	matchIDs := make([]int64, 0)
	for rows.Next() {
		var matchID int64
		if err := rows.Scan(&matchID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan pending match analytics: %w", err)
		}
		matchIDs = append(matchIDs, matchID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate pending match analytics: %w", err)
	}
	rows.Close()

	refreshed := 0
	for _, matchID := range matchIDs {
		if err := ctx.Err(); err != nil {
			return refreshed, err
		}
		if err := s.RefreshMatchAnalytics(ctx, matchID); err != nil {
			return refreshed, fmt.Errorf("refresh analytics for match %d: %w", matchID, err)
		}
		refreshed++
	}
	return refreshed, nil
}

func (s *Store) EnsureMatchAnalytics(ctx context.Context, matchID int64) error {
	var refreshNeeded int64
	err := s.db.QueryRowContext(ctx, `
		SELECT CASE
			WHEN c.match_id IS NULL THEN 1
			WHEN julianday(COALESCE(a.updated_at, '')) > julianday(c.derived_at) THEN 1
			WHEN julianday(COALESCE(m.updated_at, '')) > julianday(c.derived_at) THEN 1
			WHEN julianday(COALESCE((
				SELECT MAX(f.created_at) FROM match_replay_frames f WHERE f.match_id = m.id
			), '')) > julianday(c.derived_at) THEN 1
			ELSE 0
		END
		FROM matches m
		LEFT JOIN match_analytics_coverage c ON c.match_id = m.id
		LEFT JOIN match_replay_archives a ON a.match_id = m.id
		WHERE m.id = ?
	`, matchID).Scan(&refreshNeeded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check match analytics freshness: %w", err)
	}
	if refreshNeeded == 0 {
		return nil
	}
	return s.RefreshMatchAnalytics(ctx, matchID)
}

func (s *Store) ListMatchGames(ctx context.Context, matchID int64) ([]model.GameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, game_number, result, COALESCE(win_reason, ''), COALESCE(play_draw, ''),
			COALESCE(started_at, ''), COALESCE(ended_at, ''), turn_count,
			opening_life_total, ending_life_total, mulligan_count, kept_hand_size,
			COALESCE(result_source, ''), result_confidence,
			COALESCE(play_draw_source, ''), play_draw_confidence,
			COALESCE(opening_hand_source, ''), opening_hand_confidence
		FROM games
		WHERE match_id = ?
		ORDER BY game_number
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match games: %w", err)
	}
	defer rows.Close()

	games := make([]model.GameRow, 0)
	for rows.Next() {
		var game model.GameRow
		if err := rows.Scan(
			&game.ID, &game.GameNumber, &game.Result, &game.WinReason, &game.PlayDraw,
			&game.StartedAt, &game.EndedAt, &game.TurnCount, &game.OpeningLifeTotal,
			&game.EndingLifeTotal, &game.MulliganCount, &game.KeptHandSize,
			&game.ResultSource, &game.ResultConfidence, &game.PlayDrawSource,
			&game.PlayDrawConfidence, &game.OpeningHandSource, &game.OpeningHandConfidence,
		); err != nil {
			return nil, fmt.Errorf("scan match game: %w", err)
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match games: %w", err)
	}

	for gameIndex := range games {
		handRows, err := s.db.QueryContext(ctx, `
			SELECT id, attempt_number, decision, offered_hand_size, kept_hand_size,
				COALESCE(observed_at, ''), source, confidence
			FROM game_opening_hands
			WHERE game_id = ?
			ORDER BY attempt_number
		`, games[gameIndex].ID)
		if err != nil {
			return nil, fmt.Errorf("list opening hands: %w", err)
		}
		for handRows.Next() {
			var hand model.OpeningHandRow
			if err := handRows.Scan(&hand.ID, &hand.AttemptNumber, &hand.Decision,
				&hand.OfferedHandSize, &hand.KeptHandSize, &hand.ObservedAt,
				&hand.Source, &hand.Confidence); err != nil {
				handRows.Close()
				return nil, fmt.Errorf("scan opening hand: %w", err)
			}

			cardRows, err := s.db.QueryContext(ctx, `
				SELECT c.card_id, c.quantity, COALESCE(cc.name, ''), c.kept
				FROM game_opening_hand_cards c
				LEFT JOIN card_catalog cc ON cc.arena_id = c.card_id
				WHERE c.opening_hand_id = ?
				ORDER BY c.kept DESC, cc.name, c.card_id
			`, hand.ID)
			if err != nil {
				handRows.Close()
				return nil, fmt.Errorf("list opening hand cards: %w", err)
			}
			for cardRows.Next() {
				var card model.OpeningHandCardRow
				var kept int64
				if err := cardRows.Scan(&card.CardID, &card.Quantity, &card.CardName, &kept); err != nil {
					cardRows.Close()
					handRows.Close()
					return nil, fmt.Errorf("scan opening hand card: %w", err)
				}
				card.Kept = kept != 0
				hand.Cards = append(hand.Cards, card)
			}
			if err := cardRows.Err(); err != nil {
				cardRows.Close()
				handRows.Close()
				return nil, fmt.Errorf("iterate opening hand cards: %w", err)
			}
			cardRows.Close()
			games[gameIndex].OpeningHands = append(games[gameIndex].OpeningHands, hand)
		}
		if err := handRows.Err(); err != nil {
			handRows.Close()
			return nil, fmt.Errorf("iterate opening hands: %w", err)
		}
		handRows.Close()
	}

	return games, nil
}

func (s *Store) GetMatchAnalyticsCoverage(ctx context.Context, matchID int64) (model.MatchAnalyticsCoverage, error) {
	var out model.MatchAnalyticsCoverage
	var replayAvailable, deckSnapshotAvailable, deckVersionAvailable int64
	err := s.db.QueryRowContext(ctx, `
		SELECT replay_available, replay_frame_count, game_count, games_with_result,
			games_with_opening_hand, games_with_play_draw, deck_snapshot_available,
			deck_version_available, overall_confidence, derived_at
		FROM match_analytics_coverage
		WHERE match_id = ?
	`, matchID).Scan(&replayAvailable, &out.ReplayFrameCount, &out.GameCount,
		&out.GamesWithResult, &out.GamesWithOpeningHand, &out.GamesWithPlayDraw,
		&deckSnapshotAvailable, &deckVersionAvailable, &out.OverallConfidence, &out.DerivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("get match analytics coverage: %w", err)
	}
	out.ReplayAvailable = replayAvailable != 0
	out.DeckSnapshotAvailable = deckSnapshotAvailable != 0
	out.DeckVersionAvailable = deckVersionAvailable != 0
	return out, nil
}
