package db

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// DraftAnalyticsScope selects matches by their event, not by captured draft
// sessions or deck metadata: reused decks must not pull other queues into scope.
type DraftAnalyticsScope struct {
	Sets   []string
	clause string
	args   []any
}

// LoadDraftAnalyticsScope discovers all available draft sets independently of
// the optional selection. A nil selection means all; an empty string is unknown.
func (s *Store) LoadDraftAnalyticsScope(ctx context.Context, set *string) (DraftAnalyticsScope, error) {
	out := DraftAnalyticsScope{Sets: []string{}, clause: "0=1"}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT COALESCE(event_name, '') FROM matches`)
	if err != nil {
		return out, fmt.Errorf("list draft analytics events: %w", err)
	}
	defer rows.Close()
	sets := make(map[string]bool)
	for rows.Next() {
		var event string
		if err := rows.Scan(&event); err != nil {
			return out, err
		}
		if !isDraftDeck("", event) {
			continue
		}
		code := model.LimitedSetCode(event)
		sets[code] = true
		if set == nil || *set == code {
			out.args = append(out.args, event)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for code := range sets {
		out.Sets = append(out.Sets, code)
	}
	sort.Strings(out.Sets)
	if len(out.args) > 0 {
		// match_decks can contain several decks per match. Pick one link only,
		// preferring a captured version for coverage, without requiring a link.
		out.clause = "m.event_name IN (" + strings.TrimSuffix(strings.Repeat("?,", len(out.args)), ",") + `)
			AND (md.deck_id IS NULL OR md.deck_id = (
				SELECT deck_id FROM match_decks WHERE match_id = m.id
				ORDER BY deck_version_id IS NULL, deck_id LIMIT 1
			))`
	}
	return out, nil
}

func (s *Store) GetDraftAnalytics(ctx context.Context, scope DraftAnalyticsScope) (model.DeckAnalytics, error) {
	return s.getScopedAnalytics(ctx, 0, 0, scope.clause, scope.args)
}

func (s *Store) ListDraftKeptHandCardIDs(ctx context.Context, scope DraftAnalyticsScope) ([]int64, error) {
	return s.listScopedKeptHandCardIDs(ctx, scope.clause, scope.args)
}

func (s *Store) ListDraftAnalyticsGames(ctx context.Context, scope DraftAnalyticsScope, q DeckAnalyticsGamesQuery) ([]model.DeckAnalyticsGameRef, error) {
	return s.listScopedAnalyticsGames(ctx, q, scope.clause, scope.args)
}
