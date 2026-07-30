package appstate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solean/ponder/internal/ai"
	"github.com/solean/ponder/internal/db"
)

func TestSupportDirPathUsesPonderName(t *testing.T) {
	base := t.TempDir()
	got := supportDirPath(base)
	want := filepath.Join(base, "ponder")
	if got != want {
		t.Fatalf("support dir = %q, want %q", got, want)
	}
}

func TestNormalizeConfigPreservesAISelection(t *testing.T) {
	defaults := normalizeConfig(Config{}, 2*time.Second)
	if defaults.AIProvider != ai.ProviderClaude || defaults.AIModel != ai.DefaultClaudeModel {
		t.Fatalf("default AI config = %q/%q", defaults.AIProvider, defaults.AIModel)
	}

	openAI := normalizeConfig(Config{AIProvider: " openai ", AIModel: " gpt-test "}, 2*time.Second)
	if openAI.AIProvider != ai.ProviderOpenAI || openAI.AIModel != "gpt-test" {
		t.Fatalf("OpenAI config = %q/%q", openAI.AIProvider, openAI.AIModel)
	}
}

func TestStartLiveInitialPassIncludesRetainedPreviousLog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ponder.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	currentLogPath := filepath.Join(tmpDir, "Player.log")
	previousLogPath := filepath.Join(tmpDir, "Player-prev.log")
	if err := os.WriteFile(currentLogPath, nil, 0o644); err != nil {
		t.Fatalf("write current log: %v", err)
	}
	previousLines := []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1785082945000","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"previous-log-match"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1785082945444","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_ConnectResp","systemSeatIds":[2],"connectResp":{"deckMessage":{"deckCards":[100,100,200],"sideboardCards":[300]}}},{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"previous-log-match","gameNumber":1,"stage":"GameStage_Start"}}}]}}`,
		`[UnityCrossThreadLogger]7/26/2026 9:26:47 AM: self-user to Match: ClientToGremessage`,
		`{"payload":{"type":"ClientMessageType_SubmitDeckResp","submitDeckResp":{"deck":{"deckCards":[100,200,300],"sideboardCards":[100]}}}}`,
		`{"timestamp":"1785083207407","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"previous-log-match","gameNumber":2,"stage":"GameStage_Start"}}}]}}`,
	}
	if err := os.WriteFile(previousLogPath, []byte(strings.Join(previousLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write previous log: %v", err)
	}

	service, err := NewService(Options{
		Store:               db.NewStore(database),
		DBPath:              dbPath,
		SupportDir:          filepath.Join(tmpDir, "support"),
		DefaultLogPath:      currentLogPath,
		DefaultPrevLogPath:  previousLogPath,
		DefaultPollInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.StartLive(); err != nil {
		t.Fatalf("start live: %v", err)
	}
	defer func() {
		_, _ = service.StopLive()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var snapshots int64
		err := database.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM match_game_deck_snapshots s
			JOIN matches m ON m.id = s.match_id
			WHERE m.arena_match_id = 'previous-log-match' AND s.game_number = 2
		`).Scan(&snapshots)
		if err != nil {
			t.Fatalf("count previous-log snapshots: %v", err)
		}
		if snapshots == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("previous log was not parsed during the initial live pass")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
