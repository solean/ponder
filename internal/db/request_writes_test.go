package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/solean/ponder/internal/model"
)

func TestRequestWritesWaitForBackgroundWriter(t *testing.T) {
	cases := []struct {
		name  string
		write func(context.Context, *Store) error
		query string
	}{
		{"card names", func(ctx context.Context, s *Store) error {
			return s.UpsertCardNames(ctx, map[int64]string{1: "Forest"})
		}, "SELECT COUNT(*) FROM card_catalog"},
		{"card types", func(ctx context.Context, s *Store) error {
			return s.UpsertCardTypeLines(ctx, map[int64]string{1: "Basic Land"})
		}, "SELECT COUNT(*) FROM card_types"},
		{"card metadata", func(ctx context.Context, s *Store) error {
			return s.UpsertCardMetadata(ctx, map[int64]CardMetadata{1: {ColorIdentity: "G"}})
		}, "SELECT COUNT(*) FROM card_metadata"},
		{"sets", func(ctx context.Context, s *Store) error {
			return s.UpsertSets(ctx, map[string]model.SetInfo{"tst": {Name: "Test"}})
		}, "SELECT COUNT(*) FROM set_catalog"},
		{"match analytics", func(ctx context.Context, s *Store) error { return s.EnsureMatchAnalytics(ctx, 1) }, "SELECT COUNT(*) FROM match_analytics_coverage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			database, err := Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := Init(ctx, database); err != nil {
				t.Fatal(err)
			}
			store := NewStore(database)
			tx, err := store.BeginTx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.UpsertMatchStart(ctx, tx, "match", "Ladder", 1, "2026-09-29T00:00:00Z"); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}

			// One connection holds a real SQLite write lock. The other fails any
			// competing write immediately, making this independent of busy_timeout.
			database.SetMaxOpenConns(2)
			reader, err := database.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reader.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
				t.Fatal(err)
			}
			release, err := store.AcquireWriter(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if release != nil {
					release()
				}
			}()
			writer, err := store.BeginTx(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback()
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			go func() { done <- tc.write(ctx, store) }()
			select {
			case err := <-done:
				t.Fatalf("request finished while background writer held lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			// Queued writes must leave the remaining connection available to reads.
			var count int
			if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM matches").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("matches = %d, want 1", count)
			}

			canceled, stop := context.WithCancel(ctx)
			stop()
			if err := tc.write(canceled, store); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled request: %v", err)
			}
			if err := writer.Rollback(); err != nil {
				t.Fatal(err)
			}
			release()
			release = nil
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := database.QueryRowContext(ctx, tc.query).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("persisted rows = %d, want 1", count)
			}
		})
	}
}
