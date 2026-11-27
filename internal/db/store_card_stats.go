package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/solean/ponder/internal/model"
)

// CardStatSnapshot is a cached 17Lands rating set for one expansion, event
// type, and player group.
type CardStatSnapshot struct {
	Cards     []model.CardStat
	FetchedAt time.Time
}

// LookupCardStatSnapshot returns the cached snapshot, or nil when none exists.
// An empty userGroup is 17Lands' all-players rating set.
func (s *Store) LookupCardStatSnapshot(ctx context.Context, expansion, eventType, userGroup string) (*CardStatSnapshot, error) {
	var payload, fetchedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT payload_json, fetched_at
		FROM card_stat_snapshots
		WHERE expansion = ? AND event_type = ? AND user_group = ?
	`, expansion, eventType, userGroup).Scan(&payload, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup card stat snapshot: %w", err)
	}

	fetched, err := time.Parse(time.RFC3339Nano, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("parse card stat snapshot time: %w", err)
	}
	snapshot := &CardStatSnapshot{FetchedAt: fetched}
	if err := json.Unmarshal([]byte(payload), &snapshot.Cards); err != nil {
		return nil, fmt.Errorf("decode card stat snapshot: %w", err)
	}
	return snapshot, nil
}

// UpsertCardStatSnapshot replaces the cached snapshot for one expansion, event
// type, and player group.
func (s *Store) UpsertCardStatSnapshot(ctx context.Context, expansion, eventType, userGroup string, cards []model.CardStat, fetchedAt time.Time) error {
	payload, err := json.Marshal(cards)
	if err != nil {
		return fmt.Errorf("encode card stat snapshot: %w", err)
	}

	release, err := s.AcquireWriter(ctx)
	if err != nil {
		return err
	}
	defer release()

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO card_stat_snapshots (expansion, event_type, user_group, payload_json, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(expansion, event_type, user_group) DO UPDATE SET
			payload_json = excluded.payload_json,
			fetched_at = excluded.fetched_at
	`, expansion, eventType, userGroup, string(payload), utcTimestamp(fetchedAt)); err != nil {
		return fmt.Errorf("upsert card stat snapshot: %w", err)
	}
	return nil
}
