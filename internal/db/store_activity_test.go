package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/solean/ponder/internal/model"
)

func TestDailyActivityLocalMidnightsAndDST(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name   string
		first  time.Time
		starts []string
	}{
		{"spring", time.Date(2026, 3, 7, 0, 0, 0, 0, location), []string{
			"2026-03-07T04:59:59.999999999Z", // before requested range
			"2026-03-07T05:00:00Z",
			"2026-03-08T04:59:59.999999999Z", // still March 7 locally
			"2026-03-08T00:00:00-05:00",      // local midnight with explicit offset
			"2026-03-08T06:59:59Z",           // before skipped hour
			"2026-03-08T07:00:00Z",           // after skipped hour
			"2026-03-09T03:59:59.999999999Z", // 23-hour day ends
			"2026-03-09T04:00:00Z",
			"2026-03-10T04:00:00Z", // exclusive range end
			"invalid",
		}},
		{"fall", time.Date(2026, 10, 31, 0, 0, 0, 0, location), []string{
			"2026-10-31T03:59:59.999999999Z",
			"2026-10-31T04:00:00Z",
			"2026-11-01T03:59:59.999999999Z",
			"2026-11-01T00:00:00-04:00",
			"2026-11-01T01:30:00-04:00", // both instances of repeated hour
			"2026-11-01T01:30:00-05:00",
			"2026-11-02T04:59:59.999999999Z", // 25-hour day ends
			"2026-11-02T05:00:00Z",
			"2026-11-03T05:00:00Z",
			"invalid",
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx := context.Background()
			database := openTempSQLiteDB(t)
			if err := Init(ctx, database); err != nil {
				t.Fatal(err)
			}
			for i, start := range fixture.starts {
				// Null result is unresolved, and nonpositive explicit durations
				// must not fall back to the ended-at duration.
				seconds := 0
				if i%2 == 1 {
					seconds = -10
				}
				_, err := database.ExecContext(ctx, `INSERT INTO matches
					(arena_match_id, started_at, ended_at, seconds_count, event_name, created_at, updated_at)
					VALUES (?, ?, '2026-12-01T00:00:00Z', ?, 'Jump_In', '2026-01-01', '2026-01-01')`, fmt.Sprint(i), start, seconds)
				if err != nil {
					t.Fatal(err)
				}
			}
			days := make([]model.ActivityDayBoundary, 3)
			for i := range days {
				start := fixture.first.AddDate(0, 0, i)
				days[i] = model.ActivityDayBoundary{Date: start.Format("2006-01-02"), Start: start.UTC().Format(time.RFC3339), End: start.AddDate(0, 0, 1).UTC().Format(time.RFC3339)}
			}
			activity, err := NewStore(database).DailyActivity(ctx, days)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range []int64{2, 4, 1} {
				day := activity[i]
				if day.Count != want || day.Unknown != want || day.Limited != want || day.Constructed != 0 || day.TimedMatches != 0 || day.TrackedSeconds != 0 {
					t.Fatalf("day %d = %+v, want %d unresolved limited untimed matches", i, day, want)
				}
			}
		})
	}
}
