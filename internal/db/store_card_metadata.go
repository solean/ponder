package db

import (
	"context"
	"fmt"
	"strings"
)

// CardMetadata is the cached per-card classification input: color identity as
// a WUBRG-ordered subset string ("UB") and mana value when known.
type CardMetadata struct {
	ColorIdentity string
	ManaValue     *float64
}

// LookupCardMetadata returns cached metadata for the given card IDs. Missing
// cards are simply absent from the result.
func (s *Store) LookupCardMetadata(ctx context.Context, cardIDs []int64) (map[int64]CardMetadata, error) {
	out := make(map[int64]CardMetadata, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}
	for _, batch := range int64Batches(cardIDs, sqliteInClauseBatchSize) {
		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, cardID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, cardID)
		}
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT arena_id, color_identity, mana_value
			FROM card_metadata
			WHERE arena_id IN (%s)
		`, strings.Join(placeholders, ",")), args...)
		if err != nil {
			return nil, fmt.Errorf("lookup card metadata: %w", err)
		}
		for rows.Next() {
			var cardID int64
			var meta CardMetadata
			if err := rows.Scan(&cardID, &meta.ColorIdentity, &meta.ManaValue); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan card metadata: %w", err)
			}
			out[cardID] = meta
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate card metadata: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// UpsertCardMetadata caches resolved card metadata.
func (s *Store) UpsertCardMetadata(ctx context.Context, metadata map[int64]CardMetadata) error {
	if len(metadata) == 0 {
		return nil
	}
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := nowUTC()
	for cardID, meta := range metadata {
		if cardID <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO card_metadata (arena_id, color_identity, mana_value, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(arena_id) DO UPDATE SET
				color_identity = excluded.color_identity,
				mana_value = excluded.mana_value,
				updated_at = excluded.updated_at
		`, cardID, meta.ColorIdentity, meta.ManaValue, now); err != nil {
			return fmt.Errorf("upsert card metadata: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card metadata: %w", err)
	}
	return nil
}
