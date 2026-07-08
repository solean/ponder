package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cschnabel/mtgdata/internal/db"
	"github.com/cschnabel/mtgdata/internal/model"
)

func TestChooseGameResultUsesLastMatchingScope(t *testing.T) {
	results := []roomResultEntry{
		{Scope: "MatchScope_Game", WinningTeamID: 1, Reason: "ResultReason_Concede"},
		{Scope: "MatchScope_Game", WinningTeamID: 2, Reason: "ResultReason_Game"},
		{Scope: "MatchScope_Game", WinningTeamID: 2, Reason: "ResultReason_Concede"},
		{Scope: "MatchScope_Match", WinningTeamID: 2, Reason: "ResultReason_Concede"},
	}

	winningTeamID, reason := chooseGameResult(results)
	if winningTeamID != 2 {
		t.Fatalf("winningTeamID = %d, want 2", winningTeamID)
	}
	if reason != "Concede" {
		t.Fatalf("reason = %q, want Concede", reason)
	}
}

func TestParserPersistsLatestPlayerName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	store := db.NewStore(database)
	parser := NewParser(store)

	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"clientId":"self-user","screenName":"SelfRenamed"}`,
	}
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}

	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	playerName, err := store.PlayerName(ctx)
	if err != nil {
		t.Fatalf("PlayerName: %v", err)
	}
	if playerName != "SelfRenamed" {
		t.Fatalf("PlayerName = %q, want SelfRenamed", playerName)
	}
}

func TestTailParsePersistsStateAcrossResumeCalls(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))

	initialLines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-1"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"gameInfo":{"matchID":"match-1"},"turnInfo":{"phase":"Phase_Main1","turnNumber":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":101,"grpId":5001,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
	}

	if err := writeLogLines(logPath, initialLines, false); err != nil {
		t.Fatalf("write initial log lines: %v", err)
	}

	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatalf("first parse: %v", err)
	}

	nextLines := []string{
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"turnInfo":{"phase":"Phase_Main1","turnNumber":2},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":102,"grpId":5002,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
	}
	if err := writeLogLines(logPath, nextLines, true); err != nil {
		t.Fatalf("append log lines: %v", err)
	}

	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatalf("second parse: %v", err)
	}

	var plays int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM match_card_plays cp
		JOIN matches m ON m.id = cp.match_id
		WHERE m.arena_match_id = 'match-1'
	`).Scan(&plays); err != nil {
		t.Fatalf("count card plays: %v", err)
	}
	if plays != 2 {
		t.Fatalf("expected 2 card plays, got %d", plays)
	}

	var oppCards int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM match_opponent_card_instances oc
		JOIN matches m ON m.id = oc.match_id
		WHERE m.arena_match_id = 'match-1'
	`).Scan(&oppCards); err != nil {
		t.Fatalf("count opponent cards: %v", err)
	}
	if oppCards != 2 {
		t.Fatalf("expected 2 opponent card instances, got %d", oppCards)
	}
}

func TestParserStoresMatchRankSnapshotAcrossFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mtgdata.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))

	prevLog := filepath.Join(tempDir, "Player-prev.log")
	currentLog := filepath.Join(tempDir, "Player.log")

	prevContents := `{"PersonaId":"SELF123"}
{"timestamp":"1773367612385","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"matchId":"match-1","reservedPlayers":[{"userId":"OPP456","playerName":"Opponent","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"SELF123","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}]},"stateType":"MatchGameRoomStateType_MatchCompleted","finalMatchResult":{"matchId":"match-1","matchCompletedReason":"MatchCompletedReasonType_Success","resultList":[{"scope":"MatchScope_Match","result":"ResultType_WinLoss","winningTeamId":1,"reason":"ResultReason_Concede"}]}}}}`
	if err := os.WriteFile(prevLog, []byte(prevContents+"\n"), 0o644); err != nil {
		t.Fatalf("write prev log: %v", err)
	}

	currentContents := `[UnityCrossThreadLogger]3/12/2026 7:08:37 PM
<== RankGetCombinedRankInfo(req-1)
{"constructedSeasonOrdinal":87,"constructedLevel":3,"constructedStep":2,"constructedMatchesWon":2,"constructedMatchesLost":2,"limitedSeasonOrdinal":87,"limitedLevel":3,"limitedMatchesWon":2,"limitedMatchesLost":3}`
	if err := os.WriteFile(currentLog, []byte(currentContents+"\n"), 0o644); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	if _, err := parser.ParseFile(ctx, prevLog, false); err != nil {
		t.Fatalf("parse prev log: %v", err)
	}
	if _, err := parser.ParseFile(ctx, currentLog, false); err != nil {
		t.Fatalf("parse current log: %v", err)
	}

	var (
		matchID           string
		constructedLevel  sql.NullInt64
		constructedStep   sql.NullInt64
		constructedWins   sql.NullInt64
		constructedLosses sql.NullInt64
		limitedLevel      sql.NullInt64
		limitedWins       sql.NullInt64
		limitedLosses     sql.NullInt64
		observedAt        sql.NullString
	)
	err = database.QueryRowContext(ctx, `
		SELECT
			m.arena_match_id,
			mrs.constructed_level,
			mrs.constructed_step,
			mrs.constructed_matches_won,
			mrs.constructed_matches_lost,
			mrs.limited_level,
			mrs.limited_matches_won,
			mrs.limited_matches_lost,
			mrs.observed_at
		FROM match_rank_snapshots mrs
		JOIN matches m ON m.id = mrs.match_id
	`).Scan(
		&matchID,
		&constructedLevel,
		&constructedStep,
		&constructedWins,
		&constructedLosses,
		&limitedLevel,
		&limitedWins,
		&limitedLosses,
		&observedAt,
	)
	if err != nil {
		t.Fatalf("query rank snapshot: %v", err)
	}

	if matchID != "match-1" {
		t.Fatalf("match id = %q, want match-1", matchID)
	}
	if !constructedLevel.Valid || constructedLevel.Int64 != 3 {
		t.Fatalf("constructed level = %+v, want 3", constructedLevel)
	}
	if !constructedStep.Valid || constructedStep.Int64 != 2 {
		t.Fatalf("constructed step = %+v, want 2", constructedStep)
	}
	if !constructedWins.Valid || constructedWins.Int64 != 2 {
		t.Fatalf("constructed wins = %+v, want 2", constructedWins)
	}
	if !constructedLosses.Valid || constructedLosses.Int64 != 2 {
		t.Fatalf("constructed losses = %+v, want 2", constructedLosses)
	}
	if !limitedLevel.Valid || limitedLevel.Int64 != 3 {
		t.Fatalf("limited level = %+v, want 3", limitedLevel)
	}
	if !limitedWins.Valid || limitedWins.Int64 != 2 {
		t.Fatalf("limited wins = %+v, want 2", limitedWins)
	}
	if !limitedLosses.Valid || limitedLosses.Int64 != 3 {
		t.Fatalf("limited losses = %+v, want 3", limitedLosses)
	}
	if !observedAt.Valid || observedAt.String == "" {
		t.Fatalf("observed_at = %+v, want non-empty timestamp", observedAt)
	}
}

func TestBestOfThreeTimelineAndOpponentCountsAreGameAware(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-bo3.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))

	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-bo3"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"gameInfo":{"matchID":"match-bo3","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":2},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":101,"grpId":5001,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"gameInfo":{"matchID":"match-bo3","gameNumber":2},"turnInfo":{"phase":"Phase_Main1","turnNumber":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":101,"grpId":5001,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}

	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	detail, err := store.GetMatchDetail(ctx, 1)
	if err != nil {
		t.Fatalf("get match detail: %v", err)
	}

	if len(detail.CardPlays) != 2 {
		t.Fatalf("expected 2 card plays, got %d", len(detail.CardPlays))
	}
	if detail.CardPlays[0].GameNumber == nil || *detail.CardPlays[0].GameNumber != 1 {
		t.Fatalf("expected first card play in game 1, got %#v", detail.CardPlays[0].GameNumber)
	}
	if detail.CardPlays[1].GameNumber == nil || *detail.CardPlays[1].GameNumber != 2 {
		t.Fatalf("expected second card play in game 2, got %#v", detail.CardPlays[1].GameNumber)
	}

	if len(detail.OpponentObservedCards) != 1 {
		t.Fatalf("expected 1 observed opponent card, got %d", len(detail.OpponentObservedCards))
	}
	if detail.OpponentObservedCards[0].Quantity != 1 {
		t.Fatalf("expected observed quantity 1 (max per game), got %d", detail.OpponentObservedCards[0].Quantity)
	}
}

func TestParserIgnoresRankSnapshotWithoutCompletedMatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mtgdata.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	logPath := filepath.Join(tempDir, "Player.log")
	contents := `[UnityCrossThreadLogger]3/12/2026 7:08:37 PM
<== RankGetCombinedRankInfo(req-1)
{"constructedSeasonOrdinal":87,"constructedLevel":3,"constructedStep":2,"constructedMatchesWon":2,"constructedMatchesLost":2,"limitedSeasonOrdinal":87,"limitedLevel":3,"limitedMatchesWon":2,"limitedMatchesLost":3}`
	if err := os.WriteFile(logPath, []byte(contents+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse log: %v", err)
	}

	var count int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_rank_snapshots`).Scan(&count); err != nil {
		t.Fatalf("count rank snapshots: %v", err)
	}
	if count != 0 {
		t.Fatalf("rank snapshot count = %d, want 0", count)
	}
}

func TestParserBackfillsRankSnapshotForExistingCompletedMatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mtgdata.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	store := db.NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "match-1", "Traditional_Ladder", 2, "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("upsert match start: %v", err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "match-1", 2, 1, 28, 1140, "Concede", "2026-03-12T19:06:52Z"); err != nil {
		t.Fatalf("update match end: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded match: %v", err)
	}

	parser := NewParser(store)

	prevLog := filepath.Join(tempDir, "Player-prev.log")
	currentLog := filepath.Join(tempDir, "Player.log")

	prevContents := `{"PersonaId":"SELF123"}
{"timestamp":"1773367612385","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"matchId":"match-1","reservedPlayers":[{"userId":"OPP456","playerName":"Opponent","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"SELF123","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}]},"stateType":"MatchGameRoomStateType_MatchCompleted","finalMatchResult":{"matchId":"match-1","matchCompletedReason":"MatchCompletedReasonType_Success","resultList":[{"scope":"MatchScope_Match","result":"ResultType_WinLoss","winningTeamId":1,"reason":"ResultReason_Concede"}]}}}}`
	if err := os.WriteFile(prevLog, []byte(prevContents+"\n"), 0o644); err != nil {
		t.Fatalf("write prev log: %v", err)
	}

	currentContents := `[UnityCrossThreadLogger]3/12/2026 7:08:37 PM
<== RankGetCombinedRankInfo(req-1)
{"constructedSeasonOrdinal":87,"constructedLevel":3,"constructedStep":2,"constructedMatchesWon":2,"constructedMatchesLost":2,"limitedSeasonOrdinal":87,"limitedLevel":3,"limitedMatchesWon":2,"limitedMatchesLost":3}`
	if err := os.WriteFile(currentLog, []byte(currentContents+"\n"), 0o644); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	if _, err := parser.ParseFile(ctx, prevLog, false); err != nil {
		t.Fatalf("parse prev log: %v", err)
	}
	if _, err := parser.ParseFile(ctx, currentLog, false); err != nil {
		t.Fatalf("parse current log: %v", err)
	}

	var count int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_rank_snapshots`).Scan(&count); err != nil {
		t.Fatalf("count rank snapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("rank snapshot count = %d, want 1", count)
	}
}

func TestReplayFramesCaptureMultiCardStack(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-stack.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-stack"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-stack","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":1},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[]},{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[]}],"gameObjects":[]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Main1","turnNumber":1},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[501]}],"gameObjects":[{"instanceId":501,"grpId":9501,"type":"GameObjectType_Card","zoneId":27,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
		`{"timestamp":"1772330782311","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":3,"prevGameStateId":2,"turnInfo":{"phase":"Phase_Main1","turnNumber":1},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[501,502]}],"gameObjects":[{"instanceId":502,"grpId":9502,"type":"GameObjectType_Card","zoneId":27,"visibility":"Visibility_Public","ownerSeatId":2}]}}]}}`,
		`{"timestamp":"1772330782312","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":4,"prevGameStateId":3,"turnInfo":{"phase":"Phase_Main1","turnNumber":1},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[501,502,503]}],"gameObjects":[{"instanceId":503,"grpId":9503,"type":"GameObjectType_Card","zoneId":27,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 4 {
		t.Fatalf("expected 4 replay frames, got %d", len(frames))
	}

	lastFrame := frames[len(frames)-1]
	stackObjects := replayObjectsInZone(lastFrame, "stack")
	if len(stackObjects) != 3 {
		t.Fatalf("expected 3 stack objects in final frame, got %d", len(stackObjects))
	}
	if stackObjects[0].InstanceID != 501 || stackObjects[1].InstanceID != 502 || stackObjects[2].InstanceID != 503 {
		t.Fatalf("unexpected stack order in final frame: %#v", stackObjects)
	}
}

func TestReplayFramesTrackBoardRemovalEffects(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-removal.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-removal"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-removal","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":3},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[601,602]},{"zoneId":33,"type":"ZoneType_Graveyard","visibility":"Visibility_Public","ownerSeatId":1,"objectInstanceIds":[]}],"gameObjects":[{"instanceId":601,"grpId":9601,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1},{"instanceId":602,"grpId":9602,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":2}]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Main1","turnNumber":3},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[601]},{"zoneId":33,"type":"ZoneType_Graveyard","visibility":"Visibility_Public","ownerSeatId":1,"objectInstanceIds":[602]}],"gameObjects":[{"instanceId":602,"grpId":9602,"type":"GameObjectType_Card","zoneId":33,"visibility":"Visibility_Public","ownerSeatId":2}]}}]}}`,
		`{"timestamp":"1772330782311","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":3,"prevGameStateId":2,"turnInfo":{"phase":"Phase_Main1","turnNumber":3},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[]}],"diffDeletedInstanceIds":[601],"gameObjects":[]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 replay frames, got %d", len(frames))
	}

	secondFrame := frames[1]
	if len(replayObjectsInZone(secondFrame, "battlefield")) != 1 {
		t.Fatalf("expected 1 battlefield card after first removal, got %d", len(replayObjectsInZone(secondFrame, "battlefield")))
	}
	if len(replayObjectsInZone(secondFrame, "graveyard")) != 1 {
		t.Fatalf("expected 1 graveyard card after first removal, got %d", len(replayObjectsInZone(secondFrame, "graveyard")))
	}
	if !replayHasChange(secondFrame, "move_public", 602, "battlefield", "graveyard") {
		t.Fatalf("expected move_public change for card 602 in second frame, got %#v", secondFrame.Changes)
	}

	lastFrame := frames[2]
	if len(replayObjectsInZone(lastFrame, "battlefield")) != 0 {
		t.Fatalf("expected empty battlefield in final frame, got %d", len(replayObjectsInZone(lastFrame, "battlefield")))
	}
	if !replayHasChange(lastFrame, "leave_public", 601, "battlefield", "") {
		t.Fatalf("expected leave_public change for card 601 in final frame, got %#v", lastFrame.Changes)
	}
}

func TestReplayFramesDoNotDuplicateResolvedStackCards(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-resolve.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-resolve"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-resolve","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":2},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[701]},{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[702]}],"gameObjects":[{"instanceId":701,"grpId":9701,"type":"GameObjectType_Card","zoneId":27,"visibility":"Visibility_Public","ownerSeatId":2},{"instanceId":702,"grpId":9702,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":2}]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Main1","turnNumber":2},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public"},{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[701,702]}],"gameObjects":[{"instanceId":701,"grpId":9701,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":2}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 replay frames, got %d", len(frames))
	}

	lastFrame := frames[1]
	if len(lastFrame.Objects) != 2 {
		t.Fatalf("expected 2 public objects after resolution, got %d", len(lastFrame.Objects))
	}
	if len(replayObjectsInZone(lastFrame, "stack")) != 0 {
		t.Fatalf("expected empty stack after resolution, got %#v", replayObjectsInZone(lastFrame, "stack"))
	}
	if len(replayObjectsInZone(lastFrame, "battlefield")) != 2 {
		t.Fatalf("expected 2 battlefield cards after resolution, got %#v", replayObjectsInZone(lastFrame, "battlefield"))
	}
}

func TestReplayFramesCaptureBattlefieldTokens(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-token.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-token"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-token","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":3},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[]}],"gameObjects":[]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Main1","turnNumber":3},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[1001]}],"gameObjects":[{"instanceId":1001,"grpId":100662,"type":"GameObjectType_Token","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":2,"controllerSeatId":2,"cardTypes":["CardType_Artifact","CardType_Creature"],"subtypes":["SubType_Robot"],"power":{"value":1},"toughness":{"value":1},"hasSummoningSickness":true,"name":926665,"overlayGrpId":100662}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 replay frames, got %d", len(frames))
	}

	lastFrame := frames[1]
	battlefieldObjects := replayObjectsInZone(lastFrame, "battlefield")
	if len(battlefieldObjects) != 1 {
		t.Fatalf("expected 1 battlefield object, got %#v", battlefieldObjects)
	}

	token := battlefieldObjects[0]
	if token.InstanceID != 1001 {
		t.Fatalf("expected token instance 1001, got %#v", token)
	}
	if token.CardID != 100662 {
		t.Fatalf("expected token card id 100662, got %#v", token.CardID)
	}
	if !token.IsToken {
		t.Fatalf("expected replay object to be marked as token, got %#v", token)
	}
	if token.PlayerSide != "self" {
		t.Fatalf("expected controller-based player side self, got %q", token.PlayerSide)
	}
	if !token.HasSummoningSickness {
		t.Fatalf("expected token to preserve summoning sickness, got %#v", token)
	}
	if !replayHasAnyChange(lastFrame, "enter_public", 1001) {
		t.Fatalf("expected enter_public change for token 1001, got %#v", lastFrame.Changes)
	}

	var playCount int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_card_plays`).Scan(&playCount); err != nil {
		t.Fatalf("count match card plays: %v", err)
	}
	if playCount != 0 {
		t.Fatalf("expected tokens to stay out of match_card_plays, got %d rows", playCount)
	}
}

func TestReplayFramesCapturePermanentStateAndStateChanges(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-state.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-state"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-state","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":4},"players":[{"lifeTotal":20,"systemSeatNumber":1},{"lifeTotal":19,"systemSeatNumber":2}],"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[801,802]}],"gameObjects":[{"instanceId":801,"grpId":9801,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1,"controllerSeatId":1,"cardTypes":["CardType_Creature"],"power":{"value":1},"toughness":{"value":1}},{"instanceId":802,"grpId":9802,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":2,"controllerSeatId":2,"cardTypes":["CardType_Creature"],"power":{"value":2},"toughness":{"value":3}}]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Combat","step":"Step_DeclareAttack","turnNumber":4},"players":[{"lifeTotal":17,"systemSeatNumber":1},{"lifeTotal":18,"systemSeatNumber":2}],"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[801,802]}],"gameObjects":[{"instanceId":801,"grpId":9801,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1,"controllerSeatId":2,"cardTypes":["CardType_Creature"],"power":{"value":3},"toughness":{"value":3},"isTapped":true,"hasSummoningSickness":true,"attackState":"AttackState_Attacking","attackInfo":{"targetId":1},"counters":[{"counterType":"CounterType_P1P1","count":2}]},{"instanceId":802,"grpId":9802,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":2,"controllerSeatId":2,"cardTypes":["CardType_Creature"],"power":{"value":2},"toughness":{"value":3},"blockState":"BlockState_Declared","blockInfo":{"attackerIds":[801]}}],"annotations":[{"id":901,"affectedIds":[801],"type":["AnnotationType_TappedUntappedPermanent"],"details":[{"key":"tapped","type":"KeyValuePairValueType_int32","valueInt32":[1]}]}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 replay frames, got %d", len(frames))
	}

	lastFrame := frames[1]
	if lastFrame.SelfLifeTotal == nil || *lastFrame.SelfLifeTotal != 18 {
		t.Fatalf("expected self life total 18, got %#v", lastFrame.SelfLifeTotal)
	}
	if lastFrame.OpponentLifeTotal == nil || *lastFrame.OpponentLifeTotal != 17 {
		t.Fatalf("expected opponent life total 17, got %#v", lastFrame.OpponentLifeTotal)
	}
	var attacking model.MatchReplayFrameObjectRow
	var blocker model.MatchReplayFrameObjectRow
	for _, object := range replayObjectsInZone(lastFrame, "battlefield") {
		switch object.InstanceID {
		case 801:
			attacking = object
		case 802:
			blocker = object
		}
	}

	if attacking.InstanceID != 801 {
		t.Fatalf("expected attacking object 801 in battlefield, got %#v", attacking)
	}
	if attacking.PlayerSide != "self" {
		t.Fatalf("expected controller-based player side self, got %q", attacking.PlayerSide)
	}
	if !attacking.IsTapped || !attacking.HasSummoningSickness {
		t.Fatalf("expected tapped attacking creature with summoning sickness, got %#v", attacking)
	}
	if attacking.Power == nil || *attacking.Power != 3 || attacking.Toughness == nil || *attacking.Toughness != 3 {
		t.Fatalf("expected 3/3 stats, got %#v / %#v", attacking.Power, attacking.Toughness)
	}
	if attacking.AttackState != "attacking" || attacking.AttackTargetID == nil || *attacking.AttackTargetID != 1 {
		t.Fatalf("expected attacking state with target 1, got %#v", attacking)
	}
	if strings.TrimSpace(attacking.CounterSummaryJSON) == "" {
		t.Fatalf("expected counter summary json, got empty on %#v", attacking)
	}
	var counters []struct {
		Label string `json:"label"`
		Count int64  `json:"count"`
	}
	if err := json.Unmarshal([]byte(attacking.CounterSummaryJSON), &counters); err != nil {
		t.Fatalf("unmarshal counter summary: %v", err)
	}
	if len(counters) != 1 || counters[0].Label != "+1/+1" || counters[0].Count != 2 {
		t.Fatalf("unexpected counter summary: %#v", counters)
	}

	if blocker.InstanceID != 802 {
		t.Fatalf("expected blocker 802 in battlefield, got %#v", blocker)
	}
	if blocker.BlockState != "declared" || strings.TrimSpace(blocker.BlockAttackerIDsJSON) != "[801]" {
		t.Fatalf("expected declared blocker against attacker 801, got %#v", blocker)
	}

	if !replayHasAnyChange(lastFrame, "controller_change", 801) {
		t.Fatalf("expected controller_change for 801, got %#v", lastFrame.Changes)
	}
	if !replayHasAnyChange(lastFrame, "tap", 801) {
		t.Fatalf("expected tap change for 801, got %#v", lastFrame.Changes)
	}
	if !replayHasAnyChange(lastFrame, "attack", 801) {
		t.Fatalf("expected attack change for 801, got %#v", lastFrame.Changes)
	}
	if !replayHasAnyChange(lastFrame, "counters_change", 801) {
		t.Fatalf("expected counters_change for 801, got %#v", lastFrame.Changes)
	}
	if !replayHasAnyChange(lastFrame, "block", 802) {
		t.Fatalf("expected block change for 802, got %#v", lastFrame.Changes)
	}
}

func TestReplayFramesClearSummoningSicknessOnControllersNextTurn(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-summoning-sickness.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782400","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"self-user","playerName":"Self","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"opp-user","playerName":"Opp","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-summoning"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782401","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-summoning","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":1,"activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[901]}],"gameObjects":[{"instanceId":901,"grpId":9901,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1,"controllerSeatId":1,"cardTypes":["CardType_Creature"],"power":{"value":2},"toughness":{"value":2},"hasSummoningSickness":true}]}}]}}`,
		`{"timestamp":"1772330782402","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Main1","turnNumber":2,"activePlayer":2}}}]}}`,
		`{"timestamp":"1772330782403","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":3,"prevGameStateId":2,"turnInfo":{"phase":"Phase_Main1","turnNumber":3,"activePlayer":1}}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 replay frames, got %d", len(frames))
	}

	firstFrameObjects := replayObjectsInZone(frames[0], "battlefield")
	if len(firstFrameObjects) != 1 || !firstFrameObjects[0].HasSummoningSickness {
		t.Fatalf("expected object to enter with summoning sickness, got %#v", firstFrameObjects)
	}

	secondFrameObjects := replayObjectsInZone(frames[1], "battlefield")
	if len(secondFrameObjects) != 1 || !secondFrameObjects[0].HasSummoningSickness {
		t.Fatalf("expected object to stay summoning sick on opponent turn, got %#v", secondFrameObjects)
	}

	thirdFrameObjects := replayObjectsInZone(frames[2], "battlefield")
	if len(thirdFrameObjects) != 1 || thirdFrameObjects[0].HasSummoningSickness {
		t.Fatalf("expected object to lose summoning sickness on controller turn, got %#v", thirdFrameObjects)
	}
	if !replayHasAnyChange(frames[2], "summoning_sickness_change", 901) {
		t.Fatalf("expected summoning_sickness_change for 901, got %#v", frames[2].Changes)
	}
}

func TestReplayFramesCaptureGameResultMetadata(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-game-result.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782400","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"self-user","playerName":"Self","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"opp-user","playerName":"Opp","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-game-result"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782401","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-game-result","gameNumber":1,"stage":"GameStage_Play"},"turnInfo":{"phase":"Phase_Main1","turnNumber":3,"activePlayer":1},"players":[{"lifeTotal":20,"systemSeatNumber":1,"teamId":1},{"lifeTotal":20,"systemSeatNumber":2,"teamId":2}],"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[]}],"gameObjects":[]}}]}}`,
		`{"timestamp":"1772330782402","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"gameInfo":{"matchID":"match-replay-game-result","gameNumber":1,"stage":"GameStage_GameOver","results":[{"scope":"MatchScope_Game","result":"ResultType_WinLoss","winningTeamId":2,"reason":"ResultReason_Concede"}]},"turnInfo":{"phase":"Phase_Ending","step":"Step_End","turnNumber":8,"activePlayer":2},"players":[{"lifeTotal":0,"systemSeatNumber":1,"teamId":1},{"lifeTotal":7,"systemSeatNumber":2,"teamId":2}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 replay frames, got %d", len(frames))
	}

	lastFrame := frames[len(frames)-1]
	if lastFrame.GameStage != "gameover" {
		t.Fatalf("expected game stage gameover, got %q", lastFrame.GameStage)
	}
	if lastFrame.WinningPlayerSide != "opponent" {
		t.Fatalf("expected winning player side opponent, got %q", lastFrame.WinningPlayerSide)
	}
	if lastFrame.WinReason != "Concede" {
		t.Fatalf("expected win reason Concede, got %q", lastFrame.WinReason)
	}
	if lastFrame.SelfLifeTotal == nil || *lastFrame.SelfLifeTotal != 0 {
		t.Fatalf("expected self life total 0, got %#v", lastFrame.SelfLifeTotal)
	}
	if lastFrame.OpponentLifeTotal == nil || *lastFrame.OpponentLifeTotal != 7 {
		t.Fatalf("expected opponent life total 7, got %#v", lastFrame.OpponentLifeTotal)
	}
}

func TestReplayFramesTrackSelfHandOnly(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-replay-hand.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))
	lines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1773532594890","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"self-user","playerName":"Self","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"opp-user","playerName":"Opp","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-replay-hand"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1773532605936","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-replay-hand","gameNumber":1},"turnInfo":{"phase":"Phase_Main1","turnNumber":1,"activePlayer":1},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[]},{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[]},{"zoneId":31,"type":"ZoneType_Hand","visibility":"Visibility_Private","ownerSeatId":1,"objectInstanceIds":[101,102]},{"zoneId":35,"type":"ZoneType_Hand","visibility":"Visibility_Private","ownerSeatId":2,"objectInstanceIds":[201,202]}],"gameObjects":[{"instanceId":101,"grpId":90189,"type":"GameObjectType_Card","zoneId":31,"visibility":"Visibility_Private","ownerSeatId":1,"controllerSeatId":1,"superTypes":["SuperType_Basic"],"cardTypes":["CardType_Land"],"subtypes":["SubType_Swamp"]},{"instanceId":102,"grpId":87246,"type":"GameObjectType_Card","zoneId":31,"visibility":"Visibility_Private","ownerSeatId":1,"controllerSeatId":1,"cardTypes":["CardType_Creature"],"subtypes":["SubType_Bat"],"power":{"value":1},"toughness":{"value":1}}]}}]}}`,
		`{"timestamp":"1773532605937","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[1],"gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"turnInfo":{"phase":"Phase_Main1","turnNumber":1,"activePlayer":1},"zones":[{"zoneId":27,"type":"ZoneType_Stack","visibility":"Visibility_Public","objectInstanceIds":[102]},{"zoneId":31,"type":"ZoneType_Hand","visibility":"Visibility_Private","ownerSeatId":1,"objectInstanceIds":[101]}],"gameObjects":[{"instanceId":102,"grpId":87246,"type":"GameObjectType_Card","zoneId":27,"visibility":"Visibility_Public","ownerSeatId":1,"controllerSeatId":1,"cardTypes":["CardType_Creature"],"subtypes":["SubType_Bat"],"power":{"value":1},"toughness":{"value":1}}]}}]}}`,
	}

	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}
	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	store := db.NewStore(database)
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatalf("list replay frames: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 replay frames, got %d", len(frames))
	}

	firstHand := replayObjectsInZone(frames[0], "hand")
	if len(firstHand) != 2 {
		t.Fatalf("expected 2 self hand cards in first frame, got %#v", firstHand)
	}
	for _, object := range firstHand {
		if object.PlayerSide != "self" {
			t.Fatalf("expected self hand object, got %#v", object)
		}
		if object.Visibility != "private" {
			t.Fatalf("expected private hand visibility, got %#v", object)
		}
	}

	secondHand := replayObjectsInZone(frames[1], "hand")
	if len(secondHand) != 1 || secondHand[0].InstanceID != 101 {
		t.Fatalf("expected only remaining hand card 101, got %#v", secondHand)
	}
	secondStack := replayObjectsInZone(frames[1], "stack")
	if len(secondStack) != 1 || secondStack[0].InstanceID != 102 {
		t.Fatalf("expected 102 on the stack, got %#v", secondStack)
	}

	for _, frame := range frames {
		for _, object := range frame.Objects {
			if object.ZoneType == "hand" && object.PlayerSide != "self" {
				t.Fatalf("unexpected opponent hand object in replay frame: %#v", object)
			}
		}
	}
}

func replayObjectsInZone(frame model.MatchReplayFrameRow, zoneType string) []model.MatchReplayFrameObjectRow {
	out := make([]model.MatchReplayFrameObjectRow, 0)
	for _, obj := range frame.Objects {
		if obj.ZoneType == zoneType {
			out = append(out, obj)
		}
	}
	return out
}

func replayHasChange(frame model.MatchReplayFrameRow, action string, instanceID int64, fromZone, toZone string) bool {
	for _, change := range frame.Changes {
		if change.Action != action || change.InstanceID != instanceID {
			continue
		}
		if change.FromZoneType != fromZone {
			continue
		}
		if change.ToZoneType != toZone {
			continue
		}
		return true
	}
	return false
}

func replayHasAnyChange(frame model.MatchReplayFrameRow, action string, instanceID int64) bool {
	for _, change := range frame.Changes {
		if change.Action == action && change.InstanceID == instanceID {
			return true
		}
	}
	return false
}

func setDeckLogLine(t *testing.T, method, requestJSON string) string {
	t.Helper()
	envelope, err := json.Marshal(map[string]string{"id": "req-" + method, "request": requestJSON})
	if err != nil {
		t.Fatalf("marshal %s envelope: %v", method, err)
	}
	return "[UnityCrossThreadLogger]==> " + method + " " + string(envelope)
}

func TestParserIngestsEventSetDeckV3AndLinksMatchByDeckID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	logPath := filepath.Join(tmpDir, "Player.log")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	parser := NewParser(db.NewStore(database))

	lines := []string{
		// An older deck for the same event: the latest-by-event heuristic
		// would pick whichever deck was set most recently, so the match
		// below must link to the V3 deck by its exact arena deck id.
		setDeckLogLine(t, "EventSetDeckV2",
			`{"EventName":"Traditional_Ladder","Summary":{"DeckId":"deck-dimir","Name":"Dimir Mid 2026","Attributes":[{"name":"Format","value":"TraditionalStandard"}]},"Deck":{"MainDeck":[{"cardId":11,"quantity":4}],"Sideboard":[],"CommandZone":[],"Companions":[]}}`),
		setDeckLogLine(t, "EventSetDeckV3",
			`{"EventName":"Traditional_Ladder","Summary":{"DeckId":"deck-izzet","Name":"Izzet Prowess","Attributes":[{"name":"Format","value":"TraditionalStandard"}]},"Deck":{"MainDeck":[{"cardId":22,"quantity":4}],"Sideboard":[{"cardId":33,"quantity":2}],"CommandZone":[],"Companions":[]}}`),
		`{"timestamp":"1783485810381","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"self-user","playerName":"Self","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"opp-user","playerName":"Opp","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-izzet"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
	}
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log lines: %v", err)
	}

	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatalf("parse file: %v", err)
	}

	var deckName string
	var cardRows int64
	if err := database.QueryRowContext(ctx, `
		SELECT d.name, (SELECT COUNT(*) FROM deck_cards dc WHERE dc.deck_id = d.id)
		FROM decks d
		WHERE d.arena_deck_id = 'deck-izzet'
	`).Scan(&deckName, &cardRows); err != nil {
		t.Fatalf("query V3 deck: %v", err)
	}
	if deckName != "Izzet Prowess" {
		t.Fatalf("deck name = %q, want Izzet Prowess", deckName)
	}
	if cardRows != 2 {
		t.Fatalf("deck_cards rows = %d, want 2", cardRows)
	}

	var linkedArenaDeckID, linkReason string
	if err := database.QueryRowContext(ctx, `
		SELECT d.arena_deck_id, md.snapshot_reason
		FROM match_decks md
		JOIN matches m ON m.id = md.match_id
		JOIN decks d ON d.id = md.deck_id
		WHERE m.arena_match_id = 'match-izzet'
	`).Scan(&linkedArenaDeckID, &linkReason); err != nil {
		t.Fatalf("query match deck link: %v", err)
	}
	if linkedArenaDeckID != "deck-izzet" {
		t.Fatalf("linked deck = %q, want deck-izzet", linkedArenaDeckID)
	}
	if linkReason != "event_deck" {
		t.Fatalf("link reason = %q, want event_deck", linkReason)
	}
}

func writeLogLines(path string, lines []string, appendMode bool) error {
	if len(lines) == 0 {
		return nil
	}
	payload := strings.Join(lines, "\n") + "\n"
	if appendMode {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(payload)
		return err
	}
	return os.WriteFile(path, []byte(payload), 0o644)
}
