package db

import (
	"context"
	"database/sql"
	"fmt"
)

// matchSavedSQL reports whether the match aliased as m is bookmarked.
const matchSavedSQL = `EXISTS (SELECT 1 FROM match_bookmarks b WHERE b.match_id = m.id)`

// SetMatchBookmark saves or unsaves a match. Saving an already saved match
// keeps its original created_at. It returns sql.ErrNoRows when the match does
// not exist.
func (s *Store) SetMatchBookmark(ctx context.Context, matchID int64, saved bool) error {
	if !saved {
		if _, err := s.db.ExecContext(ctx, `
			DELETE FROM match_bookmarks WHERE match_id = ?
		`, matchID); err != nil {
			return fmt.Errorf("clear match bookmark: %w", err)
		}
		return nil
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO match_bookmarks (match_id, created_at)
		SELECT id, ? FROM matches WHERE id = ?
		ON CONFLICT(match_id) DO NOTHING
	`, nowUTC(), matchID)
	if err != nil {
		return fmt.Errorf("set match bookmark: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM matches WHERE id = ?)
		`, matchID).Scan(&exists); err != nil {
			return fmt.Errorf("check bookmarked match: %w", err)
		}
		if !exists {
			return sql.ErrNoRows
		}
	}
	return nil
}
