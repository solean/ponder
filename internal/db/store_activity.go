package db

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/solean/ponder/internal/model"
)

// DailyActivity aggregates the complete requested range without loading match
// details or applying the recent-match list limit. The API validates boundaries.
func (s *Store) DailyActivity(ctx context.Context, days []model.ActivityDayBoundary) ([]model.DailyActivity, error) {
	activity := make([]model.DailyActivity, len(days))
	if len(days) == 0 {
		return activity, nil
	}
	starts := make([]time.Time, len(days))
	for i, day := range days {
		start, err := time.Parse(time.RFC3339Nano, day.Start)
		if err != nil {
			return nil, err
		}
		starts[i] = start
		activity[i].Date = day.Date
	}
	end, err := time.Parse(time.RFC3339Nano, days[len(days)-1].End)
	if err != nil {
		return nil, err
	}
	// julianday supports offset timestamps but rounds submillisecond precision.
	// Pad this prefilter by a second; precise Go comparisons below assign days
	// and enforce the requested half-open range, even immediately before midnight.
	rows, err := s.db.QueryContext(ctx, `
		SELECT started_at, COALESCE(result, 'unknown'), COALESCE(event_name, ''),
		       COALESCE(seconds_count,
		           CAST(ROUND((julianday(ended_at) - julianday(started_at)) * 86400.0) AS INTEGER)
		       )
		FROM matches
		WHERE julianday(started_at) >= julianday(?)
		  AND julianday(started_at) < julianday(?)
	`, starts[0].Add(-time.Second).UTC().Format(time.RFC3339Nano), end.Add(time.Second).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var startedAt, result, event string
		var seconds sql.NullInt64
		if err := rows.Scan(&startedAt, &result, &event, &seconds); err != nil {
			return nil, err
		}
		started, err := time.Parse(time.RFC3339Nano, startedAt)
		if err != nil || started.Before(starts[0]) || !started.Before(end) {
			continue
		}
		index := sort.Search(len(starts), func(i int) bool { return starts[i].After(started) }) - 1
		day := &activity[index]
		day.Count++
		switch result {
		case "win":
			day.Wins++
		case "loss":
			day.Losses++
		default:
			day.Unknown++
		}
		if seconds.Valid && seconds.Int64 > 0 {
			day.TrackedSeconds += seconds.Int64
			day.TimedMatches++
		}
		// Mirror overviewStats.isLimitedEvent / parseEventName, including
		// FIN_Quick_Draft, JumpIn, Jump_In, and unclassified "Jump In" names.
		normalized := strings.ReplaceAll(strings.ToLower(event), "_", "")
		if strings.Contains(normalized, "draft") || strings.Contains(normalized, "sealed") ||
			strings.Contains(normalized, "jumpin") || strings.Contains(strings.ReplaceAll(strings.ToLower(event), "_", " "), "jump in") {
			day.Limited++
		} else {
			day.Constructed++
		}
	}
	return activity, rows.Err()
}
