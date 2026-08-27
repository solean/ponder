package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// EconomyChange is one decoded entry of an InventoryInfo Changes array.
type EconomyChange struct {
	Source             string
	SourceID           string
	GoldDelta          int64
	GemsDelta          int64
	WildcardDeltas     model.WildcardBalance
	CardsGranted       int64
	VaultProgressDelta int64
	BoostersDelta      []model.EconomyBoosterCount
	CustomTokensDelta  map[string]int64
	VouchersDelta      map[string]int64
}

type rawEconomyChange struct {
	Source            string          `json:"Source"`
	SourceID          string          `json:"SourceId"`
	InventoryGold     int64           `json:"InventoryGold"`
	InventoryGems     int64           `json:"InventoryGems"`
	WildcardCommons   int64           `json:"InventoryWildCardCommons"`
	WildcardUncommons int64           `json:"InventoryWildCardUnCommons"`
	WildcardRares     int64           `json:"InventoryWildCardRares"`
	WildcardMythics   int64           `json:"InventoryWildCardMythics"`
	CustomTokens      json.RawMessage `json:"InventoryCustomTokens"`
	Boosters          json.RawMessage `json:"Boosters"`
	Vouchers          json.RawMessage `json:"Vouchers"`
	GrantedCards      []struct {
		GrpID         int64 `json:"GrpId"`
		CardAdded     bool  `json:"CardAdded"`
		VaultProgress int64 `json:"VaultProgress"`
	} `json:"GrantedCards"`
}

// DecodeEconomyChanges parses a Changes array into normalized deltas. Gems
// granted for duplicate cards inside GrantedCards are already included in
// InventoryGems, so only per-card VaultProgress is summed separately.
func DecodeEconomyChanges(changesJSON string) []EconomyChange {
	var raw []rawEconomyChange
	if json.Unmarshal([]byte(changesJSON), &raw) != nil || len(raw) == 0 {
		return nil
	}

	out := make([]EconomyChange, 0, len(raw))
	for _, entry := range raw {
		change := EconomyChange{
			Source:    strings.TrimSpace(entry.Source),
			SourceID:  strings.TrimSpace(entry.SourceID),
			GoldDelta: entry.InventoryGold,
			GemsDelta: entry.InventoryGems,
			WildcardDeltas: model.WildcardBalance{
				Common:   entry.WildcardCommons,
				Uncommon: entry.WildcardUncommons,
				Rare:     entry.WildcardRares,
				Mythic:   entry.WildcardMythics,
			},
			BoostersDelta:     decodeBoosterCounts(string(entry.Boosters)),
			CustomTokensDelta: decodeIntMap(string(entry.CustomTokens)),
			VouchersDelta:     decodeIntMap(string(entry.Vouchers)),
		}
		for _, card := range entry.GrantedCards {
			change.CardsGranted++
			change.VaultProgressDelta += card.VaultProgress
		}
		out = append(out, change)
	}
	return out
}

// Sources whose SourceId is the EventPayEntry GUID of the run they belong to.
func economyChangeUsesPaySourceID(source string) bool {
	switch source {
	case "EventReward", "EventPayEntry", "EventRefund":
		return true
	}
	return false
}

const economyEventLinkWindowMinutes = 15.0

// DeriveEconomyTransactions decodes a snapshot's Changes payload into
// economy_transactions rows and attributes event-related changes to
// event_runs. Linking is exact where the payload names the event
// (EventGrantCardPool) or where the pay GUID is already recorded; otherwise a
// 15-minute proximity window against event_runs timestamps is used and
// labeled as such. Inserts are idempotent per (snapshot, change index).
func (s *Store) DeriveEconomyTransactions(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID int64,
	observedAt string,
	changesJSON string,
) (int64, error) {
	changes := DecodeEconomyChanges(changesJSON)
	if len(changes) == 0 {
		return 0, nil
	}

	observedAt = normalizeTS(observedAt)
	inserted := int64(0)
	for index, change := range changes {
		if change.Source == "" {
			continue
		}

		eventRunID, eventName, eventLink, err := s.linkEconomyChangeToEvent(ctx, tx, change, observedAt)
		if err != nil {
			return inserted, err
		}

		boostersJSON, err := json.Marshal(change.BoostersDelta)
		if err != nil {
			return inserted, fmt.Errorf("encode booster deltas: %w", err)
		}
		customTokensJSON, err := json.Marshal(change.CustomTokensDelta)
		if err != nil {
			return inserted, fmt.Errorf("encode custom token deltas: %w", err)
		}
		vouchersJSON, err := json.Marshal(change.VouchersDelta)
		if err != nil {
			return inserted, fmt.Errorf("encode voucher deltas: %w", err)
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO economy_transactions (
				snapshot_id,
				change_index,
				observed_at,
				source,
				source_id,
				event_name,
				event_run_id,
				event_link,
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
				vouchers_delta_json,
				created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(snapshot_id, change_index) DO NOTHING
		`, snapshotID, index, nullIfEmpty(observedAt), change.Source, nullIfEmpty(change.SourceID),
			nullIfEmpty(eventName), nullableInt(eventRunID), nullIfEmpty(eventLink),
			change.GoldDelta, change.GemsDelta,
			change.WildcardDeltas.Common, change.WildcardDeltas.Uncommon,
			change.WildcardDeltas.Rare, change.WildcardDeltas.Mythic,
			change.CardsGranted, change.VaultProgressDelta,
			string(boostersJSON), string(customTokensJSON), string(vouchersJSON), nowUTC())
		if err != nil {
			return inserted, fmt.Errorf("insert economy transaction: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return inserted, fmt.Errorf("count inserted economy transactions: %w", err)
		}
		inserted += rows

		// Remember the pay GUID on this run so its later EventReward links
		// exactly even when another run reuses the same Arena event name.
		if rows > 0 && change.Source == "EventPayEntry" && change.SourceID != "" && eventRunID > 0 {
			if err := recordEventRunPaySource(ctx, tx, eventRunID, change.SourceID); err != nil {
				return inserted, err
			}
		}
	}
	return inserted, nil
}

func (s *Store) linkEconomyChangeToEvent(
	ctx context.Context,
	tx *sql.Tx,
	change EconomyChange,
	observedAt string,
) (eventRunID int64, eventName, eventLink string, err error) {
	switch change.Source {
	case "EventGrantCardPool":
		if change.SourceID == "" {
			return 0, "", "", nil
		}
		err := tx.QueryRowContext(ctx, `
			SELECT id, event_name
			FROM event_runs
			WHERE event_name = ?
			  AND (
				? = ''
				OR started_at IS NULL
				OR started_at = ''
				OR ABS(julianday(?) - julianday(started_at)) * 1440.0 <= ?
			  )
			ORDER BY
				CASE WHEN status = 'active' THEN 0 ELSE 1 END,
				ABS(julianday(?) - julianday(started_at)),
				id DESC
			LIMIT 1
		`, change.SourceID, observedAt, observedAt, economyEventLinkWindowMinutes, observedAt).Scan(&eventRunID, &eventName)
		if err == nil {
			return eventRunID, eventName, "event_name", nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", "", fmt.Errorf("link card pool grant to event: %w", err)
		}
		// The event name remains useful on the ledger even without a run.
		return 0, change.SourceID, "event_name", nil
	}

	if !economyChangeUsesPaySourceID(change.Source) {
		return 0, "", "", nil
	}

	if change.SourceID != "" {
		err := tx.QueryRowContext(ctx, `
			SELECT id, event_name
			FROM event_runs
			WHERE pay_source_id = ?
			ORDER BY id DESC
			LIMIT 1
		`, change.SourceID).Scan(&eventRunID, &eventName)
		if err == nil {
			return eventRunID, eventName, "source_id", nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", "", fmt.Errorf("link economy change by pay source id: %w", err)
		}
	}

	if observedAt == "" {
		return 0, "", "", nil
	}

	// Entry changes land near a run's start; rewards land near its claim or
	// final match. Draft-session runs qualify before their entry currency is
	// known, while free ladder/open-play rows remain excluded.
	timeColumn := "started_at"
	if change.Source == "EventReward" {
		timeColumn = "ended_at"
	}
	query := fmt.Sprintf(`
		SELECT id, event_name
		FROM event_runs
		WHERE %[1]s IS NOT NULL AND %[1]s != ''
		  AND (
			COALESCE(entry_currency_type, 'None') != 'None'
			OR draft_session_id IS NOT NULL
		  )
		  AND ABS(julianday(?) - julianday(%[1]s)) * 1440.0 <= ?
		ORDER BY ABS(julianday(?) - julianday(%[1]s)) ASC, id DESC
		LIMIT 1
	`, timeColumn)
	err = tx.QueryRowContext(ctx, query, observedAt, economyEventLinkWindowMinutes, observedAt).Scan(&eventRunID, &eventName)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", nil
	}
	if err != nil {
		return 0, "", "", fmt.Errorf("link economy change by proximity: %w", err)
	}
	return eventRunID, eventName, "proximity", nil
}

func recordEventRunPaySource(ctx context.Context, tx *sql.Tx, eventRunID int64, sourceID string) error {
	if eventRunID <= 0 || sourceID == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE event_runs
		SET pay_source_id = COALESCE(pay_source_id, ?), updated_at = ?
		WHERE id = ?
	`, sourceID, nowUTC(), eventRunID); err != nil {
		return fmt.Errorf("record event pay source id: %w", err)
	}
	return nil
}

func (s *Store) repairUnlinkedEconomyTransactionsTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, source, COALESCE(source_id, ''), COALESCE(observed_at, '')
		FROM economy_transactions
		WHERE event_run_id IS NULL
		  AND source IN ('EventPayEntry', 'EventReward', 'EventRefund', 'EventGrantCardPool')
		ORDER BY COALESCE(observed_at, created_at), id
	`)
	if err != nil {
		return fmt.Errorf("list unlinked event transactions: %w", err)
	}
	type pendingLink struct {
		id         int64
		source     string
		sourceID   string
		observedAt string
	}
	pending := make([]pendingLink, 0)
	for rows.Next() {
		var link pendingLink
		if err := rows.Scan(&link.id, &link.source, &link.sourceID, &link.observedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan unlinked event transaction: %w", err)
		}
		pending = append(pending, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate unlinked event transactions: %w", err)
	}
	rows.Close()

	for _, pendingLink := range pending {
		change := EconomyChange{Source: pendingLink.source, SourceID: pendingLink.sourceID}
		eventRunID, eventName, eventLink, err := s.linkEconomyChangeToEvent(ctx, tx, change, pendingLink.observedAt)
		if err != nil {
			return err
		}
		if eventRunID <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE economy_transactions
			SET event_run_id = ?, event_name = ?, event_link = ?
			WHERE id = ?
		`, eventRunID, eventName, eventLink, pendingLink.id); err != nil {
			return fmt.Errorf("repair event transaction link: %w", err)
		}
		if pendingLink.source == "EventPayEntry" {
			if err := recordEventRunPaySource(ctx, tx, eventRunID, pendingLink.sourceID); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillEconomyTransactions derives normalized transactions for snapshots
// stored before the ledger existed. It is idempotent: only snapshots with a
// non-empty Changes payload and no derived rows are processed, so the routine
// can run on every startup.
func backfillEconomyTransactions(ctx context.Context, conn dbConn, store *Store) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin economy transaction backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT es.id, COALESCE(es.observed_at, ''), es.changes_json
		FROM economy_snapshots es
		WHERE es.changes_json != '[]'
		  AND NOT EXISTS (
			SELECT 1 FROM economy_transactions et WHERE et.snapshot_id = es.id
		  )
		ORDER BY COALESCE(es.observed_at, es.created_at) ASC, es.id ASC
	`)
	if err != nil {
		return fmt.Errorf("list snapshots for economy transaction backfill: %w", err)
	}
	type pendingSnapshot struct {
		id          int64
		observedAt  string
		changesJSON string
	}
	pending := make([]pendingSnapshot, 0)
	for rows.Next() {
		var snapshot pendingSnapshot
		if err := rows.Scan(&snapshot.id, &snapshot.observedAt, &snapshot.changesJSON); err != nil {
			rows.Close()
			return fmt.Errorf("scan snapshot for economy transaction backfill: %w", err)
		}
		pending = append(pending, snapshot)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate snapshots for economy transaction backfill: %w", err)
	}
	rows.Close()

	for _, snapshot := range pending {
		if _, err := store.DeriveEconomyTransactions(ctx, tx, snapshot.id, snapshot.observedAt, snapshot.changesJSON); err != nil {
			return fmt.Errorf("backfill economy transactions for snapshot %d: %w", snapshot.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit economy transaction backfill: %w", err)
	}
	return nil
}

// migrateEconomyTables gives each repeated event name a distinct run identity
// and adds that identity to the transaction ledger.
func migrateEconomyTables(ctx context.Context, conn dbConn) error {
	hasDraftSessionID, err := tableHasColumn(ctx, conn, "event_runs", "draft_session_id")
	if err != nil {
		return fmt.Errorf("inspect event_runs instance schema: %w", err)
	}
	if !hasDraftSessionID {
		hasPaySourceID, err := tableHasColumn(ctx, conn, "event_runs", "pay_source_id")
		if err != nil {
			return fmt.Errorf("inspect event_runs pay source schema: %w", err)
		}
		if err := rebuildEventRunsTable(ctx, conn, hasPaySourceID); err != nil {
			return err
		}
	}

	hasEventRunID, err := tableHasColumn(ctx, conn, "economy_transactions", "event_run_id")
	if err != nil {
		return fmt.Errorf("inspect economy transaction run schema: %w", err)
	}
	if !hasEventRunID {
		if _, err := conn.ExecContext(ctx, `
			ALTER TABLE economy_transactions
			ADD COLUMN event_run_id INTEGER REFERENCES event_runs(id) ON DELETE SET NULL
		`); err != nil {
			return fmt.Errorf("add economy transaction run id: %w", err)
		}
	}

	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_event_runs_name_started ON event_runs(event_name, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_event_runs_pay_source ON event_runs(pay_source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_transactions_run ON economy_transactions(event_run_id)`,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create economy run index: %w", err)
		}
	}
	return nil
}

func rebuildEventRunsTable(ctx context.Context, conn dbConn, hasPaySourceID bool) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event run instance migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	paySource := "NULL"
	if hasPaySourceID {
		paySource = "pay_source_id"
	}
	steps := []string{
		`ALTER TABLE event_runs RENAME TO event_runs_old`,
		`DROP INDEX IF EXISTS idx_event_runs_name_started`,
		`DROP INDEX IF EXISTS idx_event_runs_pay_source`,
		`CREATE TABLE event_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_name TEXT NOT NULL,
			event_type TEXT,
			draft_session_id INTEGER UNIQUE,
			entry_currency_type TEXT,
			entry_currency_paid INTEGER,
			pay_source_id TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			started_at TEXT,
			ended_at TEXT,
			wins INTEGER NOT NULL DEFAULT 0,
			losses INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		fmt.Sprintf(`INSERT INTO event_runs (
			id, event_name, event_type, entry_currency_type, entry_currency_paid,
			pay_source_id, status, started_at, ended_at, wins, losses, updated_at
		)
		SELECT
			id, event_name, event_type, entry_currency_type, entry_currency_paid,
			%s, status, started_at, ended_at, wins, losses, updated_at
		FROM event_runs_old`, paySource),
		`DROP TABLE event_runs_old`,
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return fmt.Errorf("migrate event run instances: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event run instance migration: %w", err)
	}
	return nil
}
