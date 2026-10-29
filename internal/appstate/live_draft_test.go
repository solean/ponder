package appstate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestLiveDraftUpdatesBeforeRegularPoll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	logPath := filepath.Join(dir, "Player.log")
	pack := `[UnityCrossThreadLogger]Draft.Notify {"draftId":"active","SelfPick":1,"SelfPack":1,"PackCards":"1001,1002"}`
	if err := os.WriteFile(logPath, []byte(pack+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	type update struct {
		draft *model.LiveDraft
		err   error
	}
	updates := make(chan update, 10)
	var service *Service
	service, err = NewService(Options{
		Store: store, DBPath: dbPath, SupportDir: filepath.Join(dir, "support"),
		DefaultLogPath: logPath, DefaultPrevLogPath: filepath.Join(dir, "Player-prev.log"),
		DefaultPollInterval: time.Hour,
		LiveDataChanged: func() {
			// The callback must see committed data and may read service state.
			_ = service.Status()
			draft, err := store.GetLiveDraft(ctx, time.Now())
			updates <- update{draft, err}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartLive(); err != nil {
		t.Fatal(err)
	}
	defer service.StopLive()
	next := func() *model.LiveDraft {
		t.Helper()
		select {
		case result := <-updates:
			if result.err != nil {
				t.Fatal(result.err)
			}
			return result.draft
		case <-time.After(2 * time.Second):
			t.Fatal("live change did not arrive before the regular poll")
			return nil
		}
	}
	appendLog := func(text string) {
		t.Helper()
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.WriteString(text); err != nil {
			t.Fatal(err)
		}
	}
	if draft := next(); draft == nil || draft.Waiting || len(draft.Pack) != 2 {
		t.Fatalf("initial pack = %+v", draft)
	}
	// Unchanged logs should not run the parser at the fast-check interval.
	lastTick := func() time.Time {
		service.mu.RLock()
		defer service.mu.RUnlock()
		return service.liveLastTickAt
	}
	initialTick := lastTick()
	time.Sleep(3 * liveLogCheckInterval)
	if !lastTick().Equal(initialTick) {
		t.Fatal("unchanged log was reparsed")
	}

	// A partially written pick must neither clear the pack nor be lost when
	// the newline arrives in a later write.
	appendLog(`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"pick-1","request":{"DraftId":"active","GrpIds":[1002],"Pack":1,"Pick":1}}`)
	time.Sleep(3 * liveLogCheckInterval)
	select {
	case result := <-updates:
		t.Fatalf("partial line published a draft update: %+v", result)
	default:
	}
	appendLog("\n")
	if draft := next(); draft == nil || !draft.Waiting || len(draft.Pack) != 0 {
		t.Fatalf("completed pick = %+v, want waiting with no cards", draft)
	}
	appendLog(`[UnityCrossThreadLogger]Draft.Notify {"draftId":"active","SelfPick":2,"SelfPack":1,"PackCards":"2001"}` + "\n")
	if draft := next(); draft == nil || draft.Waiting || draft.PickNumber != 2 || len(draft.Pack) != 1 || draft.Pack[0].CardID != 2001 {
		t.Fatalf("next pack = %+v", draft)
	}

	// Replacing Player.log must not leave the change checker watching the old
	// file. This also exercises the parser's rotation/restart handling.
	if err := os.Rename(logPath, filepath.Join(dir, "Player-prev.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(`[UnityCrossThreadLogger]Draft.Notify {"draftId":"new-draft","SelfPick":1,"SelfPack":1,"PackCards":"3001,3002"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if draft := next(); draft == nil || draft.Waiting || len(draft.Pack) != 2 || draft.Pack[0].CardID != 3001 {
		t.Fatalf("rotated log pack = %+v", draft)
	}
}
