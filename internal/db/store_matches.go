package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

var basicLandNames = map[string]struct{}{
	"plains": {}, "island": {}, "swamp": {}, "mountain": {}, "forest": {}, "wastes": {},
}

// isBasicLandName recognizes basic lands (including Snow-Covered variants) by
// name — the only cards that can legally appear more than four times in a deck.
func isBasicLandName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "snow-covered ")
	_, ok := basicLandNames[normalized]
	return ok
}

// matchListPreallocMax caps the row-slice prealloc hint so a caller-supplied
// limit cannot size a huge allocation up front; append grows past it as needed.
const matchListPreallocMax = 512

const matchBestOfSQL = `
	CASE
		WHEN LOWER(COALESCE(m.format, '')) IN ('bo3', 'bestofthree', 'best-of-three') THEN 'bo3'
		WHEN LOWER(COALESCE(m.format, '')) IN ('bo1', 'bestofone', 'best-of-one') THEN 'bo1'
		WHEN LOWER(COALESCE(m.event_name, '')) LIKE '%traditional%' THEN 'bo3'
		WHEN EXISTS (
			SELECT 1
			FROM match_replay_frames rf
			WHERE rf.match_id = m.id
			  AND rf.game_number > 1
		) THEN 'bo3'
		WHEN EXISTS (
			SELECT 1
			FROM match_card_plays cp
			WHERE cp.match_id = m.id
			  AND cp.game_number > 1
		) THEN 'bo3'
		WHEN EXISTS (
			SELECT 1
			FROM match_opponent_card_instances oc
			WHERE oc.match_id = m.id
			  AND oc.game_number > 1
		) THEN 'bo3'
		ELSE 'bo1'
	END
`

const matchPlayDrawSQL = `
	COALESCE((
		SELECT
			CASE
				WHEN cp.owner_seat_id = m.player_seat_id AND cp.turn_number % 2 = 1 THEN 'play'
				WHEN cp.owner_seat_id = m.player_seat_id AND cp.turn_number % 2 = 0 THEN 'draw'
				WHEN cp.owner_seat_id != m.player_seat_id AND cp.turn_number % 2 = 1 THEN 'draw'
				WHEN cp.owner_seat_id != m.player_seat_id AND cp.turn_number % 2 = 0 THEN 'play'
				ELSE ''
			END
		FROM match_card_plays cp
		WHERE cp.match_id = m.id
		  AND cp.game_number = 1
		  AND cp.owner_seat_id IS NOT NULL
		  AND cp.turn_number IS NOT NULL
		ORDER BY cp.turn_number ASC, COALESCE(cp.played_at, '') ASC, cp.id ASC
		LIMIT 1
	), '')
`

// matchFullTurnCountSQL projects a match's raw Arena half-turn ordinals into
// user-facing full turns. Prefer the replay-derived games table per game, then
// fill any game missing there from its latest observed card play. Applying the
// conversion before SUM is important for a multi-game match whose games can
// each end on an odd Arena turn. The legacy match-level business-event value
// is only a fallback when no per-game source exists.
//
// This expression expects the matches table to be aliased as m.
const matchFullTurnCountSQL = `
	COALESCE(
		(
			SELECT SUM(
				CASE
					WHEN per_game.arena_turn_count > 0 THEN (per_game.arena_turn_count + 1) / 2
					ELSE per_game.arena_turn_count
				END
			)
			FROM (
				SELECT g.game_number, g.turn_count AS arena_turn_count
				FROM games g
				WHERE g.match_id = m.id
				  AND g.turn_count IS NOT NULL

				UNION ALL

				SELECT cp.game_number, MAX(cp.turn_number) AS arena_turn_count
				FROM match_card_plays cp
				WHERE cp.match_id = m.id
				  AND cp.turn_number IS NOT NULL
				  AND NOT EXISTS (
					SELECT 1
					FROM games g
					WHERE g.match_id = m.id
					  AND g.game_number = cp.game_number
					  AND g.turn_count IS NOT NULL
				  )
				GROUP BY cp.game_number
			) per_game
		),
		CASE
			WHEN m.turn_count > 0 THEN (m.turn_count + 1) / 2
			ELSE m.turn_count
		END
	)
`

func (s *Store) UpsertMatchStart(ctx context.Context, tx *sql.Tx, arenaMatchID, eventName string, seatID int64, startedAt string) (int64, error) {
	resolvedEventName := eventName
	if eventName != "" {
		alias, err := s.resolveEventNameAlias(ctx, tx, eventName)
		if err != nil {
			return 0, err
		}
		resolvedEventName = alias
	}

	startedAt = normalizeTS(startedAt)
	now := nowUTC()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO matches (
			arena_match_id, event_name, player_seat_id, started_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(arena_match_id) DO UPDATE SET
			event_name = COALESCE(excluded.event_name, matches.event_name),
			player_seat_id = COALESCE(excluded.player_seat_id, matches.player_seat_id),
			started_at = COALESCE(matches.started_at, excluded.started_at),
			updated_at = excluded.updated_at
	`, arenaMatchID, nullIfEmpty(resolvedEventName), nullableInt(seatID), nullIfEmpty(startedAt), now, now)
	if err != nil {
		return 0, fmt.Errorf("upsert match start: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("fetch match id: %w", err)
	}

	if resolvedEventName != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO event_runs (event_name, event_type, status, started_at, updated_at)
			VALUES (?, ?, 'active', ?, ?)
			ON CONFLICT(event_name) DO UPDATE SET updated_at = excluded.updated_at
		`, resolvedEventName, detectEventType(resolvedEventName), nullIfEmpty(startedAt), now); err != nil {
			return 0, fmt.Errorf("ensure event run from match start: %w", err)
		}
	}

	return id, nil
}

func (s *Store) UpdateMatchOpponent(ctx context.Context, tx *sql.Tx, arenaMatchID, opponentName, opponentUserID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE matches
		SET opponent_name = COALESCE(NULLIF(?, ''), opponent_name),
			opponent_user_id = COALESCE(NULLIF(?, ''), opponent_user_id),
			updated_at = ?
		WHERE arena_match_id = ?
	`, strings.TrimSpace(opponentName), strings.TrimSpace(opponentUserID), nowUTC(), arenaMatchID)
	if err != nil {
		return fmt.Errorf("update match opponent: %w", err)
	}
	return nil
}

func (s *Store) UpsertMatchOpponentCardInstance(ctx context.Context, tx *sql.Tx, arenaMatchID string, gameNumber, instanceID, cardID int64, firstSeenAt, source string) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" || instanceID <= 0 || cardID <= 0 {
		return nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO match_opponent_card_instances (
			match_id, game_number, instance_id, card_id, source, first_seen_at, created_at
		)
		SELECT
			m.id, ?, ?, ?, ?, ?, ?
		FROM matches m
		WHERE m.arena_match_id = ?
		ON CONFLICT(match_id, game_number, instance_id) DO NOTHING
	`, gameNumber, instanceID, cardID, nullIfEmpty(source), nullIfEmpty(normalizeTS(firstSeenAt)), nowUTC(), arenaMatchID)
	if err != nil {
		return fmt.Errorf("upsert match opponent card instance: %w", err)
	}
	return nil
}

func (s *Store) UpsertMatchCardPlay(ctx context.Context, tx *sql.Tx, arenaMatchID string, gameNumber, instanceID, cardID, ownerSeatID, turnNumber int64, phase, firstPublicZone, playedAt, source string) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	firstPublicZone = strings.TrimSpace(firstPublicZone)
	if arenaMatchID == "" || instanceID <= 0 || cardID <= 0 || firstPublicZone == "" {
		return nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO match_card_plays (
			match_id, game_number, instance_id, card_id, owner_seat_id, first_public_zone, turn_number, phase, source, played_at, created_at
		)
		SELECT
			m.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM matches m
		WHERE m.arena_match_id = ?
		ON CONFLICT(match_id, game_number, instance_id) DO UPDATE SET
			owner_seat_id = COALESCE(match_card_plays.owner_seat_id, excluded.owner_seat_id),
			turn_number = COALESCE(match_card_plays.turn_number, excluded.turn_number),
			phase = COALESCE(match_card_plays.phase, excluded.phase),
			source = COALESCE(match_card_plays.source, excluded.source),
			played_at = COALESCE(match_card_plays.played_at, excluded.played_at)
		WHERE
			match_card_plays.owner_seat_id IS NULL
			OR match_card_plays.turn_number IS NULL
			OR match_card_plays.phase IS NULL
			OR match_card_plays.source IS NULL
			OR match_card_plays.played_at IS NULL
	`, gameNumber, instanceID, cardID, nullableInt(ownerSeatID), firstPublicZone, nullableInt(turnNumber), nullIfEmpty(phase), nullIfEmpty(source), nullIfEmpty(normalizeTS(playedAt)), nowUTC(), arenaMatchID)
	if err != nil {
		return fmt.Errorf("upsert match card play: %w", err)
	}
	return nil
}

func (s *Store) UpdateMatchEnd(ctx context.Context, tx *sql.Tx, arenaMatchID string, teamID, winningTeamID, turnCount, secondsCount int64, winReason, endedAt string) (string, string, bool, error) {
	endedAt = normalizeTS(endedAt)

	var eventName string
	var priorResult string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(event_name, ''), COALESCE(result, '')
		FROM matches
		WHERE arena_match_id = ?
	`, arenaMatchID).Scan(&eventName, &priorResult)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO matches (arena_match_id, ended_at, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, arenaMatchID, endedAt, nowUTC(), nowUTC()); err != nil {
			return "", "", false, fmt.Errorf("create ended-only match: %w", err)
		}
		eventName = ""
	} else if err != nil {
		return "", "", false, fmt.Errorf("get match event name: %w", err)
	}

	result := "unknown"
	if teamID > 0 && winningTeamID > 0 {
		if teamID == winningTeamID {
			result = "win"
		} else {
			result = "loss"
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE matches
		SET ended_at = COALESCE(?, ended_at),
			result = ?,
			win_reason = COALESCE(?, win_reason),
			turn_count = COALESCE(?, turn_count),
			seconds_count = COALESCE(?, seconds_count),
			updated_at = ?
		WHERE arena_match_id = ?
	`, nullIfEmpty(endedAt), result, nullIfEmpty(winReason), nullableInt(turnCount), nullableInt(secondsCount), nowUTC(), arenaMatchID)
	if err != nil {
		return "", "", false, fmt.Errorf("update match end: %w", err)
	}

	terminalChange := (result == "win" || result == "loss") && result != priorResult

	// Idempotency guard: only increment run record when match result changes into a terminal result.
	if eventName != "" && terminalChange {
		if err := s.BumpEventRunRecord(ctx, tx, eventName, result); err != nil {
			return "", "", false, err
		}
	}

	return eventName, result, terminalChange, nil
}

func (s *Store) Overview(ctx context.Context, recentLimit int64) (model.Overview, error) {
	out := model.Overview{}
	if recentLimit <= 0 {
		recentLimit = 20
	}

	playerName, err := s.PlayerName(ctx)
	if err != nil {
		return out, err
	}
	out.PlayerName = playerName

	err = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN result = 'win' THEN 1 ELSE 0 END), 0) AS wins,
			COALESCE(SUM(CASE WHEN result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
		FROM matches
	`).Scan(&out.TotalMatches, &out.Wins, &out.Losses)
	if err != nil {
		return out, fmt.Errorf("overview aggregate: %w", err)
	}
	// Win rate is over decided matches only; unknown results don't count
	// against the player.
	if decided := out.Wins + out.Losses; decided > 0 {
		out.WinRate = float64(out.Wins) / float64(decided)
	}

	recent, err := s.ListMatches(ctx, recentLimit, "", "")
	if err != nil {
		return out, err
	}
	out.Recent = recent
	return out, nil
}

func (s *Store) ListMatches(ctx context.Context, limit int64, eventName, result string) ([]model.MatchRow, error) {
	if limit <= 0 {
		limit = 200
	}
	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			COALESCE((
				SELECT d.format
				FROM match_decks md
				JOIN decks d ON d.id = md.deck_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			), ''),
			%s,
			%s,
			COALESCE(m.opponent_name, ''),
			COALESCE(m.started_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(m.win_reason, ''),
			%s,
			COALESCE(
				m.seconds_count,
				CASE
					WHEN m.started_at IS NOT NULL AND m.ended_at IS NOT NULL THEN
						CAST(ROUND((julianday(m.ended_at) - julianday(m.started_at)) * 86400.0) AS INTEGER)
					ELSE NULL
				END
			),
			(
				SELECT d.id
				FROM match_decks md
				JOIN decks d ON d.id = md.deck_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			),
			(
				SELECT d.name
				FROM match_decks md
				JOIN decks d ON d.id = md.deck_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			),
			(
				SELECT md.deck_version_id
				FROM match_decks md
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			),
			(
				SELECT dv.version_number
				FROM match_decks md
				JOIN deck_versions dv ON dv.id = md.deck_version_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			)
		FROM matches m
		WHERE (? = '' OR m.event_name = ?)
		  AND (? = '' OR m.result = ?)
		ORDER BY COALESCE(m.started_at, m.ended_at, m.updated_at) DESC
		LIMIT ?
	`, matchBestOfSQL, matchPlayDrawSQL, matchFullTurnCountSQL)
	rows, err := s.db.QueryContext(ctx, query, eventName, eventName, result, result, limit)
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	defer rows.Close()

	resultRows := make([]model.MatchRow, 0, min(limit, matchListPreallocMax))
	for rows.Next() {
		var r model.MatchRow
		if err := rows.Scan(
			&r.ID,
			&r.ArenaMatchID,
			&r.EventName,
			&r.Format,
			&r.BestOf,
			&r.PlayDraw,
			&r.Opponent,
			&r.StartedAt,
			&r.EndedAt,
			&r.Result,
			&r.WinReason,
			&r.TurnCount,
			&r.SecondsCount,
			&r.DeckID,
			&r.DeckName,
			&r.DeckVersionID,
			&r.DeckVersionNumber,
		); err != nil {
			return nil, fmt.Errorf("scan match row: %w", err)
		}
		resultRows = append(resultRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matches: %w", err)
	}

	return resultRows, nil
}

func (s *Store) CountMatches(ctx context.Context, eventName, result string) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM matches m
		WHERE (? = '' OR m.event_name = ?)
		  AND (? = '' OR m.result = ?)
	`, eventName, eventName, result, result).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("count matches: %w", err)
	}
	return total, nil
}

func (s *Store) ListMatchDeckCardQuantities(ctx context.Context, matchIDs []int64) (map[int64]map[int64]int64, error) {
	out := make(map[int64]map[int64]int64)
	for _, batch := range int64Batches(matchIDs, sqliteInClauseBatchSize) {
		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, matchID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, matchID)
		}

		query := fmt.Sprintf(`
			WITH selected_decks AS (
				SELECT md.match_id, md.deck_id, md.deck_version_id
				FROM match_decks md
				WHERE md.match_id IN (%s)
				  AND md.id = (
					SELECT first_md.id
					FROM match_decks first_md
					WHERE first_md.match_id = md.match_id
					ORDER BY first_md.id
					LIMIT 1
				)
			), selected_cards AS (
				SELECT sd.match_id, dvc.card_id, dvc.quantity
				FROM selected_decks sd
				JOIN deck_version_cards dvc ON dvc.deck_version_id = sd.deck_version_id
				WHERE dvc.section = 'main'
				UNION ALL
				SELECT sd.match_id, dc.card_id, dc.quantity
				FROM selected_decks sd
				JOIN deck_cards dc ON dc.deck_id = sd.deck_id
				WHERE sd.deck_version_id IS NULL AND dc.section = 'main'
			)
			SELECT sc.match_id, sc.card_id, MAX(sc.quantity) AS quantity
			FROM selected_cards sc
			GROUP BY sc.match_id, sc.card_id
		`, strings.Join(placeholders, ","))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("list match deck card quantities: %w", err)
		}

		for rows.Next() {
			var matchID int64
			var cardID int64
			var quantity int64
			if err := rows.Scan(&matchID, &cardID, &quantity); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan match deck card quantity: %w", err)
			}
			if _, ok := out[matchID]; !ok {
				out[matchID] = make(map[int64]int64)
			}
			out[matchID][cardID] = quantity
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate match deck card quantities: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

func (s *Store) ListMatchOpponentCardQuantities(ctx context.Context, matchIDs []int64) (map[int64]map[int64]int64, error) {
	out := make(map[int64]map[int64]int64)
	for _, batch := range int64Batches(matchIDs, sqliteInClauseBatchSize) {
		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, matchID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, matchID)
		}

		query := fmt.Sprintf(`
			WITH per_game AS (
				SELECT
					match_id,
					card_id,
					game_number,
					COUNT(*) AS quantity_in_game
				FROM match_opponent_card_instances
				WHERE match_id IN (%s)
				GROUP BY match_id, card_id, game_number
			)
			SELECT
				match_id,
				card_id,
				MAX(quantity_in_game) AS quantity
			FROM per_game
			GROUP BY match_id, card_id
		`, strings.Join(placeholders, ","))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("list opponent observed card quantities: %w", err)
		}

		for rows.Next() {
			var matchID int64
			var cardID int64
			var quantity int64
			if err := rows.Scan(&matchID, &cardID, &quantity); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan opponent observed card quantity: %w", err)
			}
			if _, ok := out[matchID]; !ok {
				out[matchID] = make(map[int64]int64)
			}
			out[matchID][cardID] = quantity
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate opponent observed card quantities: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

func (s *Store) GetMatchDetail(ctx context.Context, matchID int64) (model.MatchDetail, error) {
	var out model.MatchDetail

	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			COALESCE((
				SELECT d.format
				FROM match_decks md
				JOIN decks d ON d.id = md.deck_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			), ''),
			%s,
			%s,
			COALESCE(m.opponent_name, ''),
			COALESCE(m.started_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(m.win_reason, ''),
			%s,
			COALESCE(
				m.seconds_count,
				CASE
					WHEN m.started_at IS NOT NULL AND m.ended_at IS NOT NULL THEN
						CAST(ROUND((julianday(m.ended_at) - julianday(m.started_at)) * 86400.0) AS INTEGER)
					ELSE NULL
				END
			),
			(
				SELECT d.id
				FROM match_decks md
				JOIN decks d ON d.id = md.deck_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			),
			(
				SELECT d.name
				FROM match_decks md
				JOIN decks d ON d.id = md.deck_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			),
			(
				SELECT md.deck_version_id
				FROM match_decks md
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			),
			(
				SELECT dv.version_number
				FROM match_decks md
				JOIN deck_versions dv ON dv.id = md.deck_version_id
				WHERE md.match_id = m.id
				ORDER BY md.id ASC
				LIMIT 1
			)
		FROM matches m
		WHERE m.id = ?
		LIMIT 1
	`, matchBestOfSQL, matchPlayDrawSQL, matchFullTurnCountSQL)

	err := s.db.QueryRowContext(ctx, query, matchID).Scan(
		&out.Match.ID,
		&out.Match.ArenaMatchID,
		&out.Match.EventName,
		&out.Match.Format,
		&out.Match.BestOf,
		&out.Match.PlayDraw,
		&out.Match.Opponent,
		&out.Match.StartedAt,
		&out.Match.EndedAt,
		&out.Match.Result,
		&out.Match.WinReason,
		&out.Match.TurnCount,
		&out.Match.SecondsCount,
		&out.Match.DeckID,
		&out.Match.DeckName,
		&out.Match.DeckVersionID,
		&out.Match.DeckVersionNumber,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return out, sql.ErrNoRows
	}
	if err != nil {
		return out, fmt.Errorf("get match detail: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH per_game AS (
			SELECT
				oc.card_id,
				oc.game_number,
				COUNT(*) AS quantity_in_game
			FROM match_opponent_card_instances oc
			WHERE oc.match_id = ?
			GROUP BY oc.card_id, oc.game_number
		),
		fallback AS (
			SELECT pg.card_id, MAX(pg.quantity_in_game) AS quantity
			FROM per_game pg
			GROUP BY pg.card_id
		)
		SELECT
			f.card_id,
			-- Prefer the simultaneous-copies estimate from replay frames, which
			-- is not inflated by a single card's instance id churning across
			-- zones; fall back to distinct instances per game when no frames.
			COALESCE(mcc.max_copies, f.quantity) AS quantity,
			COALESCE(cc.name, '')
		FROM fallback f
		LEFT JOIN match_opponent_card_counts mcc
			ON mcc.match_id = ? AND mcc.card_id = f.card_id
		LEFT JOIN card_catalog cc ON cc.arena_id = f.card_id
		ORDER BY quantity DESC, cc.name ASC, f.card_id ASC
	`, matchID, matchID)
	if err != nil {
		return out, fmt.Errorf("get observed opponent cards: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var card model.OpponentObservedCardRow
		if err := rows.Scan(&card.CardID, &card.Quantity, &card.CardName); err != nil {
			return out, fmt.Errorf("scan observed opponent card: %w", err)
		}
		// Only basic lands can legally appear more than four times; cap everything
		// else so a frameless match's inflated distinct-instance fallback still
		// reports a legal copy count.
		if card.Quantity > 4 && !isBasicLandName(card.CardName) {
			card.Quantity = 4
		}
		out.OpponentObservedCards = append(out.OpponentObservedCards, card)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate observed opponent cards: %w", err)
	}
	sort.SliceStable(out.OpponentObservedCards, func(i, j int) bool {
		a, b := out.OpponentObservedCards[i], out.OpponentObservedCards[j]
		if a.Quantity != b.Quantity {
			return a.Quantity > b.Quantity
		}
		if a.CardName != b.CardName {
			return a.CardName < b.CardName
		}
		return a.CardID < b.CardID
	})

	out.CardPlays, err = s.ListMatchCardPlays(ctx, matchID)
	if err != nil {
		return out, err
	}
	out.Games, err = s.ListMatchGames(ctx, matchID)
	if err != nil {
		return out, err
	}
	out.Coverage, err = s.GetMatchAnalyticsCoverage(ctx, matchID)
	if err != nil {
		return out, err
	}

	return out, nil
}

func (s *Store) ListMatchCardPlays(ctx context.Context, matchID int64) ([]model.MatchCardPlayRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			cp.id,
			cp.game_number,
			cp.instance_id,
			cp.card_id,
			COALESCE(cc.name, ''),
			cp.owner_seat_id,
			CASE
				WHEN m.player_seat_id IS NOT NULL AND cp.owner_seat_id = m.player_seat_id THEN 'self'
				WHEN cp.owner_seat_id IS NOT NULL AND cp.owner_seat_id > 0 THEN 'opponent'
				ELSE 'unknown'
			END AS player_side,
			COALESCE(cp.first_public_zone, ''),
			cp.turn_number,
			COALESCE(cp.phase, ''),
			COALESCE(cp.played_at, '')
		FROM match_card_plays cp
		JOIN matches m ON m.id = cp.match_id
		LEFT JOIN card_catalog cc ON cc.arena_id = cp.card_id
		WHERE cp.match_id = ?
		ORDER BY cp.game_number ASC, COALESCE(cp.turn_number, 1000000) ASC, COALESCE(cp.played_at, '') ASC, cp.id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match card plays: %w", err)
	}
	defer rows.Close()

	out := make([]model.MatchCardPlayRow, 0)
	for rows.Next() {
		var row model.MatchCardPlayRow
		var gameNo sql.NullInt64
		var ownerSeat sql.NullInt64
		var turnNo sql.NullInt64
		if err := rows.Scan(
			&row.ID,
			&gameNo,
			&row.InstanceID,
			&row.CardID,
			&row.CardName,
			&ownerSeat,
			&row.PlayerSide,
			&row.FirstPublicZone,
			&turnNo,
			&row.Phase,
			&row.PlayedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match card play row: %w", err)
		}
		if gameNo.Valid {
			v := gameNo.Int64
			row.GameNumber = &v
		}
		if ownerSeat.Valid {
			v := ownerSeat.Int64
			row.OwnerSeatID = &v
		}
		if turnNo.Valid {
			v := turnNo.Int64
			row.TurnNumber = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match card plays: %w", err)
	}

	return out, nil
}
