package model

import "testing"

func TestEventCategory(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"PremierDraft_HOB_20260811":  "Premier Draft",
		"QuickDraft_TMT_20260313":    "Quick Draft",
		"FIN_Quick_Draft":            "Quick Draft",
		"TradDraft_FRA_20260929":     "Traditional Draft",
		"BotDraft_FIN":               "Bot Draft",
		"Draft":                      "Other Draft",
		"Sealed_TMT_20260313":        "Sealed",
		"PremierSealed_TMT_20260313": "Premier Sealed",
		"Traditional_Ladder":         "Traditional Ladder",
		"Ladder":                     "Ranked Ladder",
		"Play":                       "Play",
		"Some_New_Event":             "Some New Event",
		"":                           "Unknown",
	}
	for eventName, want := range cases {
		if got := EventCategory(eventName); got != want {
			t.Errorf("EventCategory(%q) = %q, want %q", eventName, got, want)
		}
	}
}
