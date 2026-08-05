package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/solean/ponder/internal/ai"
	"github.com/solean/ponder/internal/model"
)

type AIUsageTotals struct {
	Runs           int64 `json:"runs"`
	SuccessfulRuns int64 `json:"successfulRuns"`
	ai.TokenUsage
}

type AIProviderUsage struct {
	Provider string `json:"provider"`
	AIUsageTotals
}

type AIUsageEvent struct {
	DeckID    int64  `json:"deckId"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Succeeded bool   `json:"succeeded"`
	CreatedAt string `json:"createdAt"`
	ai.TokenUsage
}

type AIUsageSummary struct {
	AIUsageTotals
	Providers []AIProviderUsage `json:"providers"`
	LastRun   *AIUsageEvent     `json:"lastRun,omitempty"`
}

// GetDeckPrimer returns the cached AI primer for a deck, or (nil, nil) when
// none has been generated yet.
func (s *Store) GetDeckPrimer(ctx context.Context, deckID int64) (*model.DeckPrimer, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT deck_id, cards_hash, model, content, created_at
		FROM deck_ai_primers
		WHERE deck_id = ?
	`, deckID)

	var out model.DeckPrimer
	err := row.Scan(&out.DeckID, &out.CardsHash, &out.Model, &out.Content, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deck primer: %w", err)
	}
	return &out, nil
}

// UpsertDeckPrimer stores (or replaces) the AI primer for a deck and returns
// the stored row.
func (s *Store) UpsertDeckPrimer(ctx context.Context, deckID int64, cardsHash, modelName, content string) (*model.DeckPrimer, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO deck_ai_primers (deck_id, cards_hash, model, content, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(deck_id) DO UPDATE SET
			cards_hash = excluded.cards_hash,
			model = excluded.model,
			content = excluded.content,
			created_at = excluded.created_at
	`, deckID, cardsHash, modelName, content, createdAt)
	if err != nil {
		return nil, fmt.Errorf("upsert deck primer: %w", err)
	}
	return &model.DeckPrimer{
		DeckID:    deckID,
		CardsHash: cardsHash,
		Model:     modelName,
		Content:   content,
		CreatedAt: createdAt,
	}, nil
}

// GetGameReview returns the cached AI review for one game, or (nil, nil) when
// none has been generated yet.
func (s *Store) GetGameReview(ctx context.Context, matchID, gameNumber int64) (*model.GameReview, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT match_id, game_number, source_hash, model, content, created_at
		FROM match_ai_game_reviews
		WHERE match_id = ? AND game_number = ?
	`, matchID, gameNumber)

	var out model.GameReview
	err := row.Scan(&out.MatchID, &out.GameNumber, &out.SourceHash, &out.Model, &out.Content, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get game review: %w", err)
	}
	return &out, nil
}

// UpsertGameReview stores (or replaces) the AI review for one game and returns
// the stored row.
func (s *Store) UpsertGameReview(
	ctx context.Context,
	matchID, gameNumber int64,
	sourceHash, modelName, content string,
) (*model.GameReview, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO match_ai_game_reviews (
			match_id, game_number, source_hash, model, content, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id, game_number) DO UPDATE SET
			source_hash = excluded.source_hash,
			model = excluded.model,
			content = excluded.content,
			created_at = excluded.created_at
	`, matchID, gameNumber, sourceHash, modelName, content, createdAt)
	if err != nil {
		return nil, fmt.Errorf("upsert game review: %w", err)
	}
	return &model.GameReview{
		MatchID:    matchID,
		GameNumber: gameNumber,
		SourceHash: sourceHash,
		Model:      modelName,
		Content:    content,
		CreatedAt:  createdAt,
	}, nil
}

// RecordAIUsage stores provider-reported accounting for one metered run.
func (s *Store) RecordAIUsage(
	ctx context.Context,
	deckID int64,
	provider, modelName string,
	usage ai.TokenUsage,
	succeeded bool,
) error {
	if !usage.HasTokens() {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_usage_events (
			deck_id,
			provider,
			model,
			input_tokens,
			cached_input_tokens,
			cache_write_input_tokens,
			output_tokens,
			reasoning_output_tokens,
			succeeded,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		deckID,
		provider,
		modelName,
		max(int64(0), usage.InputTokens),
		max(int64(0), usage.CachedInputTokens),
		max(int64(0), usage.CacheWriteInputTokens),
		max(int64(0), usage.OutputTokens),
		max(int64(0), usage.ReasoningOutputTokens),
		succeeded,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("record AI usage: %w", err)
	}
	return nil
}

func (s *Store) GetAIUsageSummary(ctx context.Context) (AIUsageSummary, error) {
	var summary AIUsageSummary
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(succeeded), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(cached_input_tokens), 0),
			COALESCE(SUM(cache_write_input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_output_tokens), 0)
		FROM ai_usage_events
	`).Scan(
		&summary.Runs,
		&summary.SuccessfulRuns,
		&summary.InputTokens,
		&summary.CachedInputTokens,
		&summary.CacheWriteInputTokens,
		&summary.OutputTokens,
		&summary.ReasoningOutputTokens,
	); err != nil {
		return AIUsageSummary{}, fmt.Errorf("summarize AI usage: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			provider,
			COUNT(*),
			COALESCE(SUM(succeeded), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(cached_input_tokens), 0),
			COALESCE(SUM(cache_write_input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_output_tokens), 0)
		FROM ai_usage_events
		GROUP BY provider
		ORDER BY provider
	`)
	if err != nil {
		return AIUsageSummary{}, fmt.Errorf("summarize AI usage by provider: %w", err)
	}
	defer rows.Close()
	summary.Providers = []AIProviderUsage{}
	for rows.Next() {
		var provider AIProviderUsage
		if err := rows.Scan(
			&provider.Provider,
			&provider.Runs,
			&provider.SuccessfulRuns,
			&provider.InputTokens,
			&provider.CachedInputTokens,
			&provider.CacheWriteInputTokens,
			&provider.OutputTokens,
			&provider.ReasoningOutputTokens,
		); err != nil {
			return AIUsageSummary{}, fmt.Errorf("scan AI provider usage: %w", err)
		}
		summary.Providers = append(summary.Providers, provider)
	}
	if err := rows.Err(); err != nil {
		return AIUsageSummary{}, fmt.Errorf("iterate AI provider usage: %w", err)
	}

	var last AIUsageEvent
	err = s.db.QueryRowContext(ctx, `
		SELECT
			deck_id,
			provider,
			model,
			input_tokens,
			cached_input_tokens,
			cache_write_input_tokens,
			output_tokens,
			reasoning_output_tokens,
			succeeded,
			created_at
		FROM ai_usage_events
		ORDER BY id DESC
		LIMIT 1
	`).Scan(
		&last.DeckID,
		&last.Provider,
		&last.Model,
		&last.InputTokens,
		&last.CachedInputTokens,
		&last.CacheWriteInputTokens,
		&last.OutputTokens,
		&last.ReasoningOutputTokens,
		&last.Succeeded,
		&last.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return summary, nil
	}
	if err != nil {
		return AIUsageSummary{}, fmt.Errorf("get latest AI usage: %w", err)
	}
	summary.LastRun = &last
	return summary, nil
}
