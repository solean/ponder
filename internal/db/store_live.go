package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cschnabel/mtgdata/internal/model"
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

// GetLiveProgress reports the current game number and the latest turn within
// that game for an in-progress match, derived from recorded card plays. Both
// values are 0 when nothing has been observed yet.
func (s *Store) GetLiveProgress(ctx context.Context, matchID int64) (gameNumber, turnNumber int64, err error) {
	var game, turn sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			MAX(game_number),
			MAX(CASE WHEN game_number = (SELECT MAX(game_number) FROM match_card_plays WHERE match_id = ?) THEN turn_number END)
		FROM match_card_plays
		WHERE match_id = ?
	`, matchID, matchID).Scan(&game, &turn)
	if err != nil {
		return 0, 0, fmt.Errorf("get live progress: %w", err)
	}
	return game.Int64, turn.Int64, nil
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
