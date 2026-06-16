package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cschnabel/mtgdata/internal/model"
)

type DeckCard struct {
	Section  string
	CardID   int64
	Quantity int64
}

func isDraftDeck(format, eventName string) bool {
	if strings.EqualFold(strings.TrimSpace(format), "draft") {
		return true
	}

	e := strings.ToLower(strings.TrimSpace(eventName))
	if e == "" {
		return false
	}

	return strings.Contains(e, "draft")
}

func normalizeDeckScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "constructed":
		return "constructed"
	case "draft":
		return "draft"
	case "all":
		return "all"
	default:
		return "constructed"
	}
}

func deckInScope(scope, format, eventName string) bool {
	scope = normalizeDeckScope(scope)
	isDraft := isDraftDeck(format, eventName)
	switch scope {
	case "draft":
		return isDraft
	case "all":
		return true
	default:
		return !isDraft
	}
}

func (s *Store) UpsertDeck(ctx context.Context, tx *sql.Tx, arenaDeckID, eventName, name, format, source, lastUpdated string, cards []DeckCard) (int64, error) {
	now := nowUTC()
	lastUpdated = normalizeTS(lastUpdated)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO decks (
			arena_deck_id, event_name, name, format, source, last_updated, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(arena_deck_id) DO UPDATE SET
			event_name = COALESCE(excluded.event_name, decks.event_name),
			name = COALESCE(excluded.name, decks.name),
			format = COALESCE(excluded.format, decks.format),
			source = COALESCE(excluded.source, decks.source),
			last_updated = COALESCE(excluded.last_updated, decks.last_updated),
			updated_at = excluded.updated_at
	`, arenaDeckID, nullIfEmpty(eventName), nullIfEmpty(name), nullIfEmpty(format), nullIfEmpty(source), nullIfEmpty(lastUpdated), now, now)
	if err != nil {
		return 0, fmt.Errorf("upsert deck: %w", err)
	}

	var deckID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM decks WHERE arena_deck_id = ?`, arenaDeckID).Scan(&deckID)
	if err != nil {
		return 0, fmt.Errorf("fetch deck id: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM deck_cards WHERE deck_id = ?`, deckID); err != nil {
		return 0, fmt.Errorf("clear deck_cards: %w", err)
	}

	for _, c := range cards {
		if c.Quantity <= 0 {
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO deck_cards (deck_id, section, card_id, quantity)
			VALUES (?, ?, ?, ?)
		`, deckID, c.Section, c.CardID, c.Quantity)
		if err != nil {
			return 0, fmt.Errorf("insert deck_card: %w", err)
		}
	}

	return deckID, nil
}

func (s *Store) LinkMatchToLatestDeckByEvent(ctx context.Context, tx *sql.Tx, arenaMatchID, eventName, reason string) error {
	if eventName == "" {
		return nil
	}
	alias, err := s.resolveEventNameAlias(ctx, tx, eventName)
	if err != nil {
		return err
	}
	if alias != "" {
		eventName = alias
	}

	var (
		matchID   int64
		startedAt sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `SELECT id, started_at FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&matchID, &startedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get match id: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT snapshot_reason
		FROM match_decks
		WHERE match_id = ?
		ORDER BY id ASC
	`, matchID)
	if err != nil {
		return fmt.Errorf("list existing match decks: %w", err)
	}

	hasLinks := false
	hasSameReason := false
	for rows.Next() {
		hasLinks = true
		var existingReason string
		if err := rows.Scan(&existingReason); err != nil {
			rows.Close()
			return fmt.Errorf("scan existing match deck reason: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(existingReason), strings.TrimSpace(reason)) {
			hasSameReason = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate existing match decks: %w", err)
	}
	rows.Close()

	// Keep the first resolved room-state link stable across reparses.
	if hasSameReason {
		return nil
	}
	// Only room state is allowed to override an earlier pre-match guess.
	if hasLinks && !strings.EqualFold(strings.TrimSpace(reason), "room_state") {
		return nil
	}

	var deckID int64
	queryArgs := []any{eventName}
	query := `
		SELECT id
		FROM decks
		WHERE event_name = ?
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	if startedAt.Valid && strings.TrimSpace(startedAt.String) != "" {
		normalizedStartedAt := normalizeTS(startedAt.String)
		query = `
			SELECT id
			FROM decks
			WHERE event_name = ?
			ORDER BY
				CASE
					WHEN julianday(updated_at) <= julianday(?) THEN 0
					ELSE 1
				END,
				CASE
					WHEN julianday(updated_at) <= julianday(?) THEN julianday(updated_at)
					ELSE NULL
				END DESC,
				julianday(updated_at) DESC,
				id DESC
			LIMIT 1
		`
		queryArgs = append(queryArgs, normalizedStartedAt, normalizedStartedAt)
	}

	err = tx.QueryRowContext(ctx, query, queryArgs...).Scan(&deckID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find deck for match: %w", err)
	}

	if hasLinks {
		if _, err := tx.ExecContext(ctx, `DELETE FROM match_decks WHERE match_id = ?`, matchID); err != nil {
			return fmt.Errorf("clear prior match_decks: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO match_decks (match_id, deck_id, snapshot_reason, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(match_id, deck_id) DO NOTHING
	`, matchID, deckID, reason, nowUTC())
	if err != nil {
		return fmt.Errorf("link match_deck: %w", err)
	}

	return nil
}

func (s *Store) ListDecks(ctx context.Context) ([]model.DeckSummaryRow, error) {
	return s.ListDecksByScope(ctx, "constructed")
}

func (s *Store) ListDecksByScope(ctx context.Context, scope string) ([]model.DeckSummaryRow, error) {
	scope = normalizeDeckScope(scope)

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			d.id,
			COALESCE(d.name, d.arena_deck_id) AS deck_name,
			COALESCE(d.format, ''),
			COALESCE(d.event_name, ''),
			COUNT(m.id) AS matches,
			SUM(CASE WHEN m.result = 'win' THEN 1 ELSE 0 END) AS wins,
			SUM(CASE WHEN m.result = 'loss' THEN 1 ELSE 0 END) AS losses,
			COALESCE(MIN(COALESCE(m.started_at, m.ended_at)), '') AS first_played_at,
			COALESCE(d.last_updated, d.created_at, '') AS last_updated_at
		FROM decks d
		LEFT JOIN match_decks md ON md.deck_id = d.id
		LEFT JOIN matches m ON m.id = md.match_id
		GROUP BY d.id, d.name, d.arena_deck_id, d.format, d.event_name, d.last_updated, d.created_at
		ORDER BY matches DESC, deck_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()

	var out []model.DeckSummaryRow
	for rows.Next() {
		var r model.DeckSummaryRow
		if err := rows.Scan(
			&r.DeckID,
			&r.DeckName,
			&r.Format,
			&r.EventName,
			&r.Matches,
			&r.Wins,
			&r.Losses,
			&r.FirstPlayedAt,
			&r.LastUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan deck summary: %w", err)
		}
		if !deckInScope(scope, r.Format, r.EventName) {
			continue
		}
		if r.Matches > 0 {
			r.WinRate = float64(r.Wins) / float64(r.Matches)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decks: %w", err)
	}
	return out, nil
}

func (s *Store) GetDeckDetail(ctx context.Context, deckID int64, matchLimit int64) (model.DeckDetail, error) {
	var out model.DeckDetail
	if matchLimit <= 0 {
		matchLimit = 50
	}

	err := s.db.QueryRowContext(ctx, `
		SELECT id, arena_deck_id, COALESCE(name, ''), COALESCE(format, ''), COALESCE(event_name, '')
		FROM decks
		WHERE id = ?
	`, deckID).Scan(&out.DeckID, &out.ArenaDeckID, &out.Name, &out.Format, &out.EventName)
	if err != nil {
		return out, fmt.Errorf("get deck: %w", err)
	}

	out.Cards, err = s.ListDeckCards(ctx, deckID)
	if err != nil {
		return out, err
	}

	matchRows, err := s.db.QueryContext(ctx, `
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			COALESCE(m.opponent_name, ''),
			COALESCE(m.started_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(m.win_reason, ''),
			COALESCE(
				m.turn_count,
				(
					SELECT SUM(game_turns)
					FROM (
						SELECT MAX(cp.turn_number) AS game_turns
						FROM match_card_plays cp
						WHERE cp.match_id = m.id AND cp.turn_number IS NOT NULL
						GROUP BY cp.game_number
					)
				)
			),
			COALESCE(
				m.seconds_count,
				CASE
					WHEN m.started_at IS NOT NULL AND m.ended_at IS NOT NULL THEN
						CAST(ROUND((julianday(m.ended_at) - julianday(m.started_at)) * 86400.0) AS INTEGER)
					ELSE NULL
				END
			)
		FROM matches m
		JOIN match_decks md ON md.match_id = m.id
		WHERE md.deck_id = ?
		ORDER BY COALESCE(m.started_at, m.ended_at, m.updated_at) DESC
		LIMIT ?
	`, deckID, matchLimit)
	if err != nil {
		return out, fmt.Errorf("get deck matches: %w", err)
	}
	defer matchRows.Close()

	for matchRows.Next() {
		var m model.MatchRow
		if err := matchRows.Scan(
			&m.ID,
			&m.ArenaMatchID,
			&m.EventName,
			&m.Opponent,
			&m.StartedAt,
			&m.EndedAt,
			&m.Result,
			&m.WinReason,
			&m.TurnCount,
			&m.SecondsCount,
		); err != nil {
			return out, fmt.Errorf("scan deck match row: %w", err)
		}
		out.Matches = append(out.Matches, m)
	}
	if err := matchRows.Err(); err != nil {
		return out, fmt.Errorf("iterate deck matches: %w", err)
	}

	return out, nil
}
