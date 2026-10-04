package appstate

import (
	"encoding/json"
	"errors"
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
