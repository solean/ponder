package db

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) LookupCardNames(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return names, nil
	}

	placeholders := make([]string, 0, len(cardIDs))
	args := make([]any, 0, len(cardIDs))
	for _, id := range cardIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT arena_id, name
		FROM card_catalog
		WHERE arena_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup card names: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan card name: %w", err)
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate card names: %w", err)
	}

	return names, nil
}

func (s *Store) UpsertCardNames(ctx context.Context, names map[int64]string) error {
	if len(names) == 0 {
		return nil
	}

	release, err := s.AcquireWriter(ctx)
	if err != nil {
		return err
	}
	defer release()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin card catalog tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO card_catalog (arena_id, name, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(arena_id) DO UPDATE SET
			name = excluded.name,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare card catalog upsert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for id, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id, name, now); err != nil {
			return fmt.Errorf("upsert card catalog row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card catalog tx: %w", err)
	}
	return nil
}
