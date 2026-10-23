package appstate

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func overlayTestService(t *testing.T) *Service {
	t.Helper()
	return &Service{configPath: filepath.Join(t.TempDir(), "config.json"), defaultPoll: time.Second, config: normalizeConfig(Config{LogPath: "my-log", IncludePrev: true}, time.Second)}
}

func TestOverlaySettingsPersistenceAndRuntimePreservation(t *testing.T) {
	s := overlayTestService(t)
	if got := s.OverlaySettings(); got != DefaultOverlaySettings() {
		t.Fatalf("defaults = %+v", got)
	}
	next := s.OverlaySettings()
	next.ShowDeck = false
	next.Opacity = 0.5
	next.Shortcut = "Ctrl+Alt+P"
	if _, err := s.UpdateOverlaySettings(next); err != nil {
		t.Fatal(err)
	}
	if s.Config().LogPath != "my-log" {
		t.Fatal("overlay update replaced runtime config")
	}
	if _, err := s.UpdateConfig(Config{LogPath: "new-log"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if *saved.Overlay != next || saved.LogPath != "new-log" {
		t.Fatalf("saved config = %+v", saved)
	}
	var old Config
	if err := json.Unmarshal([]byte(`{"logPath":"legacy"}`), &old); err != nil {
		t.Fatal(err)
	}
	old = normalizeConfig(old, time.Second)
	if *old.Overlay != DefaultOverlaySettings() {
		t.Fatal("legacy config lost defaults")
	}
}

func TestOverlaySettingsRejectInvalidAndUnavailableShortcut(t *testing.T) {
	for _, change := range []func(*OverlaySettings){
		func(s *OverlaySettings) { s.PanelSize = "huge" }, func(s *OverlaySettings) { s.Opacity = 0.2 },
		func(s *OverlaySettings) { s.HoverDelayMs = 1501 }, func(s *OverlaySettings) { s.Shortcut = "P" },
		func(s *OverlaySettings) { s.Shortcut = "Ctrl+Ctrl+P" },
	} {
		s := overlayTestService(t)
		invalid := s.OverlaySettings()
		change(&invalid)
		if _, err := s.UpdateOverlaySettings(invalid); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
		if s.OverlaySettings() != DefaultOverlaySettings() {
			t.Fatal("invalid update changed config")
		}
	}
	s := overlayTestService(t)
	s.overlayShortcutChanged = func(string) error { return errors.New("already in use") }
	next := s.OverlaySettings()
	next.Shortcut = "Ctrl+P"
	if _, err := s.UpdateOverlaySettings(next); err == nil {
		t.Fatal("accepted unavailable shortcut")
	}
	if s.OverlaySettings() != DefaultOverlaySettings() {
		t.Fatal("failed registration changed config")
	}
}

func TestOverlaySettingsDefaultFieldsMissingFromSavedConfig(t *testing.T) {
	var saved Config
	raw := `{"overlay":{"showDeck":false,"showOpponent":true,"panelSize":"large","opacity":0.5,"cardPreviews":true,"hoverDelayMs":100,"shortcut":"Ctrl+Alt+P"}}`
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	saved = normalizeConfig(saved, time.Second)
	got := *saved.Overlay
	if !got.DraftCardBadges || got.DraftBadgeLeft != 17.1 || got.DraftBadgeTop != 16.1 || got.DraftBadgeWidth != 7.1 || got.DraftBadgeHeight != 17.7 || got.DraftBadgeGapX != 1.25 || got.DraftBadgeGapY != 0.9 || got.DraftBadgeColumns != 8 {
		t.Fatalf("legacy config lost badge defaults: %+v", got)
	}
	if !got.ShowDraft {
		t.Fatal("showDraft saved before it existed should default to on")
	}
	if got.ShowDeck || got.PanelSize != "large" || got.Shortcut != "Ctrl+Alt+P" {
		t.Fatalf("saved values lost: %+v", got)
	}
}

func TestOverlayDraftBadgeCalibrationValidation(t *testing.T) {
	for _, field := range []func(*OverlaySettings, float64){
		func(s *OverlaySettings, v float64) { s.DraftBadgeLeft = v },
		func(s *OverlaySettings, v float64) { s.DraftBadgeTop = v },
		func(s *OverlaySettings, v float64) { s.DraftBadgeWidth = v },
		func(s *OverlaySettings, v float64) { s.DraftBadgeHeight = v },
		func(s *OverlaySettings, v float64) { s.DraftBadgeGapX = v },
		func(s *OverlaySettings, v float64) { s.DraftBadgeGapY = v },
	} {
		for _, invalid := range []float64{-1, 91, math.NaN(), math.Inf(1)} {
			settings := DefaultOverlaySettings()
			field(&settings, invalid)
			if err := ValidateOverlaySettings(settings); err == nil {
				t.Fatalf("accepted invalid calibration: %+v", settings)
			}
		}
	}
	for _, columns := range []int{0, 16} {
		settings := DefaultOverlaySettings()
		settings.DraftBadgeColumns = columns
		if err := ValidateOverlaySettings(settings); err == nil {
			t.Fatalf("accepted %d columns", columns)
		}
	}
	var settings OverlaySettings
	if err := json.Unmarshal([]byte(`{"showDraft":false,"draftCardBadges":false}`), &settings); err != nil {
		t.Fatal(err)
	}
	expected := DefaultOverlaySettings()
	expected.ShowDraft = false
	expected.DraftCardBadges = false
	if settings != expected {
		t.Fatalf("legacy/default calibration = %+v, want %+v", settings, expected)
	}
}

func TestOverlayDraftBadgePresetMigration(t *testing.T) {
	legacy := DefaultOverlaySettings()
	legacy.DraftBadgeLeft = 18.7
	legacy.DraftBadgeTop = 28.5
	legacy.DraftBadgeWidth = 7.9
	legacy.DraftBadgeHeight = 31.6
	legacy.DraftBadgeGapX = 1.3
	legacy.DraftBadgeGapY = 1.6
	legacy.DraftBadgeColumns = 8
	legacy.DraftCardBadges = false
	legacy.ShowDeck = false
	legacy.Shortcut = "Ctrl+Alt+P"
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "old preset", true: "custom position"}[custom], func(t *testing.T) {
			stored := legacy
			if custom {
				stored.DraftBadgeLeft = 19
			}
			raw, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			var got OverlaySettings
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			want := stored
			if !custom {
				want = DefaultOverlaySettings()
				want.DraftCardBadges = stored.DraftCardBadges
				want.ShowDeck = stored.ShowDeck
				want.Shortcut = stored.Shortcut
			}
			if got != want {
				t.Fatalf("decoded = %+v, want %+v", got, want)
			}
		})
	}
	var fresh OverlaySettings
	if err := json.Unmarshal([]byte(`{}`), &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh != DefaultOverlaySettings() {
		t.Fatalf("fresh defaults = %+v", fresh)
	}
}
