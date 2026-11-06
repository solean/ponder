package model

import (
	"regexp"
	"strings"
)

var (
	// Arena event names embed dates as bare YYYYMMDD tokens.
	eventDateToken = regexp.MustCompile(`^\d{8}$`)
	// Mirrors web/src/lib/events.ts; mixed-case event type words never match.
	eventSetCodeToken = regexp.MustCompile(`^[A-Z0-9]{2,5}$`)
)

// LimitedSetCode extracts the set from an Arena event name such as
// "QuickDraft_TMT_20260313" or "FIN_Quick_Draft"; empty means unknown.
func LimitedSetCode(eventName string) string {
	for _, token := range strings.Split(eventName, "_") {
		if eventDateToken.MatchString(token) {
			continue
		}
		if eventSetCodeToken.MatchString(token) {
			return token
		}
	}
	return ""
}

// EventCategory names an event's format, e.g. "Premier Draft" or "Quick
// Draft". It mirrors classify/parseEventName in web/src/lib/events.ts so a
// format the client filters by matches the same events here; keep the two in
// step. Unrecognized events fall back to the raw name with spaces.
func EventCategory(eventName string) string {
	value := strings.TrimSpace(eventName)
	if value == "" {
		return "Unknown"
	}
	if category := classifyEvent(strings.ReplaceAll(strings.ToLower(value), "_", "")); category != "" {
		return category
	}
	return strings.ReplaceAll(value, "_", " ")
}

// classifyEvent matches a lowercased, underscore-free event name. Order
// matters: more specific patterns come before broader ones.
func classifyEvent(normalized string) string {
	has := func(part string) bool { return strings.Contains(normalized, part) }
	switch {
	case has("traddraft") || has("traditionaldraft"):
		return "Traditional Draft"
	case has("quickdraft"):
		return "Quick Draft"
	case has("premierdraft"):
		return "Premier Draft"
	case has("botdraft"):
		return "Bot Draft"
	case has("playerdraft"):
		return "Player Draft"
	case has("draft"):
		return "Other Draft"
	case has("tradsealed") || has("traditionalsealed"):
		return "Traditional Sealed"
	case has("premiersealed"):
		return "Premier Sealed"
	case has("sealed"):
		return "Sealed"
	case has("jumpin"):
		return "Jump In"
	case has("tradladder") || has("traditionalladder"):
		return "Traditional Ladder"
	case has("ladder"):
		return "Ranked Ladder"
	case has("midweek") || strings.HasPrefix(normalized, "mwm"):
		return "Midweek Magic"
	case has("brawl"):
		return "Brawl"
	case normalized == "play":
		return "Play"
	}
	return ""
}
