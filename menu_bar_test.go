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
