package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/solean/ponder/internal/model"
)

// GetLiveMatchID returns the id of the match currently in progress, if any. A
// match is "live" once UpsertMatchStart has created its row but UpdateMatchEnd
// has not yet filled in a result/ended_at. The recency bound keeps an abandoned
// game (closed Arena mid-match) from resurfacing days later.
func (s *Store) GetLiveMatchID(ctx context.Context) (int64, bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM matches
		WHERE result IS NULL
		  AND ended_at IS NULL
		  AND started_at IS NOT NULL
		  AND updated_at >= datetime('now', '-6 hours')
		ORDER BY started_at DESC
		LIMIT 1
	`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get live match id: %w", err)
	}
	return id, true, nil
}

// GetLiveProgress reports the latest observed game and raw Arena turn for an
// in-progress match. Replay frames and submitted-deck snapshots establish the
// game before either player has produced a public card play.
func (s *Store) GetLiveProgress(ctx context.Context, matchID int64) (gameNumber, turnNumber int64, err error) {
	err = s.db.QueryRowContext(ctx, `
		WITH progress AS (
			SELECT game_number, turn_number
			FROM match_card_plays
			WHERE match_id = ?
			UNION ALL
			SELECT game_number, turn_number
			FROM match_replay_frames
			WHERE match_id = ?
			UNION ALL
			SELECT game_number, NULL
			FROM match_game_deck_snapshots
			WHERE match_id = ?
			UNION ALL
			SELECT game_number, NULL
			FROM games
			WHERE match_id = ?
		)
		SELECT
			COALESCE(MAX(game_number), 0),
			COALESCE(MAX(CASE
				WHEN game_number = (SELECT MAX(game_number) FROM progress)
				THEN turn_number
			END), 0)
		FROM progress
	`, matchID, matchID, matchID, matchID).Scan(&gameNumber, &turnNumber)
	if err != nil {
		return 0, 0, fmt.Errorf("get live progress: %w", err)
	}
	return gameNumber, turnNumber, nil
}

// ListDeckCards returns every card row for a deck (all sections), with names
// resolved from the local catalog. Shared by GetDeckDetail and the live match
// assembler.
func (s *Store) ListDeckCards(ctx context.Context, deckID int64) ([]model.DeckCardRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT dc.section, dc.card_id, dc.quantity, COALESCE(cc.name, '')
		FROM deck_cards dc
		LEFT JOIN card_catalog cc ON cc.arena_id = dc.card_id
		WHERE deck_id = ?
		ORDER BY dc.section, dc.card_id
	`, deckID)
	if err != nil {
		return nil, fmt.Errorf("list deck cards: %w", err)
	}
	defer rows.Close()

	var cards []model.DeckCardRow
	for rows.Next() {
		var c model.DeckCardRow
		if err := rows.Scan(&c.Section, &c.CardID, &c.Quantity, &c.CardName); err != nil {
			return nil, fmt.Errorf("scan deck card: %w", err)
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck cards: %w", err)
	}
	return cards, nil
}

// ListMatchGameDeckCards returns the exact deck configuration Arena submitted
// for one game. found distinguishes a captured empty snapshot from no snapshot.
func (s *Store) ListMatchGameDeckCards(
	ctx context.Context,
	matchID, gameNumber int64,
) (cards []model.DeckCardRow, found bool, err error) {
	if gameNumber <= 0 {
		gameNumber = 1
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			c.section,
			c.card_id,
			c.quantity,
			COALESCE(cc.name, '')
		FROM match_game_deck_snapshots s
		LEFT JOIN match_game_deck_snapshot_cards c ON c.snapshot_id = s.id
		LEFT JOIN card_catalog cc ON cc.arena_id = c.card_id
		WHERE s.match_id = ? AND s.game_number = ?
		ORDER BY c.section, cc.name, c.card_id
	`, matchID, gameNumber)
	if err != nil {
		return nil, false, fmt.Errorf("list match game deck cards: %w", err)
	}
	defer rows.Close()

	cards = make([]model.DeckCardRow, 0)
	for rows.Next() {
		var (
			snapshotID int64
			section    sql.NullString
			cardID     sql.NullInt64
			quantity   sql.NullInt64
			cardName   string
		)
		if err := rows.Scan(&snapshotID, &section, &cardID, &quantity, &cardName); err != nil {
			return nil, false, fmt.Errorf("scan match game deck card: %w", err)
		}
		found = snapshotID > 0
		if !section.Valid || !cardID.Valid || cardID.Int64 <= 0 || !quantity.Valid || quantity.Int64 <= 0 {
			continue
		}
		cards = append(cards, model.DeckCardRow{
			Section:  section.String,
			CardID:   cardID.Int64,
			Quantity: quantity.Int64,
			CardName: cardName,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate match game deck cards: %w", err)
	}
	return cards, found, nil
}

// GetLiveKnownSelfCardCounts counts the player's non-token cards in every
// tracked zone outside the library in the latest frame for a game. available
// is false until a frame with a known player seat has been captured.
func (s *Store) GetLiveKnownSelfCardCounts(
	ctx context.Context,
	matchID, gameNumber int64,
) (counts map[int64]int64, available bool, err error) {
	if gameNumber <= 0 {
		gameNumber = 1
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT f.id AS frame_id, m.player_seat_id
			FROM match_replay_frames f
			JOIN matches m ON m.id = f.match_id
			WHERE f.match_id = ?
				AND f.game_number = ?
				AND m.player_seat_id IS NOT NULL
				AND m.player_seat_id > 0
			ORDER BY COALESCE(f.game_state_id, 0) DESC, f.id DESC
			LIMIT 1
		)
		SELECT latest.frame_id, o.card_id, COUNT(o.id)
		FROM latest
		LEFT JOIN match_replay_frame_objects o
			ON o.frame_id = latest.frame_id
			AND o.card_id > 0
			AND COALESCE(o.is_token, 0) = 0
			AND COALESCE(o.owner_seat_id, o.controller_seat_id) = latest.player_seat_id
			AND LOWER(TRIM(COALESCE(o.zone_type, ''))) <> 'library'
		GROUP BY latest.frame_id, o.card_id
	`, matchID, gameNumber)
	if err != nil {
		return nil, false, fmt.Errorf("get live known self cards: %w", err)
	}
	defer rows.Close()

	counts = make(map[int64]int64)
	for rows.Next() {
		var (
			frameID  int64
			cardID   sql.NullInt64
			quantity int64
		)
		if err := rows.Scan(&frameID, &cardID, &quantity); err != nil {
			return nil, false, fmt.Errorf("scan live known self card: %w", err)
		}
		available = frameID > 0
		if cardID.Valid && cardID.Int64 > 0 && quantity > 0 {
			counts[cardID.Int64] = quantity
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate live known self cards: %w", err)
	}
	return counts, available, nil
}
