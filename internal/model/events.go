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
