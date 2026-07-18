package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func normalizedDeckCards(cards []DeckCard) []DeckCard {
	type key struct {
		section string
		cardID  int64
	}
	quantities := make(map[key]int64, len(cards))
	for _, card := range cards {
		section := strings.ToLower(strings.TrimSpace(card.Section))
		if section == "" || card.CardID <= 0 || card.Quantity <= 0 {
			continue
		}
		quantities[key{section: section, cardID: card.CardID}] += card.Quantity
	}

	out := make([]DeckCard, 0, len(quantities))
	for cardKey, quantity := range quantities {
		out = append(out, DeckCard{
			Section:  cardKey.section,
			CardID:   cardKey.cardID,
			Quantity: quantity,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		return out[i].CardID < out[j].CardID
	})
	return out
}

func deckCardsHash(cards []DeckCard) string {
	normalized := normalizedDeckCards(cards)
	var builder strings.Builder
	for _, card := range normalized {
		builder.WriteString(card.Section)
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatInt(card.CardID, 10))
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatInt(card.Quantity, 10))
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func upsertDeckVersion(
	ctx context.Context,
	tx *sql.Tx,
	deckID int64,
	source, effectiveAt string,
	cards []DeckCard,
) (int64, error) {
	normalized := normalizedDeckCards(cards)
	if deckID <= 0 || len(normalized) == 0 {
		return 0, nil
	}

	cardsHash := deckCardsHash(normalized)
	createdAt := nowUTC()
	effectiveAt = normalizeTS(effectiveAt)
	if effectiveAt == "" {
		effectiveAt = createdAt
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO deck_versions (
			deck_id, version_number, cards_hash, source, effective_at, created_at
		)
		SELECT ?, COALESCE(MAX(version_number), 0) + 1, ?, ?, ?, ?
		FROM deck_versions
		WHERE deck_id = ?
		ON CONFLICT(deck_id, cards_hash) DO NOTHING
	`, deckID, cardsHash, nullIfEmpty(strings.TrimSpace(source)), effectiveAt, createdAt, deckID)
	if err != nil {
		return 0, fmt.Errorf("upsert deck version: %w", err)
	}

	var versionID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM deck_versions WHERE deck_id = ? AND cards_hash = ?
	`, deckID, cardsHash).Scan(&versionID); err != nil {
		return 0, fmt.Errorf("lookup deck version: %w", err)
	}

	var cardCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM deck_version_cards WHERE deck_version_id = ?
	`, versionID).Scan(&cardCount); err != nil {
		return 0, fmt.Errorf("count deck version cards: %w", err)
	}
	if cardCount == 0 {
		for _, card := range normalized {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO deck_version_cards (deck_version_id, section, card_id, quantity)
				VALUES (?, ?, ?, ?)
			`, versionID, card.Section, card.CardID, card.Quantity); err != nil {
				return 0, fmt.Errorf("insert deck version card: %w", err)
			}
		}
	}

	return versionID, nil
}

func currentDeckVersionID(ctx context.Context, tx *sql.Tx, deckID int64, at string) (*int64, error) {
	if deckID <= 0 {
		return nil, nil
	}
	at = normalizeTS(at)
	query := `
		SELECT id
		FROM deck_versions
		WHERE deck_id = ?
		ORDER BY COALESCE(effective_at, created_at) DESC, version_number DESC
		LIMIT 1
	`
	args := []any{deckID}
	if at != "" {
		query = `
			SELECT id
			FROM deck_versions
			WHERE deck_id = ?
			ORDER BY
				CASE WHEN julianday(COALESCE(effective_at, created_at)) <= julianday(?) THEN 0 ELSE 1 END,
				CASE WHEN julianday(COALESCE(effective_at, created_at)) <= julianday(?)
					THEN julianday(COALESCE(effective_at, created_at)) END DESC,
				version_number DESC
			LIMIT 1
		`
		args = append(args, at, at)
	}

	var versionID int64
	err := tx.QueryRowContext(ctx, query, args...).Scan(&versionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup current deck version: %w", err)
	}
	return &versionID, nil
}
