package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/solean/ponder/internal/model"
)

const (
	// MaxMatchTagLength caps one tag, in characters.
	MaxMatchTagLength = 40
	// MaxMatchTags caps how many tags one match can carry.
	MaxMatchTags = 20
)

// NormalizeMatchTag trims a tag, collapses inner whitespace and lowercases
// it. It returns "" for a blank tag and an error for one that is too long.
func NormalizeMatchTag(raw string) (string, error) {
	tag := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	if utf8.RuneCountInString(tag) > MaxMatchTagLength {
		return "", fmt.Errorf("tag %q is longer than %d characters", tag, MaxMatchTagLength)
	}
	return tag, nil
}

// NormalizeMatchTags normalises, de-duplicates and sorts a tag set, dropping
// blanks.
func NormalizeMatchTags(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		tag, err := NormalizeMatchTag(value)
		if err != nil {
			return nil, err
		}
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > MaxMatchTags {
		return nil, fmt.Errorf("a match can have at most %d tags", MaxMatchTags)
	}
	sort.Strings(out)
	return out, nil
}

// SetMatchTags replaces a match's tags. Tags must
// already be normalised (see NormalizeMatchTags). It returns sql.ErrNoRows
// when the match does not exist.
func (s *Store) SetMatchTags(ctx context.Context, matchID int64, tags []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set match tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM matches WHERE id = ?)
	`, matchID).Scan(&exists); err != nil {
		return fmt.Errorf("check tagged match: %w", err)
	}
	if !exists {
		return sql.ErrNoRows
	}

	// Keep created_at for tags that stay; only add and remove the difference.
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tags)), ",")
	args := make([]any, 0, len(tags)+1)
	args = append(args, matchID)
	for _, tag := range tags {
		args = append(args, tag)
	}
	deleteSQL := `DELETE FROM match_tags WHERE match_id = ?`
	if len(tags) > 0 {
		deleteSQL += ` AND tag NOT IN (` + placeholders + `)`
	}
	if _, err := tx.ExecContext(ctx, deleteSQL, args...); err != nil {
		return fmt.Errorf("remove match tags: %w", err)
	}
	now := nowUTC()
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO match_tags (match_id, tag, created_at)
			VALUES (?, ?, ?)
			ON CONFLICT(match_id, tag) DO NOTHING
		`, matchID, tag, now); err != nil {
			return fmt.Errorf("add match tag: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit match tags: %w", err)
	}
	return nil
}

// ListMatchTags returns one match's tags, sorted.
func (s *Store) ListMatchTags(ctx context.Context, matchID int64) ([]string, error) {
	byMatch, err := s.listMatchTags(ctx, `WHERE match_id = ?`, matchID)
	if err != nil {
		return nil, err
	}
	if tags := byMatch[matchID]; tags != nil {
		return tags, nil
	}
	return []string{}, nil
}

// ListAllMatchTags returns every match's tags, keyed by match id.
func (s *Store) ListAllMatchTags(ctx context.Context) (map[int64][]string, error) {
	return s.listMatchTags(ctx, ``)
}

func (s *Store) listMatchTags(ctx context.Context, where string, args ...any) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT match_id, tag FROM match_tags `+where+`
		ORDER BY match_id, tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list match tags: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]string)
	for rows.Next() {
		var matchID int64
		var tag string
		if err := rows.Scan(&matchID, &tag); err != nil {
			return nil, fmt.Errorf("scan match tag: %w", err)
		}
		out[matchID] = append(out[matchID], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match tags: %w", err)
	}
	return out, nil
}

// ListMatchTagUsage returns every tag in use with its match count, most used
// first, for autocomplete.
func (s *Store) ListMatchTagUsage(ctx context.Context) ([]model.MatchTagUsage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tag, COUNT(*) FROM match_tags
		GROUP BY tag
		ORDER BY COUNT(*) DESC, tag ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list match tag usage: %w", err)
	}
	defer rows.Close()

	out := []model.MatchTagUsage{}
	for rows.Next() {
		var usage model.MatchTagUsage
		if err := rows.Scan(&usage.Tag, &usage.Count); err != nil {
			return nil, fmt.Errorf("scan match tag usage: %w", err)
		}
		out = append(out, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match tag usage: %w", err)
	}
	return out, nil
}
