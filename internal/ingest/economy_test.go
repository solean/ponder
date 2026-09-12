package ingest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/db"
)

func TestParserTracksEconomySnapshotsFromInventoryInfo(t *testing.T) {
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

	logPath := filepath.Join(tmpDir, "Player.log")
	lines := []string{
		`[UnityCrossThreadLogger]7/12/2026 11:40:38 AM`,
		`<== StartHook(request-1)`,
		`{"InventoryInfo":{"SeqId":4,"Changes":[{"Source":"QuestReward","SourceId":"quest-1"}],"Gems":1200,"Gold":3450,"TotalVaultProgress":487,"wcTrackPosition":3,"WildCardCommons":20,"WildCardUnCommons":18,"WildCardRares":7,"WildCardMythics":2,"CustomTokens":{"PlayInToken":1,"Token_JumpIn":2},"Boosters":[{"CollationId":100061,"SetCode":"TST"},{"CollationId":100061,"SetCode":"TST"}],"Vouchers":{"DraftToken":1},"Cosmetics":{"ArtStyles":[{"Id":"ignored"}]}},"Decks":{"ignored":{"MainDeck":[]}}}`,
	}
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log: %v", err)
	}

	store := db.NewStore(database)
	parser := NewParser(store)
	stats, err := parser.ParseFile(ctx, logPath, false)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}
	if stats.EconomySnapshots != 1 {
		t.Fatalf("EconomySnapshots = %d, want 1", stats.EconomySnapshots)
	}

	history, err := store.ListEconomyHistory(ctx)
	if err != nil {
		t.Fatalf("ListEconomyHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	snapshot := history[0]
	if snapshot.ObservedAt == "" {
		t.Fatal("ObservedAt is empty")
	}
	if snapshot.SequenceID != 4 || snapshot.Gold != 3450 || snapshot.Gems != 1200 {
		t.Fatalf("core balances = seq %d, gold %d, gems %d", snapshot.SequenceID, snapshot.Gold, snapshot.Gems)
	}
	if snapshot.VaultProgress != 487 || snapshot.WildcardTrackPosition != 3 {
		t.Fatalf("vault/track = %d/%d, want 487/3", snapshot.VaultProgress, snapshot.WildcardTrackPosition)
	}
	if snapshot.Wildcards.Common != 20 || snapshot.Wildcards.Uncommon != 18 ||
		snapshot.Wildcards.Rare != 7 || snapshot.Wildcards.Mythic != 2 {
		t.Fatalf("wildcards = %+v", snapshot.Wildcards)
	}
	if snapshot.CustomTokens["PlayInToken"] != 1 || snapshot.CustomTokens["Token_JumpIn"] != 2 {
		t.Fatalf("custom tokens = %#v", snapshot.CustomTokens)
	}
	if len(snapshot.Boosters) != 1 || snapshot.Boosters[0].SetCode != "TST" || snapshot.Boosters[0].Count != 2 {
		t.Fatalf("boosters = %#v, want two TST boosters", snapshot.Boosters)
	}
	if snapshot.Vouchers["DraftToken"] != 1 {
		t.Fatalf("vouchers = %#v", snapshot.Vouchers)
	}
	if len(snapshot.ChangeSources) != 1 || snapshot.ChangeSources[0] != "QuestReward" {
		t.Fatalf("change sources = %#v", snapshot.ChangeSources)
	}

	stats, err = parser.ParseFile(ctx, logPath, false)
	if err != nil {
		t.Fatalf("reparse file: %v", err)
	}
	if stats.EconomySnapshots != 0 {
		t.Fatalf("EconomySnapshots after reparse = %d, want 0", stats.EconomySnapshots)
	}
	history, err = store.ListEconomyHistory(ctx)
	if err != nil {
		t.Fatalf("ListEconomyHistory after reparse: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length after reparse = %d, want 1", len(history))
	}
}

func TestParserReplaysCourseIdentityAndAuthoritativeDraftRecord(t *testing.T) {
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
	parser := NewParser(store)
	logPath := filepath.Join(tmpDir, "Player.log")
	lines := []string{
		`[UnityCrossThreadLogger]9/8/2026 10:00:00 AM`,
		`[UnityCrossThreadLogger]==> EventJoin {"id":"join-1","request":{"EventName":"PremierDraft_HOB_20260811","EntryCurrencyType":"Gems","EntryCurrencyPaid":1500}}`,
		`<== EventJoin(join-1)`,
		`{"Course":{"CourseId":"course-1","InternalEventName":"PremierDraft_HOB_20260811","CurrentModule":"Draft","CurrentWins":0,"CurrentLosses":0},"InventoryInfo":{"SeqId":100,"Gems":8500,"Gold":5000,"Changes":[{"Source":"EventPayEntry","SourceId":"course-1","InventoryGems":-1500}]}}`,
		`[UnityCrossThreadLogger]9/8/2026 10:01:00 AM`,
		`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"pick-1","request":{"DraftId":"draft-1","GrpIds":[1001],"Pack":1,"Pick":1}}`,
		`[UnityCrossThreadLogger]9/8/2026 10:15:00 AM`,
		`[UnityCrossThreadLogger]==> DraftCompleteDraft {"id":"complete-1","request":{"EventName":"PremierDraft_HOB_20260811","IsBotDraft":false}}`,
		`[UnityCrossThreadLogger]==> EventSetDeckV3 {"id":"deck-1","request":{"EventName":"PremierDraft_HOB_20260811","Summary":{"DeckId":"deck-1","Name":"HOB Draft","Attributes":[]},"Deck":{"MainDeck":[{"cardId":1001,"quantity":1}]}}}`,
		`[UnityCrossThreadLogger]9/8/2026 12:00:00 PM`,
		`[UnityCrossThreadLogger]==> EventClaimPrize {"id":"claim-1","request":{"EventName":"PremierDraft_HOB_20260811"}}`,
		`<== EventClaimPrize(claim-1)`,
		`{"CourseId":"course-1","InternalEventName":"PremierDraft_HOB_20260811","CurrentModule":"Complete","CurrentWins":4,"CurrentLosses":3,"CourseDeckSummary":{"DeckId":"deck-1"},"InventoryInfo":{"SeqId":101,"Gems":9900,"Gold":5000,"Changes":[{"Source":"EventReward","SourceId":"course-1","InventoryGems":1400}]}}`,
		`[UnityCrossThreadLogger]9/8/2026 12:01:00 PM`,
		`[UnityCrossThreadLogger]==> EventJoin {"id":"join-2","request":{"EventName":"PremierDraft_HOB_20260811","EntryCurrencyType":"Gems","EntryCurrencyPaid":1500}}`,
		`<== EventJoin(join-2)`,
		`{"Course":{"CourseId":"course-2","InternalEventName":"PremierDraft_HOB_20260811","CurrentModule":"Draft","CurrentWins":0,"CurrentLosses":0},"DTO_InventoryInfo":{"SeqId":102,"Gems":8400,"Gold":5000,"Changes":[{"Source":"EventPayEntry","SourceId":"course-2","InventoryGems":-1500}]}}`,
		`[UnityCrossThreadLogger]9/8/2026 12:02:00 PM`,
		`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"pick-2","request":{"DraftId":"draft-2","GrpIds":[1002],"Pack":1,"Pick":1}}`,
		`[UnityCrossThreadLogger]9/8/2026 12:15:00 PM`,
		`[UnityCrossThreadLogger]==> DraftCompleteDraft {"id":"complete-2","request":{"EventName":"PremierDraft_HOB_20260811","IsBotDraft":false}}`,
		`[UnityCrossThreadLogger]==> EventSetDeckV3 {"id":"deck-2","request":{"EventName":"PremierDraft_HOB_20260811","Summary":{"DeckId":"deck-2","Name":"HOB Draft","Attributes":[]},"Deck":{"MainDeck":[{"cardId":1002,"quantity":1}]}}}`,
		`[UnityCrossThreadLogger]9/8/2026 2:00:00 PM`,
		`<== EventGetCoursesV2(courses-1)`,
		`{"Courses":[{"CourseId":"course-1","InternalEventName":"PremierDraft_HOB_20260811","CurrentModule":"Complete","CurrentWins":4,"CurrentLosses":3,"CourseDeckSummary":{"DeckId":"deck-1"}},{"CourseId":"course-2","InternalEventName":"PremierDraft_HOB_20260811","CurrentModule":"ClaimPrize","CurrentWins":2,"CurrentLosses":3,"CourseDeckSummary":{"DeckId":"deck-2"}}]}`,
	}
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatalf("write log: %v", err)
	}

	// Replaying the older zero-win joins must not regress course records or
	// attach either payment or the observed prize to the other draft.
	for pass := range 2 {
		if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
			t.Fatalf("parse pass %d: %v", pass, err)
		}
		runs, err := store.ListEventRunEconomies(ctx)
		if err != nil {
			t.Fatalf("list event runs: %v", err)
		}
		if len(runs) != 2 {
			t.Fatalf("pass %d: event runs = %d, want 2", pass, len(runs))
		}
		sessions, err := store.ListDraftSessions(ctx)
		if err != nil {
			t.Fatalf("list draft sessions: %v", err)
		}
		if len(sessions) != 2 {
			t.Fatalf("pass %d: draft sessions = %d, want 2", pass, len(sessions))
		}
		seen := make(map[string]bool)
		for _, session := range sessions {
			if session.DraftID == nil || session.Economy == nil {
				t.Fatalf("pass %d: missing draft identity/economy: %+v", pass, session)
			}
			draftID := *session.DraftID
			seen[draftID] = true
			wantWins, wantReward := int64(2), int64(0)
			switch draftID {
			case "draft-1":
				wantWins, wantReward = 4, 1400
			case "draft-2":
			default:
				t.Fatalf("unexpected draft ID %q", draftID)
			}
			if session.Wins == nil || session.Losses == nil || *session.Wins != wantWins || *session.Losses != 3 {
				t.Fatalf("pass %d: %s record = %v-%v, want %d-3", pass, draftID, session.Wins, session.Losses, wantWins)
			}
			run := session.Economy
			if run.Wins != wantWins || run.Losses != 3 || run.EntryGems != -1500 || run.RewardGems != wantReward || run.NetGems != wantReward-1500 {
				t.Fatalf("pass %d: %s economy = %+v", pass, draftID, run)
			}
			if run.RewardGold != 0 || run.RewardCards != 0 || len(run.RewardBoosters) != 0 {
				t.Fatalf("pass %d: fabricated rewards for %s: %+v", pass, draftID, run)
			}
		}
		if !seen["draft-1"] || !seen["draft-2"] {
			t.Fatalf("pass %d: draft identities = %v", pass, seen)
		}
		transactions, err := store.ListEconomyTransactions(ctx)
		if err != nil {
			t.Fatalf("list transactions: %v", err)
		}
		if len(transactions) != 3 {
			t.Fatalf("pass %d: transactions = %d, want 3", pass, len(transactions))
		}
		for _, transaction := range transactions {
			if transaction.EventLink != "source_id" {
				t.Fatalf("pass %d: non-exact transaction: %+v", pass, transaction)
			}
		}
		history, err := store.ListEconomyHistory(ctx)
		if err != nil {
			t.Fatalf("list economy history: %v", err)
		}
		if len(history) != 3 || history[len(history)-1].Gems != 8400 {
			t.Fatalf("pass %d: inventory snapshots = %+v, want three with latest gems 8400", pass, history)
		}
	}
}
