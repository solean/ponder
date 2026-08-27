package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

const testPayChangesJSON = `[{"Source":"EventPayEntry","SourceId":"guid-run-1","InventoryGold":-5000,"InventoryCustomTokens":{},"Boosters":[],"GrantedCards":[],"Vouchers":{}}]`

const testRewardChangesJSON = `[{"Source":"EventReward","SourceId":"guid-run-1","InventoryGems":200,"Boosters":[{"CollationId":100054,"SetCode":"FIN","Count":2}],"GrantedCards":[],"Vouchers":{}}]`

const testGrantChangesJSON = `[{"Source":"EventGrantCardPool","SourceId":"QuickDraft_FIN_20250619","InventoryGems":20,"GrantedCards":[{"GrpId":95920,"CardAdded":true,"SetCode":"FIN"},{"GrpId":95928,"SetCode":"FIN","VaultProgress":3},{"GrpId":95869,"SetCode":"FIN","Gems":20}]}]`

func TestDecodeEconomyChanges(t *testing.T) {
	t.Parallel()

	changes := DecodeEconomyChanges(testGrantChangesJSON)
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	change := changes[0]
	if change.Source != "EventGrantCardPool" || change.SourceID != "QuickDraft_FIN_20250619" {
		t.Fatalf("source = %q sourceID = %q", change.Source, change.SourceID)
	}
	if change.GemsDelta != 20 {
		t.Fatalf("gems delta = %d, want 20", change.GemsDelta)
	}
	if change.CardsGranted != 3 {
		t.Fatalf("cards granted = %d, want 3", change.CardsGranted)
	}
	if change.VaultProgressDelta != 3 {
		t.Fatalf("vault progress delta = %d, want 3", change.VaultProgressDelta)
	}

	pay := DecodeEconomyChanges(testPayChangesJSON)
	if len(pay) != 1 || pay[0].GoldDelta != -5000 {
		t.Fatalf("pay changes = %#v", pay)
	}
	reward := DecodeEconomyChanges(testRewardChangesJSON)
	if len(reward) != 1 || reward[0].GemsDelta != 200 {
		t.Fatalf("reward changes = %#v", reward)
	}
	if len(reward[0].BoostersDelta) != 1 || reward[0].BoostersDelta[0].SetCode != "FIN" || reward[0].BoostersDelta[0].Count != 2 {
		t.Fatalf("reward boosters = %#v", reward[0].BoostersDelta)
	}
}

func openEconomyTestDB(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return database, NewStore(database)
}

func TestDeriveEconomyTransactionsLinksEventRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, store := openEconomyTestDB(t)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.UpsertEventRunJoin(ctx, tx, "QuickDraft_FIN_20250619", "Gold", 5000, "2026-07-01T18:00:00Z"); err != nil {
		t.Fatalf("upsert event run: %v", err)
	}

	// The pay change lands two minutes after EventJoin: proximity link that
	// also records the pay GUID on the run.
	payID, inserted, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 10, EconomySnapshotRecord{
		ObservedAt:  "2026-07-01T18:02:00Z",
		ChangesJSON: testPayChangesJSON,
	})
	if err != nil || !inserted {
		t.Fatalf("insert pay snapshot: id=%d inserted=%v err=%v", payID, inserted, err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, payID, "2026-07-01T18:02:00Z", testPayChangesJSON); err != nil {
		t.Fatalf("derive pay transactions: %v", err)
	}

	// The reward arrives hours later, far outside the proximity window, and
	// must link exactly through the recorded pay GUID.
	rewardID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 20, EconomySnapshotRecord{
		ObservedAt:  "2026-07-02T09:00:00Z",
		ChangesJSON: testRewardChangesJSON,
	})
	if err != nil {
		t.Fatalf("insert reward snapshot: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, rewardID, "2026-07-02T09:00:00Z", testRewardChangesJSON); err != nil {
		t.Fatalf("derive reward transactions: %v", err)
	}

	// Card pool grants carry the event name directly.
	grantID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 30, EconomySnapshotRecord{
		ObservedAt:  "2026-07-01T18:03:00Z",
		ChangesJSON: testGrantChangesJSON,
	})
	if err != nil {
		t.Fatalf("insert grant snapshot: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, grantID, "2026-07-01T18:03:00Z", testGrantChangesJSON); err != nil {
		t.Fatalf("derive grant transactions: %v", err)
	}

	// Re-deriving must not duplicate rows.
	again, err := store.DeriveEconomyTransactions(ctx, tx, payID, "2026-07-01T18:02:00Z", testPayChangesJSON)
	if err != nil {
		t.Fatalf("re-derive pay transactions: %v", err)
	}
	if again != 0 {
		t.Fatalf("re-derive inserted %d rows, want 0", again)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	transactions, err := store.ListEconomyTransactions(ctx)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(transactions) != 3 {
		t.Fatalf("transactions = %d, want 3", len(transactions))
	}
	bySource := make(map[string]int)
	for index, txn := range transactions {
		bySource[txn.Source] = index
		if txn.EventName != "QuickDraft_FIN_20250619" {
			t.Fatalf("%s event name = %q, want QuickDraft_FIN_20250619", txn.Source, txn.EventName)
		}
	}
	if link := transactions[bySource["EventPayEntry"]].EventLink; link != "proximity" {
		t.Fatalf("pay link = %q, want proximity", link)
	}
	if link := transactions[bySource["EventReward"]].EventLink; link != "source_id" {
		t.Fatalf("reward link = %q, want source_id", link)
	}
	if link := transactions[bySource["EventGrantCardPool"]].EventLink; link != "event_name" {
		t.Fatalf("grant link = %q, want event_name", link)
	}

	runs, err := store.ListEventRunEconomies(ctx)
	if err != nil {
		t.Fatalf("list event run economies: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("event runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.EntryGold != -5000 {
		t.Fatalf("entry gold = %d, want -5000", run.EntryGold)
	}
	if run.RewardGems != 220 {
		t.Fatalf("reward gems = %d, want 220 (200 prize + 20 duplicate protection)", run.RewardGems)
	}
	if run.NetGold != -5000 || run.NetGems != 220 {
		t.Fatalf("net gold/gems = %d/%d, want -5000/220", run.NetGold, run.NetGems)
	}
	if run.RewardCards != 3 || run.RewardVaultProgress != 3 {
		t.Fatalf("reward cards/vault = %d/%d, want 3/3", run.RewardCards, run.RewardVaultProgress)
	}
	if len(run.RewardBoosters) != 1 || run.RewardBoosters[0].Count != 2 {
		t.Fatalf("reward boosters = %#v", run.RewardBoosters)
	}
	// The pay entry linked by proximity, so the run's confidence is inferred.
	if run.LinkConfidence != "inferred" {
		t.Fatalf("link confidence = %q, want inferred", run.LinkConfidence)
	}
}

func TestEventRunEconomiesFallBackToJoinEntryCost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, store := openEconomyTestDB(t)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.UpsertEventRunJoin(ctx, tx, "PremierDraft_TMT_20260303", "Gem", 1500, "2026-03-05T18:00:00Z"); err != nil {
		t.Fatalf("upsert event run: %v", err)
	}
	if err := store.UpsertEventRunJoin(ctx, tx, "Ladder", "None", 0, "2026-03-05T19:00:00Z"); err != nil {
		t.Fatalf("upsert ladder run: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	runs, err := store.ListEventRunEconomies(ctx)
	if err != nil {
		t.Fatalf("list event run economies: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("event runs = %d, want 1 (ladder omitted)", len(runs))
	}
	run := runs[0]
	if run.EntryGems != -1500 || run.NetGems != -1500 {
		t.Fatalf("entry/net gems = %d/%d, want -1500/-1500", run.EntryGems, run.NetGems)
	}
	if run.LinkConfidence != "none" {
		t.Fatalf("link confidence = %q, want none", run.LinkConfidence)
	}
}

func TestInitBackfillsEconomyTransactionsFromStoredSnapshots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, store := openEconomyTestDB(t)

	// Simulate a pre-ledger database: a snapshot exists with a Changes
	// payload but no derived transactions.
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.UpsertEventRunJoin(ctx, tx, "QuickDraft_FIN_20250619", "Gold", 5000, "2026-07-01T18:00:00Z"); err != nil {
		t.Fatalf("upsert event run: %v", err)
	}
	if _, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 10, EconomySnapshotRecord{
		ObservedAt:  "2026-07-01T18:02:00Z",
		ChangesJSON: testPayChangesJSON,
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := Init(ctx, database); err != nil {
		t.Fatalf("re-init db: %v", err)
	}

	transactions, err := store.ListEconomyTransactions(ctx)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("transactions after backfill = %d, want 1", len(transactions))
	}
	if transactions[0].GoldDelta != -5000 || transactions[0].EventName != "QuickDraft_FIN_20250619" {
		t.Fatalf("backfilled transaction = %#v", transactions[0])
	}
}

func TestRepeatedEventNameCreatesSeparateEconomyRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, store := openEconomyTestDB(t)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	eventName := "PremierDraft_HOB_20260811"
	if err := store.UpsertEventRunJoin(ctx, tx, eventName, "Gem", 1500, "2026-08-23T23:30:00Z"); err != nil {
		t.Fatalf("join first run: %v", err)
	}
	firstPay := `[{"Source":"EventPayEntry","SourceId":"run-1","InventoryGems":-1500,"Boosters":[],"GrantedCards":[]}]`
	firstReward := `[{"Source":"EventReward","SourceId":"run-1","InventoryGems":250,"Boosters":[],"GrantedCards":[]}]`
	firstPayID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 10, EconomySnapshotRecord{
		ObservedAt:  "2026-08-23T23:30:05Z",
		ChangesJSON: firstPay,
	})
	if err != nil {
		t.Fatalf("insert first pay: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, firstPayID, "2026-08-23T23:30:05Z", firstPay); err != nil {
		t.Fatalf("derive first pay: %v", err)
	}
	firstRewardID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 20, EconomySnapshotRecord{
		ObservedAt:  "2026-08-24T00:23:00Z",
		ChangesJSON: firstReward,
	})
	if err != nil {
		t.Fatalf("insert first reward: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, firstRewardID, "2026-08-24T00:23:00Z", firstReward); err != nil {
		t.Fatalf("derive first reward: %v", err)
	}
	if err := store.MarkEventRunClaimed(ctx, tx, eventName, "2026-08-24T00:23:00Z"); err != nil {
		t.Fatalf("claim first run: %v", err)
	}

	if err := store.UpsertEventRunJoin(ctx, tx, eventName, "Gem", 1500, "2026-08-26T21:55:00Z"); err != nil {
		t.Fatalf("join second run: %v", err)
	}
	secondPay := `[{"Source":"EventPayEntry","SourceId":"run-2","InventoryGems":-1500,"Boosters":[],"GrantedCards":[]}]`
	secondReward := `[{"Source":"EventReward","SourceId":"run-2","InventoryGems":1800,"Boosters":[],"GrantedCards":[]}]`
	secondPayID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 30, EconomySnapshotRecord{
		ObservedAt:  "2026-08-26T21:55:05Z",
		ChangesJSON: secondPay,
	})
	if err != nil {
		t.Fatalf("insert second pay: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, secondPayID, "2026-08-26T21:55:05Z", secondPay); err != nil {
		t.Fatalf("derive second pay: %v", err)
	}
	secondRewardID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 40, EconomySnapshotRecord{
		ObservedAt:  "2026-08-26T23:18:00Z",
		ChangesJSON: secondReward,
	})
	if err != nil {
		t.Fatalf("insert second reward: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, secondRewardID, "2026-08-26T23:18:00Z", secondReward); err != nil {
		t.Fatalf("derive second reward: %v", err)
	}
	if err := store.MarkEventRunClaimed(ctx, tx, eventName, "2026-08-26T23:18:00Z"); err != nil {
		t.Fatalf("claim second run: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	runs, err := store.ListEventRunEconomies(ctx)
	if err != nil {
		t.Fatalf("list event run economies: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("event runs = %d, want 2", len(runs))
	}
	if runs[0].NetGems != 300 || runs[1].NetGems != -1250 {
		t.Fatalf("net gems = %d, %d; want 300, -1250", runs[0].NetGems, runs[1].NetGems)
	}
}

func TestDraftSessionRepairsEntryObservedBeforeRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, store := openEconomyTestDB(t)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	pay := `[{"Source":"EventPayEntry","SourceId":"draft-run","InventoryGems":-1500,"Boosters":[],"GrantedCards":[]}]`
	payID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 10, EconomySnapshotRecord{
		ObservedAt:  "2026-08-26T21:55:23Z",
		ChangesJSON: pay,
	})
	if err != nil {
		t.Fatalf("insert pay: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, payID, "2026-08-26T21:55:23Z", pay); err != nil {
		t.Fatalf("derive pay: %v", err)
	}

	draftID := "draft-session"
	if _, err := store.EnsureDraftSession(
		ctx, tx, "PremierDraft_HOB_20260811", &draftID, false, "2026-08-26T21:55:23Z",
	); err != nil {
		t.Fatalf("ensure draft session: %v", err)
	}

	reward := `[{"Source":"EventReward","SourceId":"draft-run","InventoryGems":1800,"Boosters":[],"GrantedCards":[]}]`
	rewardID, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", 20, EconomySnapshotRecord{
		ObservedAt:  "2026-08-26T23:18:57Z",
		ChangesJSON: reward,
	})
	if err != nil {
		t.Fatalf("insert reward: %v", err)
	}
	if _, err := store.DeriveEconomyTransactions(ctx, tx, rewardID, "2026-08-26T23:18:57Z", reward); err != nil {
		t.Fatalf("derive reward: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	runs, err := store.ListEventRunEconomies(ctx)
	if err != nil {
		t.Fatalf("list event run economies: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("event runs = %d, want 1", len(runs))
	}
	if runs[0].EntryGems != -1500 || runs[0].RewardGems != 1800 || runs[0].NetGems != 300 {
		t.Fatalf("draft gems = entry %d reward %d net %d", runs[0].EntryGems, runs[0].RewardGems, runs[0].NetGems)
	}

	sessions, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("list draft sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Economy == nil {
		t.Fatalf("draft session economy = %#v", sessions)
	}
	if sessions[0].Economy.EntryGems != -1500 || sessions[0].Economy.RewardGems != 1800 {
		t.Fatalf("draft session gems = entry %d reward %d", sessions[0].Economy.EntryGems, sessions[0].Economy.RewardGems)
	}
}

func TestRepairEventRunInstancesSplitsRepeatedDraftSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, store := openEconomyTestDB(t)
	now := "2026-08-27T00:00:00Z"
	eventName := "PremierDraft_HOB_20260811"

	if _, err := database.ExecContext(ctx, `
		INSERT INTO event_runs (
			event_name, event_type, entry_currency_type, entry_currency_paid,
			status, started_at, wins, losses, updated_at
		) VALUES (?, 'premier_draft', 'Gem', 1500, 'claimed', ?, 8, 6, ?)
	`, eventName, "2026-08-23T23:30:00Z", now); err != nil {
		t.Fatalf("insert legacy aggregate: %v", err)
	}
	for _, draft := range []struct {
		id        string
		startedAt string
	}{
		{id: "draft-one", startedAt: "2026-08-23T23:30:05Z"},
		{id: "draft-two", startedAt: "2026-08-26T21:55:23Z"},
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO draft_sessions (
				event_name, draft_id, is_bot_draft, started_at, created_at, updated_at
			) VALUES (?, ?, 0, ?, ?, ?)
		`, eventName, draft.id, draft.startedAt, now, now); err != nil {
			t.Fatalf("insert draft session %s: %v", draft.id, err)
		}
	}

	if err := store.RepairEventRunInstances(ctx); err != nil {
		t.Fatalf("repair event run instances: %v", err)
	}

	var runs, attached int64
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(draft_session_id)
		FROM event_runs
		WHERE event_name = ?
	`, eventName).Scan(&runs, &attached); err != nil {
		t.Fatalf("count repaired event runs: %v", err)
	}
	if runs != 2 || attached != 2 {
		t.Fatalf("repaired runs = %d attached = %d, want 2 and 2", runs, attached)
	}
}
