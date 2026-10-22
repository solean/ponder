package ingest

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
)

var testMST = time.FixedZone("MST", -7*60*60)

func dotNetTicks(t time.Time) int64 {
	return t.UnixNano()/100 + 621355968000000000
}

func TestParseLogClockAnchor(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 10, 22, 3, 2, 31, 251263900, time.UTC)
	cases := []string{
		fmt.Sprintf(`{ "transactionId": "t", "requestId": 1, "timestamp": "%d", "authenticateResponse": {} }`, dotNetTicks(want)),
		fmt.Sprintf(`  "timestamp": "%d",`, dotNetTicks(want)),
	}
	for _, line := range cases {
		got, ok := parseLogClockAnchor(line)
		if !ok || !got.Equal(want) {
			t.Fatalf("parseLogClockAnchor(%q) = %v, %v; want %v", line, got, ok, want)
		}
	}

	got, ok := parseLogClockAnchor(`{"timestamp":"1792638151440","greToClientEvent":{}}`)
	if !ok || !got.Equal(time.UnixMilli(1792638151440)) {
		t.Fatalf("unix ms anchor = %v, %v", got, ok)
	}
	for _, line := range []string{`{"timestamp":"12345"}`, `{"timestamp":1792638151440}`, `no anchor`} {
		if _, ok := parseLogClockAnchor(line); ok {
			t.Fatalf("parseLogClockAnchor(%q) accepted a non-anchor", line)
		}
	}
}

func TestObserveLogClockLearnsZonePerHeaderKind(t *testing.T) {
	t.Parallel()

	p := &Parser{headerLoc: testMST}
	state := &parseState{}
	anchor := time.Date(2026, 10, 22, 3, 2, 31, 0, time.UTC)
	steps := []struct {
		line string
		want string
	}{
		// Nothing learned yet: fall back to local time.
		{`[UnityCrossThreadLogger]10/22/2026 3:02:19 AM`, "2026-10-22T10:02:19Z"},
		{`[UnityCrossThreadLogger]10/21/2026 8:02:31 PM: Match to self: AuthenticateResponse`, "2026-10-22T03:02:31Z"},
		{fmt.Sprintf(`{ "requestId": 1, "timestamp": "%d" }`, dotNetTicks(anchor)), "2026-10-22T03:02:31Z"},
		// A bare header next to the anchor only fits the UTC reading.
		{`[UnityCrossThreadLogger]10/22/2026 3:02:40 AM`, "2026-10-22T03:02:40Z"},
		{`[UnityCrossThreadLogger]10/21/2026 8:03:00 PM: self to Match: ClientToGremessage`, "2026-10-22T03:03:00Z"},
		{`[UnityCrossThreadLogger]10/22/2026 3:04:00 AM`, "2026-10-22T03:04:00Z"},
	}
	for i, step := range steps {
		p.observeLogClock(state, int64(i+1), step.line)
		if state.lastUnityLogTimestamp != step.want {
			t.Fatalf("after line %d %q: timestamp = %q, want %q", i+1, step.line, state.lastUnityLogTimestamp, step.want)
		}
	}
	if got := p.headerZone(headerKindBare); got != headerZoneUTC {
		t.Fatalf("bare zone = %q, want utc", got)
	}
	if got := p.headerZone(headerKindSuffixed); got != headerZoneLocal {
		t.Fatalf("suffixed zone = %q, want local", got)
	}

	// A suffixed header whose anchor says UTC is restamped and relearned.
	later := time.Date(2026, 10, 22, 3, 10, 0, 0, time.UTC)
	p.observeLogClock(state, 10, `[UnityCrossThreadLogger]10/22/2026 3:10:00 AM: Match to self: GreToClientEvent`)
	p.observeLogClock(state, 11, fmt.Sprintf(`{"timestamp":"%d","greToClientEvent":{}}`, later.UnixMilli()))
	if state.lastUnityLogTimestamp != "2026-10-22T03:10:00Z" {
		t.Fatalf("restamped suffixed header = %q, want 2026-10-22T03:10:00Z", state.lastUnityLogTimestamp)
	}
	if got := p.headerZone(headerKindSuffixed); got != headerZoneUTC {
		t.Fatalf("suffixed zone = %q, want utc after UTC anchor", got)
	}
}

func TestObserveLogClockIgnoresDistantAnchors(t *testing.T) {
	t.Parallel()

	p := &Parser{headerLoc: testMST}
	state := &parseState{}
	anchor := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	p.observeLogClock(state, 1, fmt.Sprintf(`{"timestamp":"%d"}`, anchor.UnixMilli()))
	// Seven idle hours later a local header reads as the anchor's UTC wall
	// time; too many lines have passed to trust the coincidence.
	p.observeLogClock(state, 1+headerAnchorMaxLines+1, `[UnityCrossThreadLogger]10/22/2026 3:00:00 AM`)
	if got := p.headerZone(headerKindBare); got != headerZoneUnknown {
		t.Fatalf("bare zone = %q, want unknown", got)
	}
}

func TestParserResolvesHeadersBeforeTheFirstAnchor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	database, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	store := db.NewStore(database)

	anchor := time.Date(2026, 10, 22, 3, 2, 31, 0, time.UTC)
	prevPath := filepath.Join(tmpDir, "Player-prev.log")
	if err := writeLogLines(prevPath, []string{
		// Arena's startup headers come before the first anchor.
		`[UnityCrossThreadLogger]10/22/2026 3:00:00 AM`,
		`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"p0","request":"{\"DraftId\":\"draft-0\",\"GrpIds\":[901],\"Pack\":1,\"Pick\":1}"}`,
		`[UnityCrossThreadLogger]10/21/2026 8:02:31 PM: Match to self-user: AuthenticateResponse`,
		fmt.Sprintf(`{ "transactionId": "t1", "requestId": 1, "timestamp": "%d", "authenticateResponse": { "clientId": "self-user", "screenName":"Self" } }`, dotNetTicks(anchor)),
		`[UnityCrossThreadLogger]10/22/2026 3:02:40 AM`,
		`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"p1","request":"{\"DraftId\":\"draft-1\",\"GrpIds\":[1001],\"Pack\":1,\"Pick\":1}"}`,
	}, false); err != nil {
		t.Fatalf("write log: %v", err)
	}
	// The next log's first header comes before any anchor.
	logPath := filepath.Join(tmpDir, "Player.log")
	if err := writeLogLines(logPath, []string{
		`[UnityCrossThreadLogger]10/22/2026 4:00:00 AM`,
		`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"p2","request":"{\"DraftId\":\"draft-2\",\"GrpIds\":[2001],\"Pack\":1,\"Pick\":1}"}`,
	}, false); err != nil {
		t.Fatalf("write log: %v", err)
	}

	parser := NewParser(store)
	parser.headerLoc = testMST
	if _, err := parser.ParseFile(ctx, prevPath, false); err != nil {
		t.Fatalf("parse prev log: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse log: %v", err)
	}

	sessions, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("ListDraftSessions: %v", err)
	}
	want := map[string]string{"draft-0": "2026-10-22T03:00:00Z", "draft-1": "2026-10-22T03:02:40Z", "draft-2": "2026-10-22T04:00:00Z"}
	if len(sessions) != len(want) {
		t.Fatalf("sessions = %#v, want %d", sessions, len(want))
	}
	for _, session := range sessions {
		picks, err := store.ListDraftPicks(ctx, session.ID)
		if err != nil {
			t.Fatalf("ListDraftPicks: %v", err)
		}
		if len(picks) != 1 || session.DraftID == nil || picks[0].PickTs != want[*session.DraftID] {
			t.Fatalf("session %v picks = %#v, want pick_ts %q", session.DraftID, picks, want[*session.DraftID])
		}
	}
}
