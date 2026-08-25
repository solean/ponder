package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// arenaDeckListEntry is one parsed card line from an Arena-format deck list.
type arenaDeckListEntry struct {
	Section         string
	Quantity        int64
	Name            string
	SetCode         string
	CollectorNumber string
}

// Matches "4 Card Name (SET) 123" with the set and collector number optional.
var arenaDeckLinePattern = regexp.MustCompile(`^(\d+)\s+(.+?)(?:\s+\(([0-9A-Za-z]+)\)\s+(\S+))?$`)

var arenaSectionHeaders = map[string]string{
	"deck":      "main",
	"main":      "main",
	"maindeck":  "main",
	"mainboard": "main",
	"sideboard": "sideboard",
	// Commander and companion sections are not modeled yet; keep their cards
	// rather than dropping them on import.
	"commander": "main",
	"companion": "sideboard",
}

// parseArenaDeckList parses Arena's clipboard deck-list format. It returns
// the deck name when an About section provides one, the card entries, and any
// lines that could not be understood.
func parseArenaDeckList(text string) (deckName string, entries []arenaDeckListEntry, malformed []string) {
	section := "main"
	inAbout := false

	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			inAbout = false
			continue
		}

		lower := strings.ToLower(line)
		if lower == "about" {
			inAbout = true
			continue
		}
		if mapped, ok := arenaSectionHeaders[lower]; ok {
			section = mapped
			inAbout = false
			continue
		}
		if inAbout {
			if name, ok := strings.CutPrefix(line, "Name "); ok {
				deckName = strings.TrimSpace(name)
			}
			continue
		}

		match := arenaDeckLinePattern.FindStringSubmatch(line)
		if match == nil {
			malformed = append(malformed, line)
			continue
		}
		quantity, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || quantity <= 0 {
			malformed = append(malformed, line)
			continue
		}
		entries = append(entries, arenaDeckListEntry{
			Section:         section,
			Quantity:        quantity,
			Name:            strings.TrimSpace(match[2]),
			SetCode:         strings.ToUpper(strings.TrimSpace(match[3])),
			CollectorNumber: strings.TrimSpace(match[4]),
		})
	}

	return deckName, entries, malformed
}

// formatArenaDeckList renders project cards as an Arena-importable deck list,
// preserving set and collector number when known. Cards without a resolvable
// name cannot be exported and are returned as unresolved Arena IDs.
func formatArenaDeckList(cards []model.DeckProjectCard) (string, []int64) {
	var main, sideboard []string
	var unresolved []int64

	for _, card := range cards {
		if strings.TrimSpace(card.Name) == "" {
			unresolved = append(unresolved, card.ArenaID)
			continue
		}
		line := fmt.Sprintf("%d %s", card.Quantity, card.Name)
		if card.SetCode != "" && card.CollectorNumber != "" {
			line = fmt.Sprintf("%s (%s) %s", line, card.SetCode, card.CollectorNumber)
		}
		if card.Section == "sideboard" {
			sideboard = append(sideboard, line)
		} else {
			main = append(main, line)
		}
	}

	var out strings.Builder
	out.WriteString("Deck\n")
	for _, line := range main {
		out.WriteString(line)
		out.WriteString("\n")
	}
	if len(sideboard) > 0 {
		out.WriteString("\nSideboard\n")
		for _, line := range sideboard {
			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	return out.String(), unresolved
}

// pickPrintingForImport chooses a deterministic Arena printing for an
// imported card line: an exact set + collector match wins, then any printing
// from the requested set, then the newest printing overall (highest Arena
// ID), preferring non-digital-only printings at each step.
func pickPrintingForImport(printings []model.CardDefinition, setCode, collectorNumber string) (model.CardDefinition, bool) {
	if len(printings) == 0 {
		return model.CardDefinition{}, false
	}

	setCode = strings.ToUpper(strings.TrimSpace(setCode))
	collectorNumber = strings.TrimSpace(collectorNumber)

	if setCode != "" && collectorNumber != "" {
		for _, printing := range printings {
			if printing.SetCode == setCode && printing.CollectorNumber == collectorNumber {
				return printing, true
			}
		}
	}

	best := func(candidates []model.CardDefinition) (model.CardDefinition, bool) {
		var chosen model.CardDefinition
		found := false
		for _, printing := range candidates {
			if !found {
				chosen = printing
				found = true
				continue
			}
			if chosen.IsDigitalOnly != printing.IsDigitalOnly {
				if chosen.IsDigitalOnly {
					chosen = printing
				}
				continue
			}
			if printing.ArenaID > chosen.ArenaID {
				chosen = printing
			}
		}
		return chosen, found
	}

	if setCode != "" {
		var sameSet []model.CardDefinition
		for _, printing := range printings {
			if printing.SetCode == setCode {
				sameSet = append(sameSet, printing)
			}
		}
		if chosen, ok := best(sameSet); ok {
			return chosen, true
		}
	}

	return best(printings)
}
