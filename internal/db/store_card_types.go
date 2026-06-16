package db

import (
	"context"
	"fmt"
	"strings"
)

// LookupCardTypeLines returns cached Scryfall type lines for the given card IDs.
// Cards with no cached row are absent from the result.
func (s *Store) LookupCardTypeLines(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	placeholders := make([]string, 0, len(cardIDs))
	args := make([]any, 0, len(cardIDs))
	for _, id := range cardIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT arena_id, type_line
		FROM card_types
		WHERE arena_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup card type lines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var typeLine string
		if err := rows.Scan(&id, &typeLine); err != nil {
			return nil, fmt.Errorf("scan card type line: %w", err)
		}
		out[id] = typeLine
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate card type lines: %w", err)
	}

	return out, nil
}

// UpsertCardTypeLines caches resolved type lines.
func (s *Store) UpsertCardTypeLines(ctx context.Context, typeLines map[int64]string) error {
	if len(typeLines) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin card types tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO card_types (arena_id, type_line, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(arena_id) DO UPDATE SET
			type_line = excluded.type_line,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare card types upsert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for id, typeLine := range typeLines {
		if strings.TrimSpace(typeLine) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id, typeLine, now); err != nil {
			return fmt.Errorf("upsert card types row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card types tx: %w", err)
	}
	return nil
}
