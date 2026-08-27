package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

type EconomySnapshotRecord struct {
	ObservedAt            string
	SequenceID            int64
	Gold                  int64
	Gems                  int64
	VaultProgress         int64
	WildcardTrackPosition int64
	WildcardCommons       int64
	WildcardUncommons     int64
	WildcardRares         int64
	WildcardMythics       int64
	CustomTokensJSON      string
	BoostersJSON          string
	VouchersJSON          string
	ChangesJSON           string
}

// InsertEconomySnapshot stores one InventoryInfo snapshot. It returns the
// snapshot's row id (also when the row already existed) and whether this call
// inserted it.
func (s *Store) InsertEconomySnapshot(
	ctx context.Context,
	tx *sql.Tx,
	logPath string,
	lineNo int64,
	snapshot EconomySnapshotRecord,
) (int64, bool, error) {
	snapshot.ObservedAt = normalizeTS(snapshot.ObservedAt)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO economy_snapshots (
			log_path,
			line_no,
			observed_at,
			sequence_id,
			gold,
			gems,
			vault_progress,
			wildcard_track_position,
			wildcard_commons,
			wildcard_uncommons,
			wildcard_rares,
			wildcard_mythics,
			custom_tokens_json,
			boosters_json,
			vouchers_json,
			changes_json,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(log_path, line_no) DO NOTHING
	`, logPath, lineNo, nullIfEmpty(snapshot.ObservedAt), snapshot.SequenceID,
		snapshot.Gold, snapshot.Gems, snapshot.VaultProgress, snapshot.WildcardTrackPosition,
		snapshot.WildcardCommons, snapshot.WildcardUncommons, snapshot.WildcardRares,
		snapshot.WildcardMythics, jsonOrDefault(snapshot.CustomTokensJSON, "{}"),
		jsonOrDefault(snapshot.BoostersJSON, "[]"), jsonOrDefault(snapshot.VouchersJSON, "{}"),
		jsonOrDefault(snapshot.ChangesJSON, "[]"), nowUTC())
	if err != nil {
		return 0, false, fmt.Errorf("insert economy snapshot: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("count inserted economy snapshots: %w", err)
	}
	if rowsAffected > 0 {
		id, err := result.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("resolve inserted economy snapshot id: %w", err)
		}
		return id, true, nil
	}

	var id int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM economy_snapshots WHERE log_path = ? AND line_no = ?
	`, logPath, lineNo).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("lookup existing economy snapshot: %w", err)
	}
	return id, false, nil
}

func (s *Store) ListEconomyHistory(ctx context.Context) ([]model.EconomySnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(observed_at, ''),
			sequence_id,
			gold,
			gems,
			vault_progress,
			wildcard_track_position,
			wildcard_commons,
			wildcard_uncommons,
			wildcard_rares,
			wildcard_mythics,
			custom_tokens_json,
			boosters_json,
			vouchers_json,
			changes_json
		FROM economy_snapshots
		ORDER BY COALESCE(observed_at, created_at) ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list economy history: %w", err)
	}
	defer rows.Close()

	out := make([]model.EconomySnapshot, 0)
	for rows.Next() {
		var snapshot model.EconomySnapshot
		var customTokensJSON, boostersJSON, vouchersJSON, changesJSON string
		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.ObservedAt,
			&snapshot.SequenceID,
			&snapshot.Gold,
			&snapshot.Gems,
			&snapshot.VaultProgress,
			&snapshot.WildcardTrackPosition,
			&snapshot.Wildcards.Common,
			&snapshot.Wildcards.Uncommon,
			&snapshot.Wildcards.Rare,
			&snapshot.Wildcards.Mythic,
			&customTokensJSON,
			&boostersJSON,
			&vouchersJSON,
			&changesJSON,
		); err != nil {
			return nil, fmt.Errorf("scan economy snapshot: %w", err)
		}

		snapshot.CustomTokens = decodeIntMap(customTokensJSON)
		snapshot.Boosters = decodeBoosterCounts(boostersJSON)
		snapshot.Vouchers = decodeIntMap(vouchersJSON)
		snapshot.ChangeSources = decodeChangeSources(changesJSON)
		out = append(out, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate economy history: %w", err)
	}
	return out, nil
}

// ListEconomyTransactions returns the normalized inventory-change ledger in
// chronological order.
func (s *Store) ListEconomyTransactions(ctx context.Context) ([]model.EconomyTransaction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(observed_at, ''),
			source,
			COALESCE(event_name, ''),
			COALESCE(event_link, ''),
			gold_delta,
			gems_delta,
			wildcard_common_delta,
			wildcard_uncommon_delta,
			wildcard_rare_delta,
			wildcard_mythic_delta,
			cards_granted,
			vault_progress_delta,
			boosters_delta_json,
			custom_tokens_delta_json,
			vouchers_delta_json
		FROM economy_transactions
		ORDER BY COALESCE(observed_at, created_at) ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list economy transactions: %w", err)
	}
	defer rows.Close()

	out := make([]model.EconomyTransaction, 0)
	for rows.Next() {
		var txn model.EconomyTransaction
		var boostersJSON, customTokensJSON, vouchersJSON string
		if err := rows.Scan(
			&txn.ID,
			&txn.ObservedAt,
			&txn.Source,
			&txn.EventName,
			&txn.EventLink,
			&txn.GoldDelta,
			&txn.GemsDelta,
			&txn.WildcardDeltas.Common,
			&txn.WildcardDeltas.Uncommon,
			&txn.WildcardDeltas.Rare,
			&txn.WildcardDeltas.Mythic,
			&txn.CardsGranted,
			&txn.VaultProgressDelta,
			&boostersJSON,
			&customTokensJSON,
			&vouchersJSON,
		); err != nil {
			return nil, fmt.Errorf("scan economy transaction: %w", err)
		}
		txn.Boosters = decodeBoosterCounts(boostersJSON)
		txn.CustomTokens = decodeIntMap(customTokensJSON)
		txn.Vouchers = decodeIntMap(vouchersJSON)
		out = append(out, txn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate economy transactions: %w", err)
	}
	return out, nil
}

// ListEventRunEconomies folds the transaction ledger into per-run cost and
// reward summaries. Runs with a free entry and no linked transactions (ladder
// and open play) are omitted. SetCode is left for the API layer to derive
// from the event name.
func (s *Store) ListEventRunEconomies(ctx context.Context) ([]model.EventRunEconomy, error) {
	runRows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			event_name,
			COALESCE(event_type, 'other'),
			COALESCE(entry_currency_type, ''),
			entry_currency_paid,
			status,
			COALESCE(started_at, ''),
			COALESCE(ended_at, ''),
			wins,
			losses
		FROM event_runs
		ORDER BY COALESCE(started_at, updated_at) DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list event runs: %w", err)
	}
	defer runRows.Close()

	runs := make([]model.EventRunEconomy, 0)
	for runRows.Next() {
		var run model.EventRunEconomy
		var entryPaid sql.NullInt64
		if err := runRows.Scan(
			&run.ID,
			&run.EventName,
			&run.EventType,
			&run.EntryCurrencyType,
			&entryPaid,
			&run.Status,
			&run.StartedAt,
			&run.EndedAt,
			&run.Wins,
			&run.Losses,
		); err != nil {
			return nil, fmt.Errorf("scan event run: %w", err)
		}
		run.EntryCurrencyPaid = nullInt64Ptr(entryPaid)
		run.RewardBoosters = []model.EconomyBoosterCount{}
		run.LinkConfidence = "none"
		runs = append(runs, run)
	}
	if err := runRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event runs: %w", err)
	}

	type runEconomy struct {
		hasTransactions bool
		hasProximity    bool
		payGold         int64
		payGems         int64
		hasPay          bool
		rewardGold      int64
		rewardGems      int64
		rewardCards     int64
		rewardVault     int64
		hasReward       bool
		boosterCounts   map[string]int64
	}
	economies := make(map[int64]*runEconomy)
	economyFor := func(eventRunID int64) *runEconomy {
		if entry, ok := economies[eventRunID]; ok {
			return entry
		}
		entry := &runEconomy{boosterCounts: make(map[string]int64)}
		economies[eventRunID] = entry
		return entry
	}

	txnRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT
			event_run_id,
			COALESCE(event_link, ''),
			source,
			COALESCE(source_id, ''),
			COALESCE(observed_at, ''),
			gold_delta,
			gems_delta,
			cards_granted,
			vault_progress_delta,
			boosters_delta_json
		FROM economy_transactions
		WHERE event_run_id IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("list event-linked transactions: %w", err)
	}
	defer txnRows.Close()

	for txnRows.Next() {
		var eventRunID int64
		var eventLink, source, sourceID, observedAt, boostersJSON string
		var gold, gems, cards, vault int64
		if err := txnRows.Scan(
			&eventRunID, &eventLink, &source, &sourceID, &observedAt,
			&gold, &gems, &cards, &vault, &boostersJSON,
		); err != nil {
			return nil, fmt.Errorf("scan event-linked transaction: %w", err)
		}
		economy := economyFor(eventRunID)
		economy.hasTransactions = true
		if eventLink == "proximity" {
			economy.hasProximity = true
		}
		switch source {
		case "EventPayEntry", "EventRefund":
			economy.hasPay = true
			economy.payGold += gold
			economy.payGems += gems
		case "EventReward", "EventGrantCardPool":
			economy.hasReward = true
			economy.rewardGold += gold
			economy.rewardGems += gems
			economy.rewardCards += cards
			economy.rewardVault += vault
			for _, booster := range decodeBoosterCounts(boostersJSON) {
				economy.boosterCounts[booster.SetCode] += booster.Count
			}
		}
	}
	if err := txnRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event-linked transactions: %w", err)
	}

	out := make([]model.EventRunEconomy, 0, len(runs))
	for _, run := range runs {
		economy := economies[run.ID]
		paidEntry := run.EntryCurrencyType != "" && run.EntryCurrencyType != "None"
		if economy == nil && !paidEntry {
			continue
		}

		if economy != nil {
			if economy.hasPay {
				run.EntryGold = economy.payGold
				run.EntryGems = economy.payGems
			}
			run.RewardGold = economy.rewardGold
			run.RewardGems = economy.rewardGems
			run.RewardCards = economy.rewardCards
			run.RewardVaultProgress = economy.rewardVault
			for setCode, count := range economy.boosterCounts {
				run.RewardBoosters = append(run.RewardBoosters, model.EconomyBoosterCount{SetCode: setCode, Count: count})
			}
			sort.Slice(run.RewardBoosters, func(i, j int) bool {
				return run.RewardBoosters[i].SetCode < run.RewardBoosters[j].SetCode
			})
			if economy.hasReward {
				run.LinkConfidence = "exact"
				if economy.hasProximity {
					run.LinkConfidence = "inferred"
				}
			}
		}

		// Without an observed pay transaction, fall back to the entry price
		// from the EventJoin request itself — still an exact log value.
		if (economy == nil || !economy.hasPay) && run.EntryCurrencyPaid != nil {
			switch run.EntryCurrencyType {
			case "Gold":
				run.EntryGold = -*run.EntryCurrencyPaid
			case "Gem", "Gems":
				run.EntryGems = -*run.EntryCurrencyPaid
			}
		}

		run.NetGold = run.EntryGold + run.RewardGold
		run.NetGems = run.EntryGems + run.RewardGems
		out = append(out, run)
	}
	return out, nil
}

func jsonOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func decodeIntMap(payload string) map[string]int64 {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &raw) != nil || len(raw) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(raw))
	for key, value := range raw {
		var count int64
		if json.Unmarshal(value, &count) == nil {
			out[key] = count
		}
	}
	return out
}

func decodeBoosterCounts(payload string) []model.EconomyBoosterCount {
	var boosters []struct {
		SetCode  string `json:"SetCode"`
		Count    int64  `json:"Count"`
		Quantity int64  `json:"Quantity"`
	}
	if json.Unmarshal([]byte(payload), &boosters) != nil || len(boosters) == 0 {
		return []model.EconomyBoosterCount{}
	}
	counts := make(map[string]int64)
	for _, booster := range boosters {
		setCode := strings.ToUpper(strings.TrimSpace(booster.SetCode))
		if setCode == "" {
			continue
		}
		count := max(booster.Count, booster.Quantity)
		if count <= 0 {
			count = 1
		}
		counts[setCode] += count
	}
	out := make([]model.EconomyBoosterCount, 0, len(counts))
	for setCode, count := range counts {
		out = append(out, model.EconomyBoosterCount{SetCode: setCode, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetCode < out[j].SetCode })
	return out
}

func decodeChangeSources(payload string) []string {
	var changes []struct {
		Source string `json:"Source"`
	}
	if json.Unmarshal([]byte(payload), &changes) != nil || len(changes) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(changes))
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		source := strings.TrimSpace(change.Source)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	return out
}
