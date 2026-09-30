package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// LookupSets returns cached set metadata for the given lowercase set codes.
// Codes with no cached row are simply absent from the result.
func (s *Store) LookupSets(ctx context.Context, codes []string) (map[string]model.SetInfo, error) {
	out := make(map[string]model.SetInfo, len(codes))
	if len(codes) == 0 {
		return out, nil
	}

	placeholders := make([]string, 0, len(codes))
	args := make([]any, 0, len(codes))
	for _, code := range codes {
		placeholders = append(placeholders, "?")
		args = append(args, strings.ToLower(strings.TrimSpace(code)))
	}

	query := fmt.Sprintf(`
		SELECT code, name, icon_svg_uri, released_at
		FROM set_catalog
		WHERE code IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup sets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			info     model.SetInfo
			iconURI  sql.NullString
			released sql.NullString
		)
		if err := rows.Scan(&info.Code, &info.Name, &iconURI, &released); err != nil {
			return nil, fmt.Errorf("scan set: %w", err)
		}
		info.IconSvgURI = iconURI.String
		info.ReleasedAt = released.String
		out[info.Code] = info
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sets: %w", err)
	}

	return out, nil
}

// UpsertSets caches set metadata. Codes are normalized to lowercase.
func (s *Store) UpsertSets(ctx context.Context, sets map[string]model.SetInfo) error {
	if len(sets) == 0 {
		return nil
	}

	release, err := s.AcquireWriter(ctx)
	if err != nil {
		return err
	}
	defer release()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set catalog tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO set_catalog (code, name, icon_svg_uri, released_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			name = excluded.name,
			icon_svg_uri = excluded.icon_svg_uri,
			released_at = excluded.released_at,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare set catalog upsert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for code, info := range sets {
		normalized := strings.ToLower(strings.TrimSpace(code))
		if normalized == "" || strings.TrimSpace(info.Name) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, normalized, info.Name, nullableString(info.IconSvgURI), nullableString(info.ReleasedAt), now); err != nil {
			return fmt.Errorf("upsert set catalog row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit set catalog tx: %w", err)
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
