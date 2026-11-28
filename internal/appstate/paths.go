package appstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultMTGALogPaths() (current, prev string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user home dir: %w", err)
	}
	base := filepath.Join(home, "Library", "Logs", "Wizards Of The Coast", "MTGA")
	current = filepath.Join(base, "Player.log")
	prev = filepath.Join(base, "Player-prev.log")
	return current, prev, nil
}

func ResolveParseLogPaths(explicitPath string, includePrev bool) ([]string, error) {
	explicitPath = strings.TrimSpace(explicitPath)
	if explicitPath != "" {
		return []string{explicitPath}, nil
	}

	current, prev, err := DefaultMTGALogPaths()
	if err != nil {
		return nil, err
	}

	candidates := make([]string, 0, 2)
	if includePrev {
		candidates = append(candidates, prev)
	}
	candidates = append(candidates, current)

	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			found = append(found, candidate)
			continue
		}
		if err != nil && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", candidate, err)
		}
	}

	if len(found) == 0 {
		return nil, ErrNoDefaultLogs
	}

	return found, nil
}
