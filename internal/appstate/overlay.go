package appstate

import (
	"fmt"
	"math"
	"strings"
)

type OverlaySettings struct {
	ShowDeck     bool    `json:"showDeck"`
	ShowOpponent bool    `json:"showOpponent"`
	PanelSize    string  `json:"panelSize"`
	Opacity      float64 `json:"opacity"`
	CardPreviews bool    `json:"cardPreviews"`
	HoverDelayMs int     `json:"hoverDelayMs"`
	Shortcut     string  `json:"shortcut"`
}

func DefaultOverlaySettings() OverlaySettings {
	return OverlaySettings{true, true, "default", 0.9, true, 250, "CmdOrCtrl+Shift+O"}
}

func ValidateOverlaySettings(settings OverlaySettings) error {
	if settings.PanelSize != "compact" && settings.PanelSize != "default" && settings.PanelSize != "large" {
		return fmt.Errorf("panel size must be compact, default, or large")
	}
	if math.IsNaN(settings.Opacity) || math.IsInf(settings.Opacity, 0) || settings.Opacity < 0.3 || settings.Opacity > 1 {
		return fmt.Errorf("opacity must be between 0.3 and 1")
	}
	if settings.HoverDelayMs < 0 || settings.HoverDelayMs > 1500 {
		return fmt.Errorf("hover delay must be between 0 and 1500 milliseconds")
	}
	parts := strings.Split(settings.Shortcut, "+")
	if len(parts) < 2 || len(parts) > 5 {
		return fmt.Errorf("shortcut must include a modifier and a key")
	}
	seen := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "CmdOrCtrl", "Ctrl", "Cmd", "Alt", "Shift", "Super":
		default:
			return fmt.Errorf("unsupported shortcut modifier: %s", part)
		}
		if seen[part] {
			return fmt.Errorf("duplicate shortcut modifier: %s", part)
		}
		seen[part] = true
	}
	key := parts[len(parts)-1]
	validKey := len(key) == 1 && ((key[0] >= 'A' && key[0] <= 'Z') || (key[0] >= '0' && key[0] <= '9'))
	if !validKey {
		return fmt.Errorf("shortcut key must be a letter A–Z or digit 0–9")
	}
	return nil
}

func (s *Service) OverlaySettings() OverlaySettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.config.Overlay
}

func (s *Service) UpdateOverlaySettings(next OverlaySettings) (OverlaySettings, error) {
	next.Shortcut = strings.TrimSpace(next.Shortcut)
	if err := ValidateOverlaySettings(next); err != nil {
		return s.OverlaySettings(), err
	}
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	previous := s.OverlaySettings()
	changed := next.Shortcut != previous.Shortcut && s.overlayShortcutChanged != nil
	if changed {
		if err := s.overlayShortcutChanged(next.Shortcut); err != nil {
			return previous, err
		}
	}
	cfg := s.Config()
	cfg.Overlay = &next
	if err := s.saveConfig(cfg); err != nil {
		if changed {
			if rollbackErr := s.overlayShortcutChanged(previous.Shortcut); rollbackErr != nil {
				return previous, fmt.Errorf("%v; restore shortcut: %w", err, rollbackErr)
			}
		}
		return previous, err
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return next, nil
}
