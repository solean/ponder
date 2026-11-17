package appstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
)

func TestAppIconPersistenceAndLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Store: db.NewStore(nil), DBPath: filepath.Join(dir, "test.db"), SupportDir: dir}
	var applied []string
	opts.AppIconChanged = func(value string) error { applied = append(applied, value); return nil }
	s, err := NewService(opts)
	if err != nil {
		t.Fatal(err)
	}
	if s.Config().AppIcon != "white" {
		t.Fatal("new installs should use white")
	}
	for _, choice := range []string{"blue", "charcoal", "white", "charcoal"} {
		cfg := s.Config()
		cfg.AppIcon = choice
		status, err := s.UpdateConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if status.Config.AppIcon != choice {
			t.Fatalf("status icon = %s", status.Config.AppIcon)
		}
		reloaded, err := NewService(opts)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Config().AppIcon != choice {
			t.Fatalf("reloaded icon = %s", reloaded.Config().AppIcon)
		}
	}
	if !reflect.DeepEqual(applied, []string{"blue", "charcoal", "white", "charcoal"}) {
		t.Fatalf("applied = %v", applied)
	}
	// An older client saving another preference must not erase the icon choice.
	cfg := s.Config()
	cfg.AppIcon, cfg.LogPath = "", "different.log"
	if _, err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if s.Config().AppIcon != "charcoal" || len(applied) != 4 {
		t.Fatal("unrelated save replaced the icon")
	}
	for _, stored := range []string{"", "unknown"} {
		if got := normalizeConfig(Config{AppIcon: stored}, time.Second).AppIcon; got != "white" {
			t.Fatalf("legacy icon = %s", got)
		}
	}
}

func TestAppIconFailureDoesNotSavePreference(t *testing.T) {
	for _, failure := range []string{"invalid", "unavailable", "native", "disk"} {
		t.Run(failure, func(t *testing.T) {
			s := overlayTestService(t)
			var applied []string
			s.appIconChanged = func(value string) error {
				applied = append(applied, value)
				if failure == "native" {
					return errors.New("image unavailable")
				}
				return nil
			}
			cfg := s.Config()
			cfg.AppIcon = "blue"
			switch failure {
			case "invalid":
				cfg.AppIcon = "green"
			case "unavailable":
				s.appIconChanged = nil
			case "disk":
				// A regular file cannot contain config.json; failure is reliable even as root.
				blocker := filepath.Join(t.TempDir(), "blocker")
				if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
					t.Fatal(err)
				}
				s.configPath = filepath.Join(blocker, "config.json")
			}
			if _, err := s.UpdateConfig(cfg); err == nil {
				t.Fatal("expected save error")
			}
			if s.Config().AppIcon != "white" {
				t.Fatal("failed update changed saved selection")
			}
			if failure == "disk" && !reflect.DeepEqual(applied, []string{"blue", "white"}) {
				t.Fatalf("rollback = %v", applied)
			}
			if failure == "invalid" && len(applied) != 0 {
				t.Fatal("invalid choice reached native code")
			}
			if raw, err := os.ReadFile(s.configPath); err == nil {
				var saved Config
				if err := json.Unmarshal(raw, &saved); err != nil {
					t.Fatal(err)
				}
				if saved.AppIcon == "blue" {
					t.Fatal("failed choice was written to disk")
				}
			}
		})
	}
}
