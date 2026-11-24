package main

import (
	"testing"
	"time"

	"github.com/solean/ponder/internal/appstate"
)

func TestTrackingMenuBarStatus(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	watching := func(activity *appstate.OperationResult) *appstate.Status {
		return &appstate.Status{LiveRunning: true, ActiveLogPathExists: true, LastLiveActivity: activity}
	}
	activityAt := func(age time.Duration) *appstate.OperationResult {
		return &appstate.OperationResult{CompletedAt: now.Add(-age).Format(time.RFC3339Nano)}
	}
	match := &menuBarLiveMatch{opponent: "Rival", deckName: "Mono Red", game: 2, turn: 5}
	tests := []struct {
		name       string
		status     *appstate.Status
		startupErr string
		live       *menuBarLiveMatch
		want       menuBarStatus // detail is tooltip-only and not compared
	}{
		{name: "starting", want: menuBarStatus{status: "Starting…"}},
		{name: "startup failure", startupErr: "database unavailable", want: menuBarStatus{status: "Unavailable", activity: "database unavailable", indicator: "!"}},
		{name: "off", status: &appstate.Status{}, want: menuBarStatus{status: "Off", canToggle: true}},
		{name: "watching quiet log", status: watching(nil), want: menuBarStatus{status: "On", activity: "No MTGA activity yet", on: true, canToggle: true}},
		{name: "fresh activity", status: watching(activityAt(30 * time.Second)), want: menuBarStatus{status: "On", activity: "MTGA activity just now", on: true, canToggle: true}},
		{name: "older activity", status: watching(activityAt(61 * time.Minute)), want: menuBarStatus{status: "On", activity: "MTGA activity 1 hr ago", on: true, canToggle: true}},
		{name: "live match", status: watching(activityAt(time.Second)), live: match, want: menuBarStatus{status: "In match vs Rival", activity: "Game 2 · Turn 5 · Mono Red", on: true, canToggle: true}},
		{name: "live match before first game state", status: watching(nil), live: &menuBarLiveMatch{}, want: menuBarStatus{status: "In a match", on: true, canToggle: true}},
		{name: "missing log", status: &appstate.Status{LiveRunning: true}, live: match, want: menuBarStatus{status: "Waiting for MTGA log", on: true, canToggle: true, indicator: "!"}},
		{name: "parser error during match", status: &appstate.Status{LiveRunning: true, ActiveLogPathExists: true, LastError: "parse failed"}, live: match, want: menuBarStatus{status: "Needs attention", activity: "parse failed", on: true, canToggle: true, indicator: "!"}},
		{name: "detailed logs off", status: &appstate.Status{LiveRunning: true, ActiveLogPathExists: true, DetailedLogs: appstate.DetailedLogsDisabled}, want: menuBarStatus{status: "Detailed Logs off", activity: "Turn on in Arena settings", on: true, canToggle: true, indicator: "!"}},
		{name: "detailed logs off while stopped", status: &appstate.Status{ActiveLogPathExists: true, DetailedLogs: appstate.DetailedLogsDisabled}, want: menuBarStatus{status: "Detailed Logs off", activity: "Turn on in Arena settings", canToggle: true, indicator: "!"}},
		{name: "detailed logs on", status: &appstate.Status{LiveRunning: true, ActiveLogPathExists: true, DetailedLogs: appstate.DetailedLogsEnabled}, want: menuBarStatus{status: "On", activity: "No MTGA activity yet", on: true, canToggle: true}},
		{name: "failed start", status: &appstate.Status{LastError: "invalid log path"}, want: menuBarStatus{status: "Needs attention", activity: "invalid log path", canToggle: true, indicator: "!"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trackingMenuBarStatus(tt.status, tt.startupErr, tt.live, now)
			got.detail = ""
			if got != tt.want {
				t.Fatalf("status = %+v; want %+v", got, tt.want)
			}
		})
	}
}

func TestMenuBarStatusLine(t *testing.T) {
	tests := []struct {
		status menuBarStatus
		want   string
	}{
		{menuBarStatus{status: "Off"}, "Off"},
		{menuBarStatus{status: "On", activity: "MTGA activity 4 hr ago"}, "On · MTGA activity 4 hr ago"},
		{menuBarStatus{status: "In match vs Rival", activity: "Game 2 · Turn 5"}, "In match vs Rival · Game 2 · Turn 5"},
		{menuBarStatus{status: "Starting…", activity: "Starting…"}, "Starting…"},
	}
	for _, tt := range tests {
		if got := tt.status.statusLine(); got != tt.want {
			t.Errorf("statusLine(%+v) = %q; want %q", tt.status, got, tt.want)
		}
	}
}

func TestUpdateMenuLabel(t *testing.T) {
	const releaseURL = "https://github.com/solean/ponder/releases/tag/v1.4.0"
	tests := []struct {
		name   string
		status *appstate.Status
		want   string
	}{
		{name: "starting", want: ""},
		{name: "never checked", status: &appstate.Status{}, want: ""},
		{name: "up to date", status: &appstate.Status{UpdateCheck: &appstate.UpdateCheck{LatestVersion: "1.4.0", ReleaseURL: releaseURL}}, want: ""},
		{name: "failed check", status: &appstate.Status{UpdateCheck: &appstate.UpdateCheck{Note: "update check failed: timeout"}}, want: ""},
		{name: "available", status: &appstate.Status{UpdateCheck: &appstate.UpdateCheck{UpdateAvailable: true, LatestVersion: "1.4.0", ReleaseURL: releaseURL}}, want: "Update Available: Version 1.4.0"},
		{name: "available without version", status: &appstate.Status{UpdateCheck: &appstate.UpdateCheck{UpdateAvailable: true, ReleaseURL: releaseURL}}, want: "Update Available"},
		{name: "no release page", status: &appstate.Status{UpdateCheck: &appstate.UpdateCheck{UpdateAvailable: true, LatestVersion: "1.4.0"}}, want: ""},
		{name: "non-https release page", status: &appstate.Status{UpdateCheck: &appstate.UpdateCheck{UpdateAvailable: true, LatestVersion: "1.4.0", ReleaseURL: "file:///etc/passwd"}}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateMenuLabel(tt.status); got != tt.want {
				t.Fatalf("label = %q; want %q", got, tt.want)
			}
		})
	}
}
