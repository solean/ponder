package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestActivityIncludesCompleteYearBeyondMatchListLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	insert := func(id, start, event, result string, seconds any, end any) {
		t.Helper()
		_, err := tx.ExecContext(ctx, `INSERT INTO matches
			(arena_match_id, started_at, event_name, result, seconds_count, ended_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '2026-07-03T12:00:00Z', '2026-07-03T12:00:00Z')`, id, start, event, result, seconds, end)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 600; i++ {
		event, result := "Ladder", "win"
		if i%3 == 1 {
			event, result = "FIN_Quick_Draft", "loss"
		}
		if i%3 == 2 {
			event, result = "JumpIn", "unknown"
		}
		insert(fmt.Sprintf("in-%d", i), "2026-07-03T12:00:00Z", event, result, 60, nil)
	}
	insert("old-in-range", "2025-07-04T04:00:00Z", "Sealed", "win", nil, "2025-07-04T04:02:00Z")
	insert("before", "2025-07-04T03:59:59Z", "Ladder", "win", 99, nil)
	insert("after", "2026-07-04T04:00:00Z", "Ladder", "win", 99, nil)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	days := make([]map[string]string, 365)
	first := time.Date(2025, 7, 4, 0, 0, 0, 0, location)
	for i := range days {
		start := first.AddDate(0, 0, i)
		days[i] = map[string]string{"date": start.Format("2006-01-02"), "start": start.UTC().Format(time.RFC3339), "end": start.AddDate(0, 0, 1).UTC().Format(time.RFC3339)}
	}
	body, err := json.Marshal(map[string]any{"days": days})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(db.NewStore(database), "", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, localRequest(http.MethodPost, "/api/activity", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var activity []struct {
		Date                                                                             string
		Count, Wins, Losses, Unknown, TrackedSeconds, TimedMatches, Constructed, Limited int64
	}
	if err := json.Unmarshal(w.Body.Bytes(), &activity); err != nil {
		t.Fatal(err)
	}
	if len(activity) != 365 {
		t.Fatalf("days = %d, want 365", len(activity))
	}
	var total int64
	for i, day := range activity {
		total += day.Count
		if day.Date != days[i]["date"] {
			t.Fatalf("day %d date = %s", i, day.Date)
		}
		if day.Count != day.Wins+day.Losses+day.Unknown || day.Count != day.Constructed+day.Limited {
			t.Fatalf("inconsistent day: %+v", day)
		}
	}
	if total != 601 {
		t.Fatalf("total = %d, want 601", total)
	}
	firstDay, lastDay := activity[0], activity[364]
	if firstDay.Count != 1 || firstDay.TrackedSeconds != 120 || firstDay.TimedMatches != 1 {
		t.Fatalf("first day = %+v", firstDay)
	}
	if lastDay.Count != 600 || lastDay.Wins != 200 || lastDay.Losses != 200 || lastDay.Unknown != 200 || lastDay.TrackedSeconds != 36000 || lastDay.TimedMatches != 600 || lastDay.Constructed != 200 || lastDay.Limited != 400 {
		t.Fatalf("last day = %+v", lastDay)
	}
	if activity[1].Count != 0 {
		t.Fatalf("empty day = %+v", activity[1])
	}
	// Activity is independent of the existing bounded recent list.
	recent, err := db.NewStore(database).ListMatches(ctx, 500, "", "")
	if err != nil || len(recent) != 500 {
		t.Fatalf("recent = %d rows, %v", len(recent), err)
	}
}

func TestActivityRequestValidation(t *testing.T) {
	t.Parallel()
	valid := []model.ActivityDayBoundary{
		{Date: "2026-03-07", Start: "2026-03-07T05:00:00Z", End: "2026-03-08T05:00:00Z"},
		{Date: "2026-03-08", Start: "2026-03-08T00:00:00-05:00", End: "2026-03-09T04:00:00Z"},
		{Date: "2026-03-09", Start: "2026-03-09T04:00:00Z", End: "2026-03-10T04:00:00Z"},
	}
	if err := validateActivityDays(valid); err != nil {
		t.Fatalf("valid 23-hour day: %v", err)
	}
	for _, fixture := range []struct {
		name   string
		change func([]model.ActivityDayBoundary) []model.ActivityDayBoundary
	}{
		{"empty", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary { return nil }},
		{"too many", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			return make([]model.ActivityDayBoundary, 367)
		}},
		{"invalid date", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[0].Date = "2026-02-30"
			return days
		}},
		{"invalid timestamp", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[0].Start = "yesterday"
			return days
		}},
		{"reversed", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[0].Start = days[0].End
			return days
		}},
		{"oversized interval", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[0].Start = "2026-03-01T05:00:00Z"
			return days
		}},
		{"duplicate date", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[1].Date = days[0].Date
			return days
		}},
		{"out of order", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[0], days[1] = days[1], days[0]
			return days
		}},
		{"skipped date", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			return append(days[:1], days[2:]...)
		}},
		{"overlap", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[1].Start = "2026-03-08T04:00:00Z"
			return days
		}},
		{"gap", func(days []model.ActivityDayBoundary) []model.ActivityDayBoundary {
			days[1].Start = "2026-03-08T06:00:00Z"
			return days
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if err := validateActivityDays(fixture.change(append([]model.ActivityDayBoundary(nil), valid...))); err == nil {
				t.Fatal("expected invalid boundaries")
			}
		})
	}
	server := NewServer(nil, "", nil)
	for _, fixture := range []struct {
		method, body string
		status       int
	}{
		{http.MethodGet, "", http.StatusMethodNotAllowed},
		{http.MethodPost, `{"days":[]}`, http.StatusBadRequest},
		{http.MethodPost, `{"days":[]} {}`, http.StatusBadRequest},
		{http.MethodPost, `{"unexpected":true}`, http.StatusBadRequest},
		{http.MethodPost, strings.Repeat(" ", 64<<10) + `{}`, http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, localRequest(fixture.method, "/api/activity", strings.NewReader(fixture.body)))
		if w.Code != fixture.status {
			t.Fatalf("request %q: status = %d, want %d", fixture.body[:min(30, len(fixture.body))], w.Code, fixture.status)
		}
	}
}
