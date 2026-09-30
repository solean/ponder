package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// Arena applies some inventory changes without logging a Changes entry. The
// notable one is booster opening: set-completion gems, duplicate vault
// progress, and wildcards only surface in the next InventoryInfo balance.
// These sources label the part of a balance movement that the snapshot's own
// Changes do not explain.
const (
	InferredBoosterOpenSource = "BoosterOpen"
	InferredUnreportedSource  = "Unreported"
)

type economyBalanceSnapshot struct {
	id          int64
	observedAt  string
	sequenceID  int64
	gold        int64
	gems        int64
	vault       int64
	wildcards   model.WildcardBalance
	boosters    map[string]int64
	changesJSON string
}

// ListEconomyLedger returns the recorded transaction ledger with inferred
// rows for unreported balance movements merged in chronologically.
func (s *Store) ListEconomyLedger(ctx context.Context) ([]model.EconomyTransaction, error) {
	recorded, err := s.ListEconomyTransactions(ctx)
	if err != nil {
		return nil, err
	}
	inferred, err := s.ListInferredEconomyTransactions(ctx)
	if err != nil {
		return nil, err
	}
	return mergeEconomyLedger(recorded, inferred), nil
}

// ListInferredEconomyTransactions reconciles consecutive inventory snapshots
// against their reported Changes and returns a row for every unexplained
// residual. Derived at read time, so it needs no backfill and stays correct
// when log files are ingested out of order.
func (s *Store) ListInferredEconomyTransactions(ctx context.Context) ([]model.EconomyTransaction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			observed_at,
			sequence_id,
			gold,
			gems,
			vault_progress,
			wildcard_commons,
			wildcard_uncommons,
			wildcard_rares,
			wildcard_mythics,
			boosters_json,
			changes_json
		FROM economy_snapshots
		WHERE COALESCE(observed_at, '') != ''
		ORDER BY observed_at ASC, sequence_id ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list economy balances: %w", err)
	}
	defer rows.Close()

	snapshots := make([]economyBalanceSnapshot, 0)
	// A rotated Player.log is re-read as Player-prev.log, storing the same
	// observation twice; a duplicate would reconcile as a phantom reversal.
	seen := make(map[string]struct{})
	for rows.Next() {
		var snapshot economyBalanceSnapshot
		var boostersJSON string
		if err := rows.Scan(
			&snapshot.id,
			&snapshot.observedAt,
			&snapshot.sequenceID,
			&snapshot.gold,
			&snapshot.gems,
			&snapshot.vault,
			&snapshot.wildcards.Common,
			&snapshot.wildcards.Uncommon,
			&snapshot.wildcards.Rare,
			&snapshot.wildcards.Mythic,
			&boostersJSON,
			&snapshot.changesJSON,
		); err != nil {
			return nil, fmt.Errorf("scan economy balance: %w", err)
		}
		key := fmt.Sprintf("%s|%d", snapshot.observedAt, snapshot.sequenceID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		snapshot.boosters = boosterCountMap(decodeInventoryBoosters(boostersJSON))
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate economy balances: %w", err)
	}

	out := make([]model.EconomyTransaction, 0)
	for index := 1; index < len(snapshots); index++ {
		out = append(out, inferEconomyResidual(snapshots[index-1], snapshots[index])...)
	}
	return out, nil
}

// A booster yields at most a few dozen set-completion gems, while the
// smallest store gem bundle (also unlogged) is 750. Larger gains are never
// attributed to packs.
const inferredPackGemsLimit = 750

// inferEconomyResidual splits the unexplained movement between two snapshots
// into at most two rows. Pack-screen activity (packs consumed or vault
// progress moved; only card grants move the vault) claims the gains a pack can
// produce: gems, wildcards, vault progress, and the consumed packs. Everything
// else, including all gold, is left unattributed.
func inferEconomyResidual(previous, current economyBalanceSnapshot) []model.EconomyTransaction {
	gold := current.gold - previous.gold
	gems := current.gems - previous.gems
	vault := current.vault - previous.vault
	wildcards := model.WildcardBalance{
		Common:   current.wildcards.Common - previous.wildcards.Common,
		Uncommon: current.wildcards.Uncommon - previous.wildcards.Uncommon,
		Rare:     current.wildcards.Rare - previous.wildcards.Rare,
		Mythic:   current.wildcards.Mythic - previous.wildcards.Mythic,
	}
	boosters := make(map[string]int64)
	for setCode, count := range current.boosters {
		boosters[setCode] += count
	}
	for setCode, count := range previous.boosters {
		boosters[setCode] -= count
	}
	for _, change := range DecodeEconomyChanges(current.changesJSON) {
		gold -= change.GoldDelta
		gems -= change.GemsDelta
		vault -= change.VaultProgressDelta
		wildcards.Common -= change.WildcardDeltas.Common
		wildcards.Uncommon -= change.WildcardDeltas.Uncommon
		wildcards.Rare -= change.WildcardDeltas.Rare
		wildcards.Mythic -= change.WildcardDeltas.Mythic
		for _, booster := range change.BoostersDelta {
			boosters[booster.SetCode] -= booster.Count
		}
	}

	packsOpened := false
	for _, count := range boosters {
		if count < 0 {
			packsOpened = true
		}
	}
	packActivity := packsOpened || vault != 0

	packs := model.EconomyTransaction{
		ID:         -2 * current.id,
		ObservedAt: current.observedAt,
		Source:     InferredBoosterOpenSource,
		Inferred:   true,
	}
	other := model.EconomyTransaction{
		ID:         -2*current.id - 1,
		ObservedAt: current.observedAt,
		Source:     InferredUnreportedSource,
		Inferred:   true,
		GoldDelta:  gold,
	}
	if packActivity {
		if gems < inferredPackGemsLimit {
			packs.GemsDelta, other.GemsDelta = splitGain(gems)
		} else {
			other.GemsDelta = gems
		}
		packs.WildcardDeltas.Common, other.WildcardDeltas.Common = splitGain(wildcards.Common)
		packs.WildcardDeltas.Uncommon, other.WildcardDeltas.Uncommon = splitGain(wildcards.Uncommon)
		packs.WildcardDeltas.Rare, other.WildcardDeltas.Rare = splitGain(wildcards.Rare)
		packs.WildcardDeltas.Mythic, other.WildcardDeltas.Mythic = splitGain(wildcards.Mythic)
		packs.VaultProgressDelta = vault
		packs.Boosters, other.Boosters = splitBoosterResidual(boosters)
	} else {
		other.GemsDelta = gems
		other.WildcardDeltas = wildcards
		other.VaultProgressDelta = vault
		other.Boosters = boosterCountList(boosters)
	}

	out := make([]model.EconomyTransaction, 0, 2)
	for _, txn := range []model.EconomyTransaction{packs, other} {
		if txn.Boosters == nil {
			txn.Boosters = []model.EconomyBoosterCount{}
		}
		txn.CustomTokens = map[string]int64{}
		txn.Vouchers = map[string]int64{}
		if !economyTransactionIsZero(txn) {
			out = append(out, txn)
		}
	}
	return out
}

// splitGain returns a delta's gain and loss parts.
func splitGain(delta int64) (gain, loss int64) {
	if delta > 0 {
		return delta, 0
	}
	return 0, delta
}

// splitBoosterResidual separates consumed packs from unreported grants.
func splitBoosterResidual(boosters map[string]int64) (opened, granted []model.EconomyBoosterCount) {
	openedCounts := make(map[string]int64)
	grantedCounts := make(map[string]int64)
	for setCode, count := range boosters {
		if count < 0 {
			openedCounts[setCode] = count
		} else if count > 0 {
			grantedCounts[setCode] = count
		}
	}
	return boosterCountList(openedCounts), boosterCountList(grantedCounts)
}

func boosterCountMap(boosters []model.EconomyBoosterCount) map[string]int64 {
	out := make(map[string]int64, len(boosters))
	for _, booster := range boosters {
		out[booster.SetCode] += booster.Count
	}
	return out
}

func boosterCountList(counts map[string]int64) []model.EconomyBoosterCount {
	out := make([]model.EconomyBoosterCount, 0, len(counts))
	for setCode, count := range counts {
		if count != 0 {
			out = append(out, model.EconomyBoosterCount{SetCode: setCode, Count: count})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetCode < out[j].SetCode })
	return out
}

func economyTransactionIsZero(txn model.EconomyTransaction) bool {
	return txn.GoldDelta == 0 &&
		txn.GemsDelta == 0 &&
		txn.VaultProgressDelta == 0 &&
		txn.WildcardDeltas == (model.WildcardBalance{}) &&
		len(txn.Boosters) == 0
}

// mergeEconomyLedger interleaves inferred rows into the recorded ledger. An
// inferred row covers the gap before its snapshot, so it sorts ahead of
// recorded rows observed at the same instant.
func mergeEconomyLedger(recorded, inferred []model.EconomyTransaction) []model.EconomyTransaction {
	out := make([]model.EconomyTransaction, 0, len(recorded)+len(inferred))
	next := 0
	for _, txn := range recorded {
		for txn.ObservedAt != "" && next < len(inferred) && inferred[next].ObservedAt <= txn.ObservedAt {
			out = append(out, inferred[next])
			next++
		}
		out = append(out, txn)
	}
	return append(out, inferred[next:]...)
}

// decodeInventoryBoosters reads an InventoryInfo Boosters balance. Arena
// omits Count when it is zero and keeps the entry after its packs are
// opened, so a missing Count means none are left (unlike change deltas).
func decodeInventoryBoosters(payload string) []model.EconomyBoosterCount {
	var boosters []struct {
		SetCode string `json:"SetCode"`
		Count   int64  `json:"Count"`
	}
	if json.Unmarshal([]byte(payload), &boosters) != nil {
		return []model.EconomyBoosterCount{}
	}
	counts := make(map[string]int64)
	for _, booster := range boosters {
		setCode := strings.ToUpper(strings.TrimSpace(booster.SetCode))
		if setCode != "" && booster.Count > 0 {
			counts[setCode] += booster.Count
		}
	}
	return boosterCountList(counts)
}
