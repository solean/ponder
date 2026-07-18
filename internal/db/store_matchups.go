package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// MatchupMatchRow is one deck-linked match plus the fields matchup
// classification and aggregation need.
type MatchupMatchRow struct {
	MatchID   int64
	DeckID    int64
	DeckName  string
	Format    string
	EventName string
	Opponent  string
	Result    string
	StartedAt string
}

// ListMatchupMatchRows returns matches linked to a deck, newest first; a
// positive deckID restricts to that deck. Matches without a deck link are
// excluded: matchup aggregation is deck-scoped.
func (s *Store) ListMatchupMatchRows(ctx context.Context, deckID int64) ([]MatchupMatchRow, error) {
	where := ""
	args := []any{}
	if deckID > 0 {
		where = "WHERE md.deck_id = ?"
		args = append(args, deckID)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			m.id, md.deck_id, COALESCE(d.name, d.arena_deck_id), COALESCE(d.format, ''),
			COALESCE(m.event_name, ''), COALESCE(m.opponent_name, ''),
			COALESCE(m.result, 'unknown'), COALESCE(m.started_at, '')
		FROM matches m
		JOIN match_decks md ON md.match_id = m.id
		JOIN decks d ON d.id = md.deck_id
		%s
		ORDER BY COALESCE(m.started_at, m.ended_at, m.updated_at) DESC, m.id DESC
	`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("list matchup matches: %w", err)
	}
	defer rows.Close()

	out := make([]MatchupMatchRow, 0)
	for rows.Next() {
		var row MatchupMatchRow
		if err := rows.Scan(&row.MatchID, &row.DeckID, &row.DeckName, &row.Format,
			&row.EventName, &row.Opponent, &row.Result, &row.StartedAt); err != nil {
			return nil, fmt.Errorf("scan matchup match: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matchup matches: %w", err)
	}
	return out, nil
}

// ListMatchGameSummaries aggregates the derived games of every match into
// per-match records and splits, keyed by match id.
func (s *Store) ListMatchGameSummaries(ctx context.Context) (map[int64]model.MatchGameSummary, error) {
	query := fmt.Sprintf(`
		SELECT
			g.match_id,
			SUM(CASE WHEN g.result NOT IN ('win', 'loss', 'draw') THEN 1 ELSE 0 END),
			%s,
			%s,
			%s,
			%s,
			%s
		FROM games g
		GROUP BY g.match_id
	`,
		resultRecordColumns("1=1"),
		resultRecordColumns("g.game_number = 1"),
		resultRecordColumns("g.game_number > 1"),
		resultRecordColumns("g.play_draw = 'play'"),
		resultRecordColumns("g.play_draw = 'draw'"))

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list match game summaries: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]model.MatchGameSummary)
	for rows.Next() {
		var matchID int64
		var unknown sql.NullInt64
		var total, gameOne, postBoard, onPlay, onDraw recordScanner
		dests := []any{&matchID, &unknown}
		dests = append(dests, total.dests()...)
		dests = append(dests, gameOne.dests()...)
		dests = append(dests, postBoard.dests()...)
		dests = append(dests, onPlay.dests()...)
		dests = append(dests, onDraw.dests()...)
		if err := rows.Scan(dests...); err != nil {
			return nil, fmt.Errorf("scan match game summary: %w", err)
		}
		out[matchID] = model.MatchGameSummary{
			Games:        total.agg(),
			UnknownGames: unknown.Int64,
			GameOne:      gameOne.agg(),
			PostBoard:    postBoard.agg(),
			OnPlay:       onPlay.agg(),
			OnDraw:       onDraw.agg(),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match game summaries: %w", err)
	}
	return out, nil
}

// ListMatchOpponentArchetypeOverrides returns every manual archetype
// correction, keyed by match id.
func (s *Store) ListMatchOpponentArchetypeOverrides(ctx context.Context) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT match_id, archetype FROM match_opponent_archetype_overrides
	`)
	if err != nil {
		return nil, fmt.Errorf("list opponent archetype overrides: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]string)
	for rows.Next() {
		var matchID int64
		var archetype string
		if err := rows.Scan(&matchID, &archetype); err != nil {
			return nil, fmt.Errorf("scan opponent archetype override: %w", err)
		}
		out[matchID] = archetype
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate opponent archetype overrides: %w", err)
	}
	return out, nil
}

// SetMatchOpponentArchetypeOverride stores a manual correction; an empty
// archetype clears any existing one.
func (s *Store) SetMatchOpponentArchetypeOverride(ctx context.Context, matchID int64, archetype string) error {
	archetype = strings.ToLower(strings.TrimSpace(archetype))
	if archetype == "" {
		if _, err := s.db.ExecContext(ctx, `
			DELETE FROM match_opponent_archetype_overrides WHERE match_id = ?
		`, matchID); err != nil {
			return fmt.Errorf("clear opponent archetype override: %w", err)
		}
		return nil
	}
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO match_opponent_archetype_overrides (match_id, archetype, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(match_id) DO UPDATE SET
			archetype = excluded.archetype,
			updated_at = excluded.updated_at
	`, matchID, archetype, now, now); err != nil {
		return fmt.Errorf("set opponent archetype override: %w", err)
	}
	return nil
}
