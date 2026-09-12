package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// EventCourseRecord contains Arena's cumulative record for a single paid run.
// CourseID is also the SourceId used by entry, refund, and reward transactions.
type EventCourseRecord struct {
	CourseID      string
	EventName     string
	DeckID        string
	CurrentModule string
	Wins          int64
	Losses        int64
}

func (s *Store) UpsertEventCourse(ctx context.Context, tx *sql.Tx, course EventCourseRecord, observedAt string) error {
	course.CourseID = strings.TrimSpace(course.CourseID)
	course.EventName = strings.TrimSpace(course.EventName)
	if course.CourseID == "" || course.EventName == "" {
		return nil
	}
	observedAt = normalizeTS(observedAt)
	// Records are cumulative. Replaying an older log must not replace a final
	// course with the 0-0 observation made when it was joined.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_courses (
			course_id, event_name, deck_id, current_module, wins, losses,
			first_observed_at, observed_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(course_id) DO UPDATE SET
			event_name = excluded.event_name,
			deck_id = COALESCE(excluded.deck_id, event_courses.deck_id),
			first_observed_at = CASE
				WHEN event_courses.first_observed_at IS NULL THEN excluded.first_observed_at
				WHEN julianday(excluded.first_observed_at) < julianday(event_courses.first_observed_at) THEN excluded.first_observed_at
				ELSE event_courses.first_observed_at END,
			current_module = CASE
				WHEN excluded.wins + excluded.losses > event_courses.wins + event_courses.losses
					OR (excluded.wins + excluded.losses = event_courses.wins + event_courses.losses
						AND COALESCE(julianday(excluded.observed_at), 0) >= COALESCE(julianday(event_courses.observed_at), 0))
				THEN COALESCE(excluded.current_module, event_courses.current_module) ELSE event_courses.current_module END,
			wins = CASE WHEN excluded.wins + excluded.losses >= event_courses.wins + event_courses.losses
				THEN excluded.wins ELSE event_courses.wins END,
			losses = CASE WHEN excluded.wins + excluded.losses >= event_courses.wins + event_courses.losses
				THEN excluded.losses ELSE event_courses.losses END,
			observed_at = CASE
				WHEN excluded.wins + excluded.losses > event_courses.wins + event_courses.losses
					OR (excluded.wins + excluded.losses = event_courses.wins + event_courses.losses
						AND julianday(excluded.observed_at) > julianday(event_courses.observed_at))
				THEN excluded.observed_at ELSE event_courses.observed_at END,
			updated_at = excluded.updated_at
	`, course.CourseID, course.EventName, nullIfEmpty(course.DeckID), nullIfEmpty(course.CurrentModule),
		course.Wins, course.Losses, nullIfEmpty(observedAt), nullIfEmpty(observedAt), nowUTC()); err != nil {
		return fmt.Errorf("upsert event course: %w", err)
	}
	return s.reconcileEventCourseTx(ctx, tx, course.CourseID)
}

func (s *Store) reconcileEventCourseTx(ctx context.Context, tx *sql.Tx, courseID string) error {
	var course EventCourseRecord
	var firstObservedAt, observedAt string
	if err := tx.QueryRowContext(ctx, `
		SELECT course_id, event_name, COALESCE(deck_id, ''), COALESCE(current_module, ''),
			wins, losses, COALESCE(first_observed_at, ''), COALESCE(observed_at, '')
		FROM event_courses WHERE course_id = ?
	`, courseID).Scan(&course.CourseID, &course.EventName, &course.DeckID, &course.CurrentModule,
		&course.Wins, &course.Losses, &firstObservedAt, &observedAt); err != nil {
		return fmt.Errorf("read event course: %w", err)
	}

	var existingID, runID int64
	var sessionID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT id, draft_session_id FROM event_runs WHERE pay_source_id = ?
		ORDER BY draft_session_id IS NULL, id LIMIT 1
	`, courseID).Scan(&existingID, &sessionID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("find course event run: %w", err)
	}
	if sessionID.Valid {
		runID = existingID
	} else {
		// A retained GetCourses response can be much later than the draft. Its
		// exact deck's first match is stronger evidence than response proximity.
		var deckAnchor string
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MIN(COALESCE(m.started_at, m.ended_at)), '')
			FROM decks d JOIN match_decks md ON md.deck_id = d.id JOIN matches m ON m.id = md.match_id
			WHERE d.arena_deck_id = ?
		`, course.DeckID).Scan(&deckAnchor); err != nil {
			return fmt.Errorf("find course deck boundary: %w", err)
		}
		anchor := deckAnchor
		if anchor == "" {
			anchor = firstObservedAt
		}
		if anchor != "" {
			err = tx.QueryRowContext(ctx, `
				SELECT er.id
				FROM draft_sessions ds JOIN event_runs er ON er.draft_session_id = ds.id
				WHERE ds.event_name = ? AND COALESCE(ds.started_at, '') != ''
				  AND (COALESCE(er.pay_source_id, '') = '' OR er.pay_source_id = ?)
				  AND julianday(COALESCE(er.started_at, ds.started_at)) <= julianday(?)
				  AND NOT EXISTS (
					SELECT 1 FROM draft_sessions next WHERE next.event_name = ds.event_name
					AND julianday(next.started_at) > julianday(ds.started_at)
					AND julianday(next.started_at) <= julianday(?)
				  )
				ORDER BY ds.started_at DESC, ds.id DESC LIMIT 1
			`, course.EventName, courseID, anchor, anchor).Scan(&runID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("find course draft interval: %w", err)
			}
			if runID == 0 && deckAnchor == "" {
				// EventJoin's response normally precedes the first draft pick.
				err = tx.QueryRowContext(ctx, `
					SELECT id FROM event_runs WHERE event_name = ?
					AND (COALESCE(pay_source_id, '') = '' OR pay_source_id = ?)
					AND ABS(julianday(started_at) - julianday(?)) * 1440.0 <= ?
					ORDER BY ABS(julianday(started_at) - julianday(?)), id LIMIT 1
				`, course.EventName, courseID, anchor, economyEventLinkWindowMinutes, anchor).Scan(&runID)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("find course join observation: %w", err)
				}
			}
		}
	}
	if runID == 0 {
		runID = existingID
	}
	if runID == 0 {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO event_runs (event_name, event_type, pay_source_id, started_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, course.EventName, detectEventType(course.EventName), courseID, nullIfEmpty(firstObservedAt), nowUTC())
		if err != nil {
			return fmt.Errorf("insert course event run: %w", err)
		}
		runID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read course event run id: %w", err)
		}
	}
	if existingID > 0 && existingID != runID {
		if err := mergeEventRunsTx(ctx, tx, runID, existingID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE event_runs SET pay_source_id = ?, wins = ?, losses = ?,
			status = CASE WHEN ? = 'Complete' THEN 'claimed' ELSE status END,
			ended_at = CASE WHEN ? IN ('ClaimPrize', 'Complete') AND julianday(?) >= julianday(started_at)
				THEN ?
				ELSE ended_at END,
			updated_at = ? WHERE id = ?
	`, courseID, course.Wins, course.Losses, course.CurrentModule, course.CurrentModule, observedAt, nullIfEmpty(observedAt), nowUTC(), runID); err != nil {
		return fmt.Errorf("apply course record: %w", err)
	}
	// Repair existing proximity links as well as unlinked observations. The
	// source identity is conclusive even if a previous replay chose a phantom.
	if _, err := tx.ExecContext(ctx, `
		UPDATE economy_transactions SET event_run_id = ?, event_name = ?, event_link = 'source_id'
		WHERE source_id = ? AND source IN ('EventPayEntry', 'EventReward', 'EventRefund')
	`, runID, course.EventName, courseID); err != nil {
		return fmt.Errorf("link course economy transactions: %w", err)
	}
	return nil
}

func mergeEventRunsTx(ctx context.Context, tx *sql.Tx, keepID, removeID int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE event_runs AS kept SET
			entry_currency_type = COALESCE(entry_currency_type, (SELECT entry_currency_type FROM event_runs WHERE id = ?)),
			entry_currency_paid = COALESCE(entry_currency_paid, (SELECT entry_currency_paid FROM event_runs WHERE id = ?)),
			pay_source_id = COALESCE(pay_source_id, (SELECT pay_source_id FROM event_runs WHERE id = ?)),
			ended_at = COALESCE(ended_at, (SELECT ended_at FROM event_runs removed WHERE id = ? AND julianday(removed.ended_at) >= julianday(kept.started_at))),
			status = CASE WHEN status = 'claimed' OR (SELECT status FROM event_runs WHERE id = ?) = 'claimed' THEN 'claimed' ELSE status END,
			updated_at = ? WHERE id = ?
	`, removeID, removeID, removeID, removeID, removeID, nowUTC(), keepID); err != nil {
		return fmt.Errorf("merge event run evidence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE economy_transactions SET event_run_id = ? WHERE event_run_id = ?`, keepID, removeID); err != nil {
		return fmt.Errorf("move duplicate event ledger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM event_runs WHERE id = ? AND draft_session_id IS NULL`, removeID); err != nil {
		return fmt.Errorf("remove duplicate event run: %w", err)
	}
	return nil
}

func (s *Store) repairEventCoursesTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT course_id FROM event_courses ORDER BY first_observed_at, course_id`)
	if err != nil {
		return fmt.Errorf("list event courses for repair: %w", err)
	}
	var courseIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan event course for repair: %w", err)
		}
		courseIDs = append(courseIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate event courses for repair: %w", err)
	}
	rows.Close()
	for _, id := range courseIDs {
		if err := s.reconcileEventCourseTx(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) repairDuplicateDraftRunsTx(ctx context.Context, tx *sql.Tx) error {
	// Only merge a session-less replay row inside a known draft's lifetime.
	// Distinct payments and undated/missing-log history are never discarded.
	rows, err := tx.QueryContext(ctx, `
		SELECT kept.id, duplicate.id
		FROM event_runs duplicate
		JOIN event_runs kept ON kept.event_name = duplicate.event_name
		JOIN draft_sessions ds ON ds.id = kept.draft_session_id
		WHERE duplicate.draft_session_id IS NULL
		  AND COALESCE(duplicate.started_at, '') != ''
		  AND julianday(duplicate.started_at) >= julianday(COALESCE(kept.started_at, ds.started_at))
		  AND (COALESCE(duplicate.pay_source_id, '') = '' OR duplicate.pay_source_id = kept.pay_source_id)
		  AND NOT EXISTS (
			SELECT 1 FROM economy_transactions et WHERE et.event_run_id = duplicate.id
			AND et.source IN ('EventPayEntry', 'EventReward', 'EventRefund')
			AND COALESCE(et.source_id, '') != '' AND et.source_id != COALESCE(kept.pay_source_id, '')
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM draft_sessions next WHERE next.event_name = ds.event_name
			AND julianday(next.started_at) > julianday(ds.started_at)
			AND julianday(next.started_at) <= julianday(duplicate.started_at)
		  )
		  AND (
			duplicate.pay_source_id = kept.pay_source_id
			OR (
				julianday(duplicate.started_at) <= julianday(kept.ended_at)
				AND (
					ABS(julianday(duplicate.started_at) - julianday(ds.started_at)) * 1440.0 <= ?
					OR EXISTS (
						SELECT 1 FROM matches m WHERE m.event_name = kept.event_name
						AND ABS(julianday(m.started_at) - julianday(duplicate.started_at)) * 86400.0 < 1
					)
				)
			)
		  )
		ORDER BY duplicate.id, ds.started_at DESC
	`, economyEventLinkWindowMinutes)
	if err != nil {
		return fmt.Errorf("list duplicate draft runs: %w", err)
	}
	type duplicateRun struct{ keepID, removeID int64 }
	var duplicates []duplicateRun
	for rows.Next() {
		var duplicate duplicateRun
		if err := rows.Scan(&duplicate.keepID, &duplicate.removeID); err != nil {
			rows.Close()
			return fmt.Errorf("scan duplicate draft run: %w", err)
		}
		duplicates = append(duplicates, duplicate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate duplicate draft runs: %w", err)
	}
	rows.Close()
	for _, duplicate := range duplicates {
		if err := mergeEventRunsTx(ctx, tx, duplicate.keepID, duplicate.removeID); err != nil {
			return err
		}
	}
	return nil
}
