package appstate

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
)

func newConfigTestService(t *testing.T, configPath string) *Service {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ponder.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	service, err := NewService(Options{
		Store:               db.NewStore(database),
		DBPath:              dbPath,
		SupportDir:          filepath.Dir(configPath),
		ConfigPath:          configPath,
		DefaultLogPath:      filepath.Join(tmpDir, "Player.log"),
		DefaultPrevLogPath:  filepath.Join(tmpDir, "Player-prev.log"),
		DefaultPollInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func TestNewServiceBacksUpCorruptConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	damaged := []byte(`{"logPath": "/custom/Player.log", "autoStartLive": fal`)
	if err := os.WriteFile(configPath, damaged, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	service := newConfigTestService(t, configPath)

	status := service.Status()
	reset := status.ConfigReset
	if reset == nil {
		t.Fatal("status should report the config reset")
	}
	if reset.BackupPath == "" || filepath.Dir(reset.BackupPath) != filepath.Dir(configPath) {
		t.Fatalf("backup path = %q, want a file next to %q", reset.BackupPath, configPath)
	}
	if filepath.Ext(reset.BackupPath) != ".json" {
		t.Fatalf("backup path %q should keep the .json extension", reset.BackupPath)
	}
	backup, err := os.ReadFile(reset.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != string(damaged) {
		t.Fatalf("backup = %q, want the damaged bytes %q", backup, damaged)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("damaged config should be moved aside, stat err = %v", err)
	}
	if !slices.Contains(status.RevealablePaths(), reset.BackupPath) {
		t.Fatal("backup path should be revealable")
	}
	if service.Config().LogPath != "" || !service.Config().AutoStartLive {
		t.Fatalf("config should fall back to defaults, got %+v", service.Config())
	}

	// The next settings change writes a fresh config and leaves the backup alone.
	cfg := service.Config()
	cfg.IncludePrev = false
	if _, err := service.UpdateConfig(cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}
	if after, err := os.ReadFile(reset.BackupPath); err != nil || string(after) != string(damaged) {
		t.Fatalf("backup changed after save: %q, %v", after, err)
	}

	if service.DismissConfigReset().ConfigReset != nil {
		t.Fatal("dismiss should clear the notice")
	}
	if _, err := os.Stat(reset.BackupPath); err != nil {
		t.Fatalf("dismiss should keep the backup: %v", err)
	}

	// The saved config now parses, so a relaunch shows no notice.
	relaunched := newConfigTestService(t, configPath)
	if relaunched.Status().ConfigReset != nil {
		t.Fatal("relaunch after a good save should not report a reset")
	}
	if relaunched.Config().IncludePrev {
		t.Fatal("relaunch should load the saved setting")
	}
}

func TestNewServiceWithValidConfigReportsNoReset(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"logPath": "/custom/Player.log"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	service := newConfigTestService(t, configPath)
	if service.Status().ConfigReset != nil {
		t.Fatal("valid config should not report a reset")
	}
	if service.Config().LogPath != "/custom/Player.log" {
		t.Fatalf("log path = %q", service.Config().LogPath)
	}
}

func TestWriteFileAtomicReplacesFileAndCleansUp(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "config.json")
	if err := WriteFileAtomic(path, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second\n" {
		t.Fatalf("contents = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("temp files left behind: %v", names)
	}
}

func TestCorruptConfigBackupPath(t *testing.T) {
	now := time.Date(2026, 11, 20, 15, 30, 0, 0, time.UTC)
	got := corruptConfigBackupPath("/support/config.json", now)
	if want := "/support/config.corrupt-20261120-153000.json"; got != want {
		t.Fatalf("backup path = %q, want %q", got, want)
	}
}
