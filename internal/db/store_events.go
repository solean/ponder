package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

func detectEventType(eventName string) string {
	e := strings.ToLower(eventName)
	switch {
	case strings.Contains(e, "quickdraft"):
		return "quick_draft"
	case strings.Contains(e, "premierdraft"):
		return "premier_draft"
	case strings.Contains(e, "traditionalsealed") || strings.Contains(e, "sealed"):
		return "sealed"
	case strings.Contains(e, "jump_in"):
		return "jump_in"
	case strings.Contains(e, "ladder"):
		return "ladder"
	default:
		return "other"
	}
}

var reSetKindEvent = regexp.MustCompile(`^([A-Za-z0-9]+)_(Quick_Draft|Premier_Draft|Sealed)$`)

func (s *Store) resolveEventNameAlias(ctx context.Context, tx *sql.Tx, eventName string) (string, error) {
	eventName = strings.TrimSpace(eventName)
	if eventName == "" {
		return "", nil
	}

	var existing string
	err := tx.QueryRowContext(ctx, `SELECT event_name FROM event_runs WHERE event_name = ? LIMIT 1`, eventName).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("resolve event alias exact match: %w", err)
	}

	matches := reSetKindEvent.FindStringSubmatch(eventName)
	if len(matches) != 3 {
		return eventName, nil
	}

	setCode := strings.ToLower(matches[1])
	kind := strings.ToLower(matches[2])
	likePattern := ""
	switch kind {
	case "quick_draft":
		likePattern = fmt.Sprintf("quickdraft_%s_%%", setCode)
	case "premier_draft":
		likePattern = fmt.Sprintf("premierdraft_%s_%%", setCode)
	case "sealed":
		likePattern = fmt.Sprintf("sealed_%s_%%", setCode)
	}
	if likePattern == "" {
		return eventName, nil
	}

	err = tx.QueryRowContext(ctx, `
		SELECT event_name
		FROM event_runs
		WHERE LOWER(event_name) LIKE ?
		ORDER BY COALESCE(started_at, updated_at) DESC
		LIMIT 1
	`, likePattern).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("resolve event alias pattern: %w", err)
	}

	return eventName, nil
}

func (s *Store) UpsertEventRunJoin(ctx context.Context, tx *sql.Tx, eventName, currencyType string, currencyPaid int64, ts string) error {
	eventName = strings.TrimSpace(eventName)
	if eventName == "" {
		return nil
	}
	eventType := detectEventType(eventName)
	ts = normalizeTS(ts)

	var runID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM event_runs
		WHERE event_name = ?
		  AND (
			? = ''
			OR (
				started_at IS NOT NULL
				AND started_at != ''
				AND ABS(julianday(?) - julianday(started_at)) * 1440.0 <= ?
			)
		  )
		ORDER BY
			CASE WHEN status = 'active' THEN 0 ELSE 1 END,
			ABS(julianday(?) - julianday(started_at)),
			id DESC
		LIMIT 1
	`, eventName, ts, ts, economyEventLinkWindowMinutes, ts).Scan(&runID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("find event run join instance: %w", err)
	}

	if errors.Is(err, sql.ErrNoRows) {
		result, insertErr := tx.ExecContext(ctx, `
			INSERT INTO event_runs (
				event_name, event_type, entry_currency_type, entry_currency_paid, status, started_at, updated_at
			) VALUES (?, ?, ?, ?, 'active', ?, ?)
		`, eventName, eventType, nullIfEmpty(currencyType), nullableInt(currencyPaid), nullIfEmpty(ts), nowUTC())
		if insertErr != nil {
			return fmt.Errorf("insert event run join: %w", insertErr)
		}
		runID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read inserted event run id: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE event_runs
		SET event_type = ?,
			entry_currency_type = COALESCE(?, entry_currency_type),
			entry_currency_paid = COALESCE(?, entry_currency_paid),
			started_at = COALESCE(started_at, ?),
			updated_at = ?
		WHERE id = ?
	`, eventType, nullIfEmpty(currencyType), nullableInt(currencyPaid), nullIfEmpty(ts), nowUTC(), runID); err != nil {
		return fmt.Errorf("update event run join: %w", err)
	}
	return nil
}

func (s *Store) MarkEventRunClaimed(ctx context.Context, tx *sql.Tx, eventName, ts string) error {
	ts = normalizeTS(ts)
	_, err := tx.ExecContext(ctx, `
		UPDATE event_runs
		SET status = 'claimed',
			ended_at = COALESCE(ended_at, ?),
			updated_at = ?
		WHERE id = (
			SELECT id
			FROM event_runs
			WHERE event_name = ?
			  AND (? = '' OR started_at IS NULL OR started_at <= ?)
			ORDER BY
				CASE WHEN status = 'active' THEN 0 ELSE 1 END,
				COALESCE(started_at, updated_at) DESC,
				id DESC
			LIMIT 1
		)
	`, nullIfEmpty(ts), nowUTC(), eventName, ts, ts)
	if err != nil {
		return fmt.Errorf("mark event run claimed: %w", err)
	}
	return nil
}

func (s *Store) BumpEventRunRecord(ctx context.Context, tx *sql.Tx, eventName, result, ts string) error {
	if eventName == "" || (result != "win" && result != "loss") {
		return nil
	}
	col := "wins"
	if result == "loss" {
		col = "losses"
	}
	ts = normalizeTS(ts)
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE event_runs
		SET %[1]s = %[1]s + 1,
			updated_at = ?
		WHERE id = (
			SELECT id
			FROM event_runs
			WHERE event_name = ?
			  AND (? = '' OR started_at IS NULL OR started_at <= ?)
			ORDER BY
				CASE WHEN status = 'active' THEN 0 ELSE 1 END,
				COALESCE(started_at, updated_at) DESC,
				id DESC
			LIMIT 1
		)
	`, col), nowUTC(), eventName, ts, ts)
	if err != nil {
		return fmt.Errorf("bump event run record: %w", err)
	}
	return nil
}

func (s *Store) attachDraftSessionToEventRun(
	ctx context.Context,
	tx *sql.Tx,
	sessionID int64,
	eventName, startedAt string,
) (int64, bool, error) {
	eventName = strings.TrimSpace(eventName)
	startedAt = normalizeTS(startedAt)
	if sessionID <= 0 || eventName == "" {
		return 0, false, nil
	}

	var runID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM event_runs WHERE draft_session_id = ?
	`, sessionID).Scan(&runID)
	if err == nil {
		return runID, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("find draft event run: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM event_runs
		WHERE event_name = ?
		  AND draft_session_id IS NULL
		  AND (
			started_at IS NULL
			OR started_at = ''
			OR (
				? != ''
				AND ABS(julianday(?) - julianday(started_at)) * 1440.0 <= ?
			)
		  )
		ORDER BY
			CASE WHEN started_at IS NULL OR started_at = '' THEN 1 ELSE 0 END,
			ABS(julianday(?) - julianday(started_at)),
			id DESC
		LIMIT 1
	`, eventName, startedAt, startedAt, economyEventLinkWindowMinutes, startedAt).Scan(&runID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("find event run for draft session: %w", err)
	}

	if errors.Is(err, sql.ErrNoRows) {
		result, insertErr := tx.ExecContext(ctx, `
			INSERT INTO event_runs (
				event_name, event_type, draft_session_id, status, started_at, updated_at
			) VALUES (?, ?, ?, 'active', ?, ?)
		`, eventName, detectEventType(eventName), sessionID, nullIfEmpty(startedAt), nowUTC())
		if insertErr != nil {
			return 0, false, fmt.Errorf("insert draft event run: %w", insertErr)
		}
		runID, err = result.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("read draft event run id: %w", err)
		}
		return runID, true, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE event_runs
		SET draft_session_id = ?,
			event_type = ?,
			started_at = COALESCE(started_at, ?),
			updated_at = ?
		WHERE id = ?
	`, sessionID, detectEventType(eventName), nullIfEmpty(startedAt), nowUTC(), runID); err != nil {
		return 0, false, fmt.Errorf("attach draft session to event run: %w", err)
	}
	return runID, true, nil
}

// RepairEventRunInstances splits legacy event-name aggregates into one row per
// draft session, restores each run's record, and links retained economy
// transactions to the recovered run identity.
func (s *Store) RepairEventRunInstances(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(event_name, ''), COALESCE(started_at, ''), COALESCE(completed_at, '')
		FROM draft_sessions
		WHERE COALESCE(event_name, '') != ''
		ORDER BY COALESCE(started_at, created_at), id
	`)
	if err != nil {
		return fmt.Errorf("list draft sessions for event run repair: %w", err)
	}
	type draftRun struct {
		sessionID   int64
		eventName   string
		startedAt   string
		completedAt string
	}
	drafts := make([]draftRun, 0)
	for rows.Next() {
		var draft draftRun
		if err := rows.Scan(&draft.sessionID, &draft.eventName, &draft.startedAt, &draft.completedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan draft session for event run repair: %w", err)
		}
		drafts = append(drafts, draft)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate draft sessions for event run repair: %w", err)
	}
	rows.Close()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin event run repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, draft := range drafts {
		if _, _, err := s.attachDraftSessionToEventRun(ctx, tx, draft.sessionID, draft.eventName, draft.startedAt); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit draft event run repair: %w", err)
	}

	for _, draft := range drafts {
		candidate, ok, err := s.resolveDraftSessionDeckCandidate(ctx, draft.eventName, draft.startedAt, draft.completedAt)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		endedAt := ""
		if !candidate.LastPlayedAt.IsZero() {
			endedAt = candidate.LastPlayedAt.UTC().Format(time.RFC3339Nano)
		}
		status := "active"
		if candidate.Wins >= 7 || candidate.Losses >= 3 {
			status = "claimed"
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE event_runs
			SET wins = ?,
				losses = ?,
				status = CASE WHEN ? = 'claimed' THEN 'claimed' ELSE status END,
				ended_at = COALESCE(ended_at, ?),
				updated_at = ?
			WHERE draft_session_id = ?
		`, candidate.Wins, candidate.Losses, status, nullIfEmpty(endedAt), nowUTC(), draft.sessionID); err != nil {
			return fmt.Errorf("restore draft event run record: %w", err)
		}
	}

	tx, err = s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin event transaction link repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.repairUnlinkedEconomyTransactionsTx(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event transaction link repair: %w", err)
	}
	return nil
}
