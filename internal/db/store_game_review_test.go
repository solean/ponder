package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGameReviewPersistsPerGameAndCascadesWithMatch(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "ponder.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	if err := Init(ctx, database); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO matches (id, arena_match_id, created_at, updated_at)
		VALUES (7, 'review-match', '2026-08-02T00:00:00Z', '2026-08-02T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert match: %v", err)
	}

	store := NewStore(database)
	saved, err := store.UpsertGameReview(ctx, 7, 2, "source-a", "coach-model", "first review")
	if err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	if saved.MatchID != 7 || saved.GameNumber != 2 || saved.SourceHash != "source-a" || saved.Content != "first review" {
		t.Fatalf("saved review = %+v", saved)
	}

	if _, err := store.UpsertGameReview(ctx, 7, 2, "source-b", "coach-model-2", "replacement review"); err != nil {
		t.Fatalf("replace review: %v", err)
	}
	loaded, err := store.GetGameReview(ctx, 7, 2)
	if err != nil {
		t.Fatalf("get review: %v", err)
	}
	if loaded == nil || loaded.SourceHash != "source-b" || loaded.Model != "coach-model-2" || loaded.Content != "replacement review" {
		t.Fatalf("loaded review = %+v", loaded)
	}
	missing, err := store.GetGameReview(ctx, 7, 1)
	if err != nil || missing != nil {
		t.Fatalf("missing game review = %+v, %v", missing, err)
	}

	if _, err := database.ExecContext(ctx, `DELETE FROM matches WHERE id = 7`); err != nil {
		t.Fatalf("delete match: %v", err)
	}
	deleted, err := store.GetGameReview(ctx, 7, 2)
	if err != nil || deleted != nil {
		t.Fatalf("review after match delete = %+v, %v", deleted, err)
	}
}
