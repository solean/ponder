package appstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigReset reports that the saved config could not be read at startup, so
// the app is running on default settings. BackupPath holds the unreadable file
// so the old settings can still be recovered by hand; it is empty only when the
// backup itself failed.
type ConfigReset struct {
	BackupPath string `json:"backupPath,omitempty"`
	Reason     string `json:"reason"`
	At         string `json:"at"`
}

// backupCorruptConfig moves an unreadable config aside so the next settings
// save can't overwrite it. Moving it (rather than copying) also keeps the next
// launch from warning about the same file again. If the move fails, the raw
// bytes are written to the backup path instead.
func backupCorruptConfig(configPath string, raw []byte, parseErr error, now time.Time) *ConfigReset {
	reset := &ConfigReset{
		Reason: fmt.Sprintf("could not parse %s: %v", filepath.Base(configPath), parseErr),
		At:     formatTime(now),
	}
	backupPath := corruptConfigBackupPath(configPath, now)
	if err := os.Rename(configPath, backupPath); err == nil {
		reset.BackupPath = backupPath
		return reset
	}
	if err := WriteFileAtomic(backupPath, raw, 0o644); err != nil {
		reset.Reason += fmt.Sprintf("; backup failed: %v", err)
		return reset
	}
	reset.BackupPath = backupPath
	return reset
}

// corruptConfigBackupPath names the backup next to the config, keeping the
// extension so it still opens as JSON: config.json → config.corrupt-20261120-153000.json.
func corruptConfigBackupPath(configPath string, now time.Time) string {
	ext := filepath.Ext(configPath)
	stem := strings.TrimSuffix(configPath, ext)
	return stem + ".corrupt-" + now.Format("20060102-150405") + ext
}

// WriteFileAtomic writes data to a temporary file in the same directory and
// renames it over path, so a crash mid-write leaves either the old file or the
// new one, never a truncated mix.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(perm); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
