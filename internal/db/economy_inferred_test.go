package db

import (
	"context"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestInferredEconomyTransactionsReconcileUnreportedBalances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, store := openEconomyTestDB(t)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	reward := `[{"Source":"EventReward","SourceId":"run-1","InventoryGems":1400,"Boosters":[{"CollationId":100062,"SetCode":"HOB","Count":3}],"GrantedCards":[]}]`
	pay := `[{"Source":"EventPayEntry","SourceId":"run-2","InventoryGems":-1500,"Boosters":[],"GrantedCards":[]}]`
	snapshots := []struct {
		path string
		line int64
		rec  EconomySnapshotRecord
	}{
		{"Player.log", 1, EconomySnapshotRecord{ObservedAt: "2026-09-28T23:46:49Z", SequenceID: 23, Gold: 2650, Gems: 6570, VaultProgress: 845,
			WildcardCommons: 387, WildcardUncommons: 406, WildcardRares: 58, WildcardMythics: 53, BoostersJSON: `[]`}},
		// Reported reward: fully explained, nothing inferred.
		{"Player.log", 2, EconomySnapshotRecord{ObservedAt: "2026-09-28T23:46:54Z", SequenceID: 25, Gold: 2650, Gems: 7970, VaultProgress: 845,
			WildcardCommons: 387, WildcardUncommons: 406, WildcardRares: 58, WildcardMythics: 53,
			BoostersJSON: `[{"CollationId":100062,"SetCode":"HOB","Count":3}]`, ChangesJSON: reward}},
		// The three packs were opened before this entry payment: +40 gems,
		// +2.5% vault, and wildcards appear only in the balance. The emptied
		// booster entry has no Count.
		{"Player.log", 3, EconomySnapshotRecord{ObservedAt: "2026-09-28T23:47:54Z", SequenceID: 29, Gold: 2650, Gems: 6510, VaultProgress: 870,
			WildcardCommons: 389, WildcardUncommons: 408, WildcardRares: 58, WildcardMythics: 53,
			BoostersJSON: `[{"CollationId":100062,"SetCode":"HOB"}]`, ChangesJSON: pay}},
		// The same observation re-read from Player-prev.log must not reconcile
		// as a reversal.
		{"Player-prev.log", 3, EconomySnapshotRecord{ObservedAt: "2026-09-28T23:47:54Z", SequenceID: 29, Gold: 2650, Gems: 6510, VaultProgress: 870,
			WildcardCommons: 389, WildcardUncommons: 408, WildcardRares: 58, WildcardMythics: 53,
			BoostersJSON: `[{"CollationId":100062,"SetCode":"HOB"}]`, ChangesJSON: pay}},
		// Win gold, plus a store gem bundle while more packs were opened
		// (vault moved): only the vault progress belongs to the packs.
		{"Player.log", 4, EconomySnapshotRecord{ObservedAt: "2026-09-29T00:11:26Z", SequenceID: 35, Gold: 2750, Gems: 9910, VaultProgress: 921,
			WildcardCommons: 389, WildcardUncommons: 408, WildcardRares: 58, WildcardMythics: 53,
			BoostersJSON: `[{"CollationId":100062,"SetCode":"HOB"}]`}},
	}
	for _, snapshot := range snapshots {
		id, _, err := store.InsertEconomySnapshot(ctx, tx, snapshot.path, snapshot.line, snapshot.rec)
		if err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
		if snapshot.path == "Player.log" {
			if _, err := store.DeriveEconomyTransactions(ctx, tx, id, snapshot.rec.ObservedAt, snapshot.rec.ChangesJSON); err != nil {
				t.Fatalf("derive transactions: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	ledger, err := store.ListEconomyLedger(ctx)
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	type row struct {
		source   string
		inferred bool
		gold     int64
		gems     int64
		vault    int64
	}
	got := make([]row, 0, len(ledger))
	for _, txn := range ledger {
		got = append(got, row{txn.Source, txn.Inferred, txn.GoldDelta, txn.GemsDelta, txn.VaultProgressDelta})
	}
	want := []row{
		{"EventReward", false, 0, 1400, 0},
		{InferredBoosterOpenSource, true, 0, 40, 25},
		{"EventPayEntry", false, 0, -1500, 0},
		{InferredBoosterOpenSource, true, 0, 0, 51},
		{InferredUnreportedSource, true, 100, 3400, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("ledger = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ledger[%d] = %+v, want %+v (full %+v)", index, got[index], want[index], got)
		}
	}

	packs := ledger[1]
	if packs.WildcardDeltas != (model.WildcardBalance{Common: 2, Uncommon: 2}) {
		t.Fatalf("pack wildcards = %+v", packs.WildcardDeltas)
	}
	if len(packs.Boosters) != 1 || packs.Boosters[0] != (model.EconomyBoosterCount{SetCode: "HOB", Count: -3}) {
		t.Fatalf("pack boosters = %+v", packs.Boosters)
	}
	history, err := store.ListEconomyHistory(ctx)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if boosters := history[len(history)-1].Boosters; len(boosters) != 0 {
		t.Fatalf("latest boosters = %+v, want none unopened", boosters)
	}
}
