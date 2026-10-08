package main

import (
	"testing"

	"github.com/solean/ponder/internal/appstate"
)

func TestTrackingMenuBarStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     *appstate.Status
		startupErr string
		label      string
		indicator  string
	}{
		{name: "starting", label: "Starting Ponder…"},
		{name: "startup failure", startupErr: "database unavailable", label: "Tracking unavailable", indicator: "!"},
		{name: "disabled", status: &appstate.Status{}, label: "Live tracking is off", indicator: "Ⅱ"},
		{name: "watching idle log", status: &appstate.Status{LiveRunning: true, ActiveLogPathExists: true}, label: "Tracking MTGA data"},
		{name: "missing log", status: &appstate.Status{LiveRunning: true}, label: "Waiting for MTGA log", indicator: "!"},
		{name: "parser error while running", status: &appstate.Status{LiveRunning: true, ActiveLogPathExists: true, LastError: "parse failed"}, label: "Tracking needs attention", indicator: "!"},
		{name: "failed start", status: &appstate.Status{LastError: "invalid log path"}, label: "Tracking needs attention", indicator: "!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trackingMenuBarStatus(tt.status, tt.startupErr)
			if got.label != tt.label || got.indicator != tt.indicator {
				t.Fatalf("status = %+v; want %q, %q", got, tt.label, tt.indicator)
			}
		})
	}
}
