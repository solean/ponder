package model

// ActivityDayBoundary describes one browser-local calendar day as a half-open
// instant range. Explicit boundaries preserve browser timezone and DST rules.
type ActivityDayBoundary struct {
	Date  string `json:"date"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type DailyActivity struct {
	Date           string `json:"date"`
	Count          int64  `json:"count"`
	Wins           int64  `json:"wins"`
	Losses         int64  `json:"losses"`
	Unknown        int64  `json:"unknown"`
	TrackedSeconds int64  `json:"trackedSeconds"`
	TimedMatches   int64  `json:"timedMatches"`
	Constructed    int64  `json:"constructed"`
	Limited        int64  `json:"limited"`
}
