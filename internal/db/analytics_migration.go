package db

import (
	"context"
	"fmt"
)

func migrateAnalyticsTables(ctx context.Context, conn dbConn) error {
	hasDeckVersionID, err := tableHasColumn(ctx, conn, "match_decks", "deck_version_id")
	if err != nil {
		return fmt.Errorf("inspect match_decks deck version schema: %w", err)
	}
	if !hasDeckVersionID {
		if _, err := conn.ExecContext(ctx, `
			ALTER TABLE match_decks
			ADD COLUMN deck_version_id INTEGER REFERENCES deck_versions(id) ON DELETE SET NULL
		`); err != nil {
			return fmt.Errorf("add match deck version: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_match_decks_version ON match_decks(deck_version_id)
	`); err != nil {
		return fmt.Errorf("index match deck versions: %w", err)
	}
	return nil
}

func backfillDeckVersions(ctx context.Context, conn dbConn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin deck version backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(source, ''), COALESCE(last_updated, created_at, '')
		FROM decks
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("list decks for version backfill: %w", err)
	}
	type deckSeed struct {
		id          int64
		source      string
		effectiveAt string
	}
	seeds := make([]deckSeed, 0)
	for rows.Next() {
		var seed deckSeed
		if err := rows.Scan(&seed.id, &seed.source, &seed.effectiveAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan deck version seed: %w", err)
		}
		seeds = append(seeds, seed)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate deck version seeds: %w", err)
	}
	rows.Close()

	for _, seed := range seeds {
		cardRows, err := tx.QueryContext(ctx, `
			SELECT section, card_id, quantity
			FROM deck_cards
			WHERE deck_id = ?
			ORDER BY section, card_id
		`, seed.id)
		if err != nil {
			return fmt.Errorf("list cards for deck version backfill: %w", err)
		}
		cards := make([]DeckCard, 0)
		for cardRows.Next() {
			var card DeckCard
			if err := cardRows.Scan(&card.Section, &card.CardID, &card.Quantity); err != nil {
				cardRows.Close()
				return fmt.Errorf("scan deck card for version backfill: %w", err)
			}
			cards = append(cards, card)
		}
		if err := cardRows.Err(); err != nil {
			cardRows.Close()
			return fmt.Errorf("iterate deck cards for version backfill: %w", err)
		}
		cardRows.Close()

		versionID, err := upsertDeckVersion(ctx, tx, seed.id, seed.source, seed.effectiveAt, cards)
		if err != nil {
			return err
		}
		if versionID > 0 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE match_decks
				SET deck_version_id = ?
				WHERE deck_id = ? AND deck_version_id IS NULL
			`, versionID, seed.id); err != nil {
				return fmt.Errorf("backfill match deck version: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deck version backfill: %w", err)
	}
	return nil
}
