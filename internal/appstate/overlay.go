package appstate

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type OverlaySettings struct {
	DraftCardBadges   bool    `json:"draftCardBadges"`
	DraftBadgeLeft    float64 `json:"draftBadgeLeft"`
	DraftBadgeTop     float64 `json:"draftBadgeTop"`
	DraftBadgeWidth   float64 `json:"draftBadgeWidth"`
	DraftBadgeHeight  float64 `json:"draftBadgeHeight"`
	DraftBadgeGapX    float64 `json:"draftBadgeGapX"`
	DraftBadgeGapY    float64 `json:"draftBadgeGapY"`
	DraftBadgeColumns int     `json:"draftBadgeColumns"`
	ShowDeck          bool    `json:"showDeck"`
	ShowOpponent      bool    `json:"showOpponent"`
	ShowDraft         bool    `json:"showDraft"`
	PanelSize         string  `json:"panelSize"`
	Opacity           float64 `json:"opacity"`
	CardPreviews      bool    `json:"cardPreviews"`
	HoverDelayMs      int     `json:"hoverDelayMs"`
	Shortcut          string  `json:"shortcut"`
}

func DefaultOverlaySettings() OverlaySettings {
	return OverlaySettings{
		DraftCardBadges:   true,
		DraftBadgeLeft:    17.1,
		DraftBadgeTop:     16.1,
		DraftBadgeWidth:   7.1,
		DraftBadgeHeight:  17.7,
		DraftBadgeGapX:    1.25,
		DraftBadgeGapY:    0.9,
		DraftBadgeColumns: 8,
		ShowDeck:          true,
		ShowOpponent:      true,
		ShowDraft:         true,
		PanelSize:         "default",
		Opacity:           0.9,
		CardPreviews:      true,
		HoverDelayMs:      250,
		Shortcut:          "CmdOrCtrl+Shift+O",
	}
}

// UnmarshalJSON fills fields missing from the JSON with their defaults, so a
// setting added later starts out at its default in configs saved before it.
func (s *OverlaySettings) UnmarshalJSON(data []byte) error {
	type plain OverlaySettings
	settings := plain(DefaultOverlaySettings())
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	// The first experimental grid was measured from a cropped screenshot.
	// Migrate only that exact preset, leaving calibrated layouts and toggles intact.
	if settings.DraftBadgeLeft == 18.7 && settings.DraftBadgeTop == 28.5 &&
		settings.DraftBadgeWidth == 7.9 && settings.DraftBadgeHeight == 31.6 &&
		settings.DraftBadgeGapX == 1.3 && settings.DraftBadgeGapY == 1.6 && settings.DraftBadgeColumns == 8 {
		defaults := DefaultOverlaySettings()
		settings.DraftBadgeLeft = defaults.DraftBadgeLeft
		settings.DraftBadgeTop = defaults.DraftBadgeTop
		settings.DraftBadgeWidth = defaults.DraftBadgeWidth
		settings.DraftBadgeHeight = defaults.DraftBadgeHeight
		settings.DraftBadgeGapX = defaults.DraftBadgeGapX
		settings.DraftBadgeGapY = defaults.DraftBadgeGapY
		settings.DraftBadgeColumns = defaults.DraftBadgeColumns
	}
	*s = OverlaySettings(settings)
	return nil
}

func ValidateOverlaySettings(settings OverlaySettings) error {
	for _, field := range []struct {
		name            string
		value, min, max float64
	}{
		{"draft badge left", settings.DraftBadgeLeft, 0, 90},
		{"draft badge top", settings.DraftBadgeTop, 0, 90},
		{"draft badge width", settings.DraftBadgeWidth, 1, 60},
		{"draft badge height", settings.DraftBadgeHeight, 1, 60},
		{"draft badge horizontal gap", settings.DraftBadgeGapX, 0, 20},
		{"draft badge vertical gap", settings.DraftBadgeGapY, 0, 20},
	} {
		if math.IsNaN(field.value) || math.IsInf(field.value, 0) || field.value < field.min || field.value > field.max {
			return fmt.Errorf("%s must be between %g and %g", field.name, field.min, field.max)
		}
	}
	if settings.DraftBadgeColumns < 1 || settings.DraftBadgeColumns > 15 {
		return fmt.Errorf("draft badge columns must be between 1 and 15")
	}

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
