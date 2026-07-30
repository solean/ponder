package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/ai"
)

func TestAIUsageSummaryPersistsProviderCounters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ponder.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := Init(ctx, database); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	store := NewStore(database)

	records := []struct {
		deckID    int64
		provider  string
		model     string
		usage     ai.TokenUsage
		succeeded bool
	}{
		{
			deckID:   1,
			provider: ai.ProviderOpenAI,
			model:    "gpt-test",
			usage: ai.TokenUsage{
				InputTokens:           100,
				CachedInputTokens:     20,
				CacheWriteInputTokens: 5,
				OutputTokens:          50,
				ReasoningOutputTokens: 10,
			},
			succeeded: true,
		},
		{
			deckID:   2,
			provider: ai.ProviderOpenAI,
			model:    "gpt-test",
			usage: ai.TokenUsage{
				InputTokens:  30,
				OutputTokens: 5,
			},
			succeeded: false,
		},
		{
			deckID:   3,
			provider: ai.ProviderClaude,
			model:    "opus",
			usage: ai.TokenUsage{
				InputTokens:           40,
				CachedInputTokens:     80,
				CacheWriteInputTokens: 10,
				OutputTokens:          20,
			},
			succeeded: true,
		},
	}
	for _, record := range records {
		if err := store.RecordAIUsage(
			ctx,
			record.deckID,
			record.provider,
			record.model,
			record.usage,
			record.succeeded,
		); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}
	if err := store.RecordAIUsage(ctx, 4, ai.ProviderClaude, "opus", ai.TokenUsage{}, false); err != nil {
		t.Fatalf("ignore empty usage: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	database, err = Open(path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer database.Close()
	summary, err := NewStore(database).GetAIUsageSummary(ctx)
	if err != nil {
		t.Fatalf("get usage summary: %v", err)
	}
	if summary.Runs != 3 || summary.SuccessfulRuns != 2 {
		t.Fatalf("runs = %d/%d, want 2 successful of 3", summary.SuccessfulRuns, summary.Runs)
	}
	if summary.InputTokens != 170 || summary.CachedInputTokens != 100 || summary.CacheWriteInputTokens != 15 ||
		summary.OutputTokens != 75 || summary.ReasoningOutputTokens != 10 {
		t.Fatalf("usage totals = %+v", summary.TokenUsage)
	}
	if len(summary.Providers) != 2 {
		t.Fatalf("providers = %+v, want two summaries", summary.Providers)
	}
	if summary.Providers[0].Provider != ai.ProviderClaude || summary.Providers[0].Runs != 1 ||
		summary.Providers[0].OutputTokens != 20 {
		t.Fatalf("Claude usage = %+v", summary.Providers[0])
	}
	if summary.Providers[1].Provider != ai.ProviderOpenAI || summary.Providers[1].Runs != 2 ||
		summary.Providers[1].InputTokens != 130 || summary.Providers[1].OutputTokens != 55 {
		t.Fatalf("OpenAI usage = %+v", summary.Providers[1])
	}
	if summary.LastRun == nil || summary.LastRun.DeckID != 3 || summary.LastRun.Provider != ai.ProviderClaude ||
		summary.LastRun.Model != "opus" || !summary.LastRun.Succeeded || summary.LastRun.OutputTokens != 20 {
		t.Fatalf("last usage event = %+v", summary.LastRun)
	}
}
