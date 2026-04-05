package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cschnabel/mtgdata/internal/model"
)

type Store struct {
	db *sql.DB
}

type IngestState struct {
	Offset int64
	LineNo int64
	Found  bool
}

type DeckCard struct {
	Section  string
	CardID   int64
	Quantity int64
}

type MatchRankSnapshot struct {
	ObservedAt               string
	PayloadJSON              string
	ConstructedSeasonOrdinal *int64
	ConstructedRankClass     string
	ConstructedLevel         *int64
	ConstructedStep          *int64
	ConstructedMatchesWon    *int64
	ConstructedMatchesLost   *int64
	LimitedSeasonOrdinal     *int64
	LimitedRankClass         string
	LimitedLevel             *int64
	LimitedStep              *int64
	LimitedMatchesWon        *int64
	LimitedMatchesLost       *int64
}

const sqliteInClauseBatchSize = 900

const matchBestOfSQL = `
	CASE
		WHEN LOWER(COALESCE(m.format, '')) IN ('bo3', 'bestofthree', 'best-of-three') THEN 'bo3'
		WHEN LOWER(COALESCE(m.format, '')) IN ('bo1', 'bestofone', 'best-of-one') THEN 'bo1'
		WHEN LOWER(COALESCE(m.event_name, '')) LIKE '%traditional%' THEN 'bo3'
		WHEN EXISTS (
			SELECT 1
			FROM match_replay_frames rf
			WHERE rf.match_id = m.id
			  AND rf.game_number > 1
		) THEN 'bo3'
		WHEN EXISTS (
			SELECT 1
			FROM match_card_plays cp
			WHERE cp.match_id = m.id
			  AND cp.game_number > 1
		) THEN 'bo3'
		WHEN EXISTS (
			SELECT 1
			FROM match_opponent_card_instances oc
			WHERE oc.match_id = m.id
			  AND oc.game_number > 1
		) THEN 'bo3'
		ELSE 'bo1'
	END
`

const matchPlayDrawSQL = `
	COALESCE((
		SELECT
			CASE
				WHEN cp.owner_seat_id = m.player_seat_id AND cp.turn_number % 2 = 1 THEN 'play'
				WHEN cp.owner_seat_id = m.player_seat_id AND cp.turn_number % 2 = 0 THEN 'draw'
				WHEN cp.owner_seat_id != m.player_seat_id AND cp.turn_number % 2 = 1 THEN 'draw'
				WHEN cp.owner_seat_id != m.player_seat_id AND cp.turn_number % 2 = 0 THEN 'play'
				ELSE ''
			END
		FROM match_card_plays cp
		WHERE cp.match_id = m.id
		  AND cp.game_number = 1
		  AND cp.owner_seat_id IS NOT NULL
		  AND cp.turn_number IS NOT NULL
		ORDER BY cp.turn_number ASC, COALESCE(cp.played_at, '') ASC, cp.id ASC
		LIMIT 1
	), '')
`

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func uniquePositiveInt64(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}

	out := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func int64Batches(values []int64, batchSize int) [][]int64 {
	values = uniquePositiveInt64(values)
	if len(values) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = sqliteInClauseBatchSize
	}

	out := make([][]int64, 0, (len(values)+batchSize-1)/batchSize)
	for start := 0; start < len(values); start += batchSize {
		end := min(start+batchSize, len(values))
		out = append(out, values[start:end])
	}
	return out
}

func normalizeTS(ts string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

func (s *Store) GetIngestState(ctx context.Context, logPath string) (IngestState, error) {
	state := IngestState{}
	err := s.db.QueryRowContext(ctx, `
		SELECT byte_offset, line_no
		FROM ingest_state
		WHERE log_path = ?
	`, logPath).Scan(&state.Offset, &state.LineNo)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("get ingest_state: %w", err)
	}
	state.Found = true
	return state, nil
}

func (s *Store) SaveIngestState(ctx context.Context, tx *sql.Tx, logPath string, offset, lineNo int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ingest_state (log_path, byte_offset, line_no, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(log_path) DO UPDATE SET
			byte_offset = excluded.byte_offset,
			line_no = excluded.line_no,
			updated_at = excluded.updated_at
	`, logPath, offset, lineNo, nowUTC())
	if err != nil {
		return fmt.Errorf("save ingest_state: %w", err)
	}
	return nil
}

func (s *Store) InsertRawEvent(ctx context.Context, tx *sql.Tx, logPath string, lineNo, byteOffset int64, kind, method, requestID string, payload []byte, rawText string) error {
	payloadText := ""
	if len(payload) > 0 {
		payloadText = string(payload)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events_raw (
			log_path, line_no, byte_offset, kind, method_name, request_id, payload_json, raw_text, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, logPath, lineNo, byteOffset, kind, method, requestID, payloadText, rawText, nowUTC())
	if err != nil {
		return fmt.Errorf("insert events_raw: %w", err)
	}
	return nil
}

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

func isDraftDeck(format, eventName string) bool {
	if strings.EqualFold(strings.TrimSpace(format), "draft") {
		return true
	}

	e := strings.ToLower(strings.TrimSpace(eventName))
	if e == "" {
		return false
	}

	return strings.Contains(e, "draft")
}

func normalizeDeckScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "constructed":
		return "constructed"
	case "draft":
		return "draft"
	case "all":
		return "all"
	default:
		return "constructed"
	}
}

func deckInScope(scope, format, eventName string) bool {
	scope = normalizeDeckScope(scope)
	isDraft := isDraftDeck(format, eventName)
	switch scope {
	case "draft":
		return isDraft
	case "all":
		return true
	default:
		return !isDraft
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
	eventType := detectEventType(eventName)
	ts = normalizeTS(ts)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO event_runs (
			event_name, event_type, entry_currency_type, entry_currency_paid, status, started_at, updated_at
		) VALUES (?, ?, ?, ?, 'active', ?, ?)
		ON CONFLICT(event_name) DO UPDATE SET
			event_type = excluded.event_type,
			entry_currency_type = COALESCE(excluded.entry_currency_type, event_runs.entry_currency_type),
			entry_currency_paid = COALESCE(excluded.entry_currency_paid, event_runs.entry_currency_paid),
			updated_at = excluded.updated_at
	`, eventName, eventType, nullIfEmpty(currencyType), nullableInt(currencyPaid), nullIfEmpty(ts), nowUTC())
	if err != nil {
		return fmt.Errorf("upsert event_runs join: %w", err)
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
		WHERE event_name = ?
	`, nullIfEmpty(ts), nowUTC(), eventName)
	if err != nil {
		return fmt.Errorf("mark event run claimed: %w", err)
	}
	return nil
}

func (s *Store) BumpEventRunRecord(ctx context.Context, tx *sql.Tx, eventName, result string) error {
	if eventName == "" || (result != "win" && result != "loss") {
		return nil
	}
	col := "wins"
	if result == "loss" {
		col = "losses"
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE event_runs
		SET %s = %s + 1,
			updated_at = ?
		WHERE event_name = ?
	`, col, col), nowUTC(), eventName)
	if err != nil {
		return fmt.Errorf("bump event run record: %w", err)
	}
	return nil
}

func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableIntPtr(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullIfEmpty(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func (s *Store) UpsertDeck(ctx context.Context, tx *sql.Tx, arenaDeckID, eventName, name, format, source, lastUpdated string, cards []DeckCard) (int64, error) {
	now := nowUTC()
	lastUpdated = normalizeTS(lastUpdated)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO decks (
			arena_deck_id, event_name, name, format, source, last_updated, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(arena_deck_id) DO UPDATE SET
			event_name = COALESCE(excluded.event_name, decks.event_name),
			name = COALESCE(excluded.name, decks.name),
			format = COALESCE(excluded.format, decks.format),
			source = COALESCE(excluded.source, decks.source),
			last_updated = COALESCE(excluded.last_updated, decks.last_updated),
			updated_at = excluded.updated_at
	`, arenaDeckID, nullIfEmpty(eventName), nullIfEmpty(name), nullIfEmpty(format), nullIfEmpty(source), nullIfEmpty(lastUpdated), now, now)
	if err != nil {
		return 0, fmt.Errorf("upsert deck: %w", err)
	}

	var deckID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM decks WHERE arena_deck_id = ?`, arenaDeckID).Scan(&deckID)
	if err != nil {
		return 0, fmt.Errorf("fetch deck id: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM deck_cards WHERE deck_id = ?`, deckID); err != nil {
		return 0, fmt.Errorf("clear deck_cards: %w", err)
	}

	for _, c := range cards {
		if c.Quantity <= 0 {
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO deck_cards (deck_id, section, card_id, quantity)
			VALUES (?, ?, ?, ?)
		`, deckID, c.Section, c.CardID, c.Quantity)
		if err != nil {
			return 0, fmt.Errorf("insert deck_card: %w", err)
		}
	}

	return deckID, nil
}

func (s *Store) UpsertMatchStart(ctx context.Context, tx *sql.Tx, arenaMatchID, eventName string, seatID int64, startedAt string) (int64, error) {
	resolvedEventName := eventName
	if eventName != "" {
		alias, err := s.resolveEventNameAlias(ctx, tx, eventName)
		if err != nil {
			return 0, err
		}
		resolvedEventName = alias
	}

	startedAt = normalizeTS(startedAt)
	now := nowUTC()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO matches (
			arena_match_id, event_name, player_seat_id, started_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(arena_match_id) DO UPDATE SET
			event_name = COALESCE(excluded.event_name, matches.event_name),
			player_seat_id = COALESCE(excluded.player_seat_id, matches.player_seat_id),
			started_at = COALESCE(matches.started_at, excluded.started_at),
			updated_at = excluded.updated_at
	`, arenaMatchID, nullIfEmpty(resolvedEventName), nullableInt(seatID), nullIfEmpty(startedAt), now, now)
	if err != nil {
		return 0, fmt.Errorf("upsert match start: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("fetch match id: %w", err)
	}

	if resolvedEventName != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO event_runs (event_name, event_type, status, started_at, updated_at)
			VALUES (?, ?, 'active', ?, ?)
			ON CONFLICT(event_name) DO UPDATE SET updated_at = excluded.updated_at
		`, resolvedEventName, detectEventType(resolvedEventName), nullIfEmpty(startedAt), now); err != nil {
			return 0, fmt.Errorf("ensure event run from match start: %w", err)
		}
	}

	return id, nil
}

func (s *Store) UpdateMatchOpponent(ctx context.Context, tx *sql.Tx, arenaMatchID, opponentName, opponentUserID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE matches
		SET opponent_name = COALESCE(NULLIF(?, ''), opponent_name),
			opponent_user_id = COALESCE(NULLIF(?, ''), opponent_user_id),
			updated_at = ?
		WHERE arena_match_id = ?
	`, strings.TrimSpace(opponentName), strings.TrimSpace(opponentUserID), nowUTC(), arenaMatchID)
	if err != nil {
		return fmt.Errorf("update match opponent: %w", err)
	}
	return nil
}

func (s *Store) UpsertMatchOpponentCardInstance(ctx context.Context, tx *sql.Tx, arenaMatchID string, gameNumber, instanceID, cardID int64, firstSeenAt, source string) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" || instanceID <= 0 || cardID <= 0 {
		return nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO match_opponent_card_instances (
			match_id, game_number, instance_id, card_id, source, first_seen_at, created_at
		)
		SELECT
			m.id, ?, ?, ?, ?, ?, ?
		FROM matches m
		WHERE m.arena_match_id = ?
		ON CONFLICT(match_id, game_number, instance_id) DO NOTHING
	`, gameNumber, instanceID, cardID, nullIfEmpty(source), nullIfEmpty(normalizeTS(firstSeenAt)), nowUTC(), arenaMatchID)
	if err != nil {
		return fmt.Errorf("upsert match opponent card instance: %w", err)
	}
	return nil
}

func (s *Store) UpsertMatchCardPlay(ctx context.Context, tx *sql.Tx, arenaMatchID string, gameNumber, instanceID, cardID, ownerSeatID, turnNumber int64, phase, firstPublicZone, playedAt, source string) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	firstPublicZone = strings.TrimSpace(firstPublicZone)
	if arenaMatchID == "" || instanceID <= 0 || cardID <= 0 || firstPublicZone == "" {
		return nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO match_card_plays (
			match_id, game_number, instance_id, card_id, owner_seat_id, first_public_zone, turn_number, phase, source, played_at, created_at
		)
		SELECT
			m.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM matches m
		WHERE m.arena_match_id = ?
		ON CONFLICT(match_id, game_number, instance_id) DO UPDATE SET
			owner_seat_id = COALESCE(match_card_plays.owner_seat_id, excluded.owner_seat_id),
			turn_number = COALESCE(match_card_plays.turn_number, excluded.turn_number),
			phase = COALESCE(match_card_plays.phase, excluded.phase),
			source = COALESCE(match_card_plays.source, excluded.source),
			played_at = COALESCE(match_card_plays.played_at, excluded.played_at)
		WHERE
			match_card_plays.owner_seat_id IS NULL
			OR match_card_plays.turn_number IS NULL
			OR match_card_plays.phase IS NULL
			OR match_card_plays.source IS NULL
			OR match_card_plays.played_at IS NULL
	`, gameNumber, instanceID, cardID, nullableInt(ownerSeatID), firstPublicZone, nullableInt(turnNumber), nullIfEmpty(phase), nullIfEmpty(source), nullIfEmpty(normalizeTS(playedAt)), nowUTC(), arenaMatchID)
	if err != nil {
		return fmt.Errorf("upsert match card play: %w", err)
	}
	return nil
}

func (s *Store) UpdateMatchEnd(ctx context.Context, tx *sql.Tx, arenaMatchID string, teamID, winningTeamID, turnCount, secondsCount int64, winReason, endedAt string) (string, string, bool, error) {
	endedAt = normalizeTS(endedAt)

	var eventName string
	var priorResult string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(event_name, ''), COALESCE(result, '')
		FROM matches
		WHERE arena_match_id = ?
	`, arenaMatchID).Scan(&eventName, &priorResult)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO matches (arena_match_id, ended_at, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, arenaMatchID, endedAt, nowUTC(), nowUTC()); err != nil {
			return "", "", false, fmt.Errorf("create ended-only match: %w", err)
		}
		eventName = ""
	} else if err != nil {
		return "", "", false, fmt.Errorf("get match event name: %w", err)
	}

	result := "unknown"
	if teamID > 0 && winningTeamID > 0 {
		if teamID == winningTeamID {
			result = "win"
		} else {
			result = "loss"
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE matches
		SET ended_at = COALESCE(?, ended_at),
			result = ?,
			win_reason = COALESCE(?, win_reason),
			turn_count = COALESCE(?, turn_count),
			seconds_count = COALESCE(?, seconds_count),
			updated_at = ?
		WHERE arena_match_id = ?
	`, nullIfEmpty(endedAt), result, nullIfEmpty(winReason), nullableInt(turnCount), nullableInt(secondsCount), nowUTC(), arenaMatchID)
	if err != nil {
		return "", "", false, fmt.Errorf("update match end: %w", err)
	}

	terminalChange := (result == "win" || result == "loss") && result != priorResult

	// Idempotency guard: only increment run record when match result changes into a terminal result.
	if eventName != "" && terminalChange {
		if err := s.BumpEventRunRecord(ctx, tx, eventName, result); err != nil {
			return "", "", false, err
		}
	}

	return eventName, result, terminalChange, nil
}

func (s *Store) MatchHasRankSnapshot(ctx context.Context, tx *sql.Tx, arenaMatchID string) (bool, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return false, nil
	}

	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM matches m
		JOIN match_rank_snapshots mrs ON mrs.match_id = m.id
		WHERE m.arena_match_id = ?
		LIMIT 1
	`, arenaMatchID).Scan(new(int64))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check match rank snapshot: %w", err)
	}
	return true, nil
}

func (s *Store) UpsertMatchRankSnapshot(ctx context.Context, tx *sql.Tx, arenaMatchID string, snapshot MatchRankSnapshot) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return nil
	}

	var matchID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM matches
		WHERE arena_match_id = ?
	`, arenaMatchID).Scan(&matchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup rank snapshot match: %w", err)
	}

	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM match_rank_snapshots
		WHERE match_id = ?
	`, matchID).Scan(new(int64))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lookup existing rank snapshot: %w", err)
	}

	prevSnapshotID := any(nil)
	if errors.Is(err, sql.ErrNoRows) {
		var prevID int64
		err = tx.QueryRowContext(ctx, `
			SELECT id
			FROM match_rank_snapshots
			ORDER BY COALESCE(observed_at, created_at) DESC, id DESC
			LIMIT 1
		`).Scan(&prevID)
		if err == nil {
			prevSnapshotID = prevID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lookup previous rank snapshot: %w", err)
		}
	}

	snapshot.ObservedAt = normalizeTS(snapshot.ObservedAt)
	now := nowUTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO match_rank_snapshots (
			match_id,
			prev_snapshot_id,
			observed_at,
			payload_json,
			constructed_season_ordinal,
			constructed_rank_class,
			constructed_level,
			constructed_step,
			constructed_matches_won,
			constructed_matches_lost,
			limited_season_ordinal,
			limited_rank_class,
			limited_level,
			limited_step,
			limited_matches_won,
			limited_matches_lost,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id) DO UPDATE SET
			prev_snapshot_id = COALESCE(match_rank_snapshots.prev_snapshot_id, excluded.prev_snapshot_id),
			observed_at = COALESCE(excluded.observed_at, match_rank_snapshots.observed_at),
			payload_json = excluded.payload_json,
			constructed_season_ordinal = COALESCE(excluded.constructed_season_ordinal, match_rank_snapshots.constructed_season_ordinal),
			constructed_rank_class = COALESCE(excluded.constructed_rank_class, match_rank_snapshots.constructed_rank_class),
			constructed_level = COALESCE(excluded.constructed_level, match_rank_snapshots.constructed_level),
			constructed_step = COALESCE(excluded.constructed_step, match_rank_snapshots.constructed_step),
			constructed_matches_won = COALESCE(excluded.constructed_matches_won, match_rank_snapshots.constructed_matches_won),
			constructed_matches_lost = COALESCE(excluded.constructed_matches_lost, match_rank_snapshots.constructed_matches_lost),
			limited_season_ordinal = COALESCE(excluded.limited_season_ordinal, match_rank_snapshots.limited_season_ordinal),
			limited_rank_class = COALESCE(excluded.limited_rank_class, match_rank_snapshots.limited_rank_class),
			limited_level = COALESCE(excluded.limited_level, match_rank_snapshots.limited_level),
			limited_step = COALESCE(excluded.limited_step, match_rank_snapshots.limited_step),
			limited_matches_won = COALESCE(excluded.limited_matches_won, match_rank_snapshots.limited_matches_won),
			limited_matches_lost = COALESCE(excluded.limited_matches_lost, match_rank_snapshots.limited_matches_lost),
			updated_at = excluded.updated_at
	`, matchID, prevSnapshotID, nullIfEmpty(snapshot.ObservedAt), snapshot.PayloadJSON,
		nullableIntPtr(snapshot.ConstructedSeasonOrdinal), nullIfEmpty(snapshot.ConstructedRankClass),
		nullableIntPtr(snapshot.ConstructedLevel), nullableIntPtr(snapshot.ConstructedStep),
		nullableIntPtr(snapshot.ConstructedMatchesWon), nullableIntPtr(snapshot.ConstructedMatchesLost),
		nullableIntPtr(snapshot.LimitedSeasonOrdinal), nullIfEmpty(snapshot.LimitedRankClass),
		nullableIntPtr(snapshot.LimitedLevel), nullableIntPtr(snapshot.LimitedStep),
		nullableIntPtr(snapshot.LimitedMatchesWon), nullableIntPtr(snapshot.LimitedMatchesLost),
		now, now)
	if err != nil {
		return fmt.Errorf("upsert match rank snapshot: %w", err)
	}

	return nil
}

func (s *Store) LinkMatchToLatestDeckByEvent(ctx context.Context, tx *sql.Tx, arenaMatchID, eventName, reason string) error {
	if eventName == "" {
		return nil
	}
	alias, err := s.resolveEventNameAlias(ctx, tx, eventName)
	if err != nil {
		return err
	}
	if alias != "" {
		eventName = alias
	}

	var matchID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&matchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get match id: %w", err)
	}

	var deckID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM decks
		WHERE event_name = ?
		ORDER BY COALESCE(last_updated, updated_at) DESC
		LIMIT 1
	`, eventName).Scan(&deckID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find deck for match: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO match_decks (match_id, deck_id, snapshot_reason, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(match_id, deck_id) DO NOTHING
	`, matchID, deckID, reason, nowUTC())
	if err != nil {
		return fmt.Errorf("link match_deck: %w", err)
	}

	return nil
}

func (s *Store) EnsureDraftSession(ctx context.Context, tx *sql.Tx, eventName string, draftID *string, isBot bool, ts string) (int64, error) {
	isBotInt := 0
	if isBot {
		isBotInt = 1
	}
	ts = normalizeTS(ts)

	var sessionID int64
	if draftID != nil && *draftID != "" {
		err := tx.QueryRowContext(ctx, `SELECT id FROM draft_sessions WHERE draft_id = ? AND is_bot_draft = ?`, *draftID, isBotInt).Scan(&sessionID)
		if err == nil {
			_, _ = tx.ExecContext(ctx, `
				UPDATE draft_sessions
				SET event_name = COALESCE(?, event_name), started_at = COALESCE(started_at, ?), updated_at = ?
				WHERE id = ?
			`, nullIfEmpty(eventName), nullIfEmpty(ts), nowUTC(), sessionID)
			return sessionID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("select draft session by draft_id: %w", err)
		}
	}

	if draftID == nil || *draftID == "" {
		// For bot drafts (or unknown IDs), reuse active session for same event if incomplete.
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM draft_sessions
			WHERE event_name = ? AND is_bot_draft = ? AND completed_at IS NULL
			ORDER BY id DESC
			LIMIT 1
		`, eventName, isBotInt).Scan(&sessionID)
		if err == nil {
			_, _ = tx.ExecContext(ctx, `
				UPDATE draft_sessions
				SET started_at = COALESCE(started_at, ?), updated_at = ?
				WHERE id = ?
			`, nullIfEmpty(ts), nowUTC(), sessionID)
			return sessionID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("select active draft session: %w", err)
		}
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO draft_sessions (event_name, draft_id, is_bot_draft, started_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, nullIfEmpty(eventName), nullDraftID(draftID), isBotInt, nullIfEmpty(ts), nowUTC(), nowUTC())
	if err != nil {
		return 0, fmt.Errorf("insert draft_session: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `SELECT last_insert_rowid()`).Scan(&sessionID); err != nil {
		return 0, fmt.Errorf("last_insert_rowid draft_session: %w", err)
	}

	return sessionID, nil
}

func nullDraftID(v *string) any {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func (s *Store) InsertDraftPick(ctx context.Context, tx *sql.Tx, sessionID int64, packNo, pickNo int64, pickedIDs []int64, packIDs []int64, ts string) error {
	pickedJSON, _ := json.Marshal(pickedIDs)
	packJSON := []byte("[]")
	if len(packIDs) > 0 {
		packJSON, _ = json.Marshal(packIDs)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO draft_picks (
			draft_session_id, pack_number, pick_number, picked_card_ids, pack_card_ids, pick_ts, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(draft_session_id, pack_number, pick_number) DO UPDATE SET
			picked_card_ids = excluded.picked_card_ids,
			pack_card_ids = excluded.pack_card_ids,
			pick_ts = COALESCE(excluded.pick_ts, draft_picks.pick_ts)
	`, sessionID, packNo, pickNo, string(pickedJSON), string(packJSON), nullIfEmpty(normalizeTS(ts)), nowUTC())
	if err != nil {
		return fmt.Errorf("insert draft_pick: %w", err)
	}

	_, _ = tx.ExecContext(ctx, `UPDATE draft_sessions SET updated_at = ? WHERE id = ?`, nowUTC(), sessionID)
	return nil
}

func (s *Store) CompleteDraftSession(ctx context.Context, tx *sql.Tx, eventName string, draftID *string, isBot bool, ts string) error {
	isBotInt := 0
	if isBot {
		isBotInt = 1
	}
	ts = normalizeTS(ts)

	if draftID != nil && strings.TrimSpace(*draftID) != "" {
		_, err := tx.ExecContext(ctx, `
			UPDATE draft_sessions
			SET completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE draft_id = ? AND is_bot_draft = ?
		`, nullIfEmpty(ts), nowUTC(), strings.TrimSpace(*draftID), isBotInt)
		if err != nil {
			return fmt.Errorf("complete draft session by draft_id: %w", err)
		}
		return nil
	}

	if eventName != "" {
		res, err := tx.ExecContext(ctx, `
			UPDATE draft_sessions
			SET event_name = COALESCE(?, event_name), completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE id = (
				SELECT id FROM draft_sessions
				WHERE is_bot_draft = ?
				  AND completed_at IS NULL
				  AND (
					event_name = ?
					OR COALESCE(event_name, '') = ''
				  )
				ORDER BY CASE
					WHEN event_name = ? THEN 0
					WHEN COALESCE(event_name, '') = '' THEN 1
					ELSE 2
				END,
				COALESCE(started_at, updated_at, created_at) DESC,
				id DESC
				LIMIT 1
			)
		`, nullIfEmpty(eventName), nullIfEmpty(ts), nowUTC(), isBotInt, eventName, eventName)
		if err != nil {
			return fmt.Errorf("complete draft session by event_name: %w", err)
		}
		if rowsAffected, rowsErr := res.RowsAffected(); rowsErr == nil && rowsAffected > 0 {
			return nil
		}
	}

	return nil
}

func (s *Store) RepairDraftDataFromRawEvents(ctx context.Context) error {
	now := nowUTC()

	_, err := s.db.ExecContext(ctx, `
		UPDATE draft_sessions
		SET
			event_name = COALESCE(
				NULLIF(draft_sessions.event_name, ''),
				(
					SELECT COALESCE(
						NULLIF(json_extract(er.payload_json, '$.EventId'), ''),
						NULLIF(json_extract(er.payload_json, '$.EventName'), '')
					)
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
					ORDER BY COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at) DESC, er.id DESC
					LIMIT 1
				),
				(
					SELECT NULLIF(json_extract(er.payload_json, '$.EventName'), '')
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'DraftCompleteDraft'
					  AND json_extract(er.payload_json, '$.IsBotDraft') = draft_sessions.is_bot_draft
					  AND er.created_at >= draft_sessions.updated_at
					ORDER BY er.created_at ASC, er.id ASC
					LIMIT 1
				),
				(
					SELECT NULLIF(json_extract(er.payload_json, '$.EventName'), '')
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventSetDeckV2'
					  AND LOWER(COALESCE(json_extract(er.payload_json, '$.EventName'), '')) LIKE '%draft%'
					  AND er.created_at >= draft_sessions.updated_at
					ORDER BY er.created_at ASC, er.id ASC
					LIMIT 1
				)
			),
			started_at = COALESCE(
				NULLIF(draft_sessions.started_at, ''),
				(
					SELECT MIN(COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at))
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				),
				(
					SELECT MIN(er.created_at)
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventPlayerDraftMakePick'
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
			),
			completed_at = COALESCE(
				NULLIF(draft_sessions.completed_at, ''),
				(
					SELECT MAX(COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at))
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				),
				(
					SELECT er.created_at
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'DraftCompleteDraft'
					  AND json_extract(er.payload_json, '$.IsBotDraft') = draft_sessions.is_bot_draft
					  AND er.created_at >= draft_sessions.updated_at
					ORDER BY er.created_at ASC, er.id ASC
					LIMIT 1
				),
				(
					SELECT MAX(er.created_at)
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventPlayerDraftMakePick'
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
			),
			updated_at = ?
		WHERE draft_sessions.draft_id IS NOT NULL
		  AND (
			COALESCE(draft_sessions.event_name, '') = ''
			OR COALESCE(draft_sessions.started_at, '') = ''
			OR COALESCE(draft_sessions.completed_at, '') = ''
		  )
		  AND EXISTS (
			SELECT 1
			FROM events_raw er
			WHERE er.kind = 'outgoing'
			  AND (
				(
					er.method_name = 'LogBusinessEvents'
					AND json_extract(er.payload_json, '$.EventType') = 24
					AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
				OR (
					er.method_name = 'EventPlayerDraftMakePick'
					AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
				OR (
					er.method_name = 'DraftCompleteDraft'
					AND json_extract(er.payload_json, '$.IsBotDraft') = draft_sessions.is_bot_draft
					AND er.created_at >= draft_sessions.updated_at
				)
			  )
		  )
	`, now)
	if err != nil {
		return fmt.Errorf("repair draft sessions from raw events: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE draft_picks
		SET
			pick_ts = COALESCE(
				NULLIF(draft_picks.pick_ts, ''),
				(
					SELECT COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at)
					FROM events_raw er
					JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND ds.id = draft_picks.draft_session_id
					  AND CAST(json_extract(er.payload_json, '$.PackNumber') AS INTEGER) = draft_picks.pack_number
					  AND CAST(json_extract(er.payload_json, '$.PickNumber') AS INTEGER) = draft_picks.pick_number
					ORDER BY er.id DESC
					LIMIT 1
				),
				(
					SELECT er.created_at
					FROM events_raw er
					JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventPlayerDraftMakePick'
					  AND ds.id = draft_picks.draft_session_id
					  AND CAST(json_extract(er.payload_json, '$.Pack') AS INTEGER) = draft_picks.pack_number
					  AND CAST(json_extract(er.payload_json, '$.Pick') AS INTEGER) = draft_picks.pick_number
					ORDER BY er.id DESC
					LIMIT 1
				)
			),
			pack_card_ids = CASE
				WHEN COALESCE(draft_picks.pack_card_ids, '') IN ('', '[]') THEN COALESCE(
					(
						SELECT json_extract(er.payload_json, '$.CardsInPack')
						FROM events_raw er
						JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
						WHERE er.kind = 'outgoing'
						  AND er.method_name = 'LogBusinessEvents'
						  AND json_extract(er.payload_json, '$.EventType') = 24
						  AND ds.id = draft_picks.draft_session_id
						  AND CAST(json_extract(er.payload_json, '$.PackNumber') AS INTEGER) = draft_picks.pack_number
						  AND CAST(json_extract(er.payload_json, '$.PickNumber') AS INTEGER) = draft_picks.pick_number
						ORDER BY er.id DESC
						LIMIT 1
					),
					draft_picks.pack_card_ids
				)
				ELSE draft_picks.pack_card_ids
			END
		WHERE (
			COALESCE(draft_picks.pick_ts, '') = ''
			OR COALESCE(draft_picks.pack_card_ids, '') IN ('', '[]')
		)
		  AND EXISTS (
			SELECT 1
			FROM events_raw er
			JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
			WHERE er.kind = 'outgoing'
			  AND (
				(
					er.method_name = 'LogBusinessEvents'
					AND json_extract(er.payload_json, '$.EventType') = 24
					AND ds.id = draft_picks.draft_session_id
					AND CAST(json_extract(er.payload_json, '$.PackNumber') AS INTEGER) = draft_picks.pack_number
					AND CAST(json_extract(er.payload_json, '$.PickNumber') AS INTEGER) = draft_picks.pick_number
				)
				OR (
					er.method_name = 'EventPlayerDraftMakePick'
					AND ds.id = draft_picks.draft_session_id
					AND CAST(json_extract(er.payload_json, '$.Pack') AS INTEGER) = draft_picks.pack_number
					AND CAST(json_extract(er.payload_json, '$.Pick') AS INTEGER) = draft_picks.pick_number
				)
			  )
		  )
	`)
	if err != nil {
		return fmt.Errorf("repair draft picks from raw events: %w", err)
	}

	return nil
}

func (s *Store) Overview(ctx context.Context, recentLimit int64) (model.Overview, error) {
	out := model.Overview{}
	if recentLimit <= 0 {
		recentLimit = 20
	}

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN result = 'win' THEN 1 ELSE 0 END), 0) AS wins,
			COALESCE(SUM(CASE WHEN result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
		FROM matches
	`).Scan(&out.TotalMatches, &out.Wins, &out.Losses)
	if err != nil {
		return out, fmt.Errorf("overview aggregate: %w", err)
	}
	if out.TotalMatches > 0 {
		out.WinRate = float64(out.Wins) / float64(out.TotalMatches)
	}

	recent, err := s.ListMatches(ctx, recentLimit, "", "")
	if err != nil {
		return out, err
	}
	out.Recent = recent
	return out, nil
}

func (s *Store) ListMatches(ctx context.Context, limit int64, eventName, result string) ([]model.MatchRow, error) {
	if limit <= 0 {
		limit = 200
	}
	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			%s,
			%s,
			COALESCE(m.opponent_name, ''),
			COALESCE(m.started_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(m.win_reason, ''),
			COALESCE(
				m.turn_count,
				(
					SELECT SUM(game_turns)
					FROM (
						SELECT MAX(cp.turn_number) AS game_turns
						FROM match_card_plays cp
						WHERE cp.match_id = m.id AND cp.turn_number IS NOT NULL
						GROUP BY cp.game_number
					)
				)
			),
			COALESCE(
				m.seconds_count,
				CASE
					WHEN m.started_at IS NOT NULL AND m.ended_at IS NOT NULL THEN
						CAST(ROUND((julianday(m.ended_at) - julianday(m.started_at)) * 86400.0) AS INTEGER)
					ELSE NULL
				END
			),
			d.id,
			d.name
		FROM matches m
		LEFT JOIN match_decks md ON md.match_id = m.id
		LEFT JOIN decks d ON d.id = md.deck_id
		WHERE (? = '' OR m.event_name = ?)
		  AND (? = '' OR m.result = ?)
		ORDER BY COALESCE(m.started_at, m.ended_at, m.updated_at) DESC
		LIMIT ?
	`, matchBestOfSQL, matchPlayDrawSQL)
	rows, err := s.db.QueryContext(ctx, query, eventName, eventName, result, result, limit)
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	defer rows.Close()

	resultRows := make([]model.MatchRow, 0, limit)
	for rows.Next() {
		var r model.MatchRow
		if err := rows.Scan(
			&r.ID,
			&r.ArenaMatchID,
			&r.EventName,
			&r.BestOf,
			&r.PlayDraw,
			&r.Opponent,
			&r.StartedAt,
			&r.EndedAt,
			&r.Result,
			&r.WinReason,
			&r.TurnCount,
			&r.SecondsCount,
			&r.DeckID,
			&r.DeckName,
		); err != nil {
			return nil, fmt.Errorf("scan match row: %w", err)
		}
		resultRows = append(resultRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matches: %w", err)
	}

	return resultRows, nil
}

func (s *Store) ListMatchDeckCardQuantities(ctx context.Context, matchIDs []int64) (map[int64]map[int64]int64, error) {
	out := make(map[int64]map[int64]int64)
	for _, batch := range int64Batches(matchIDs, sqliteInClauseBatchSize) {
		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, matchID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, matchID)
		}

		query := fmt.Sprintf(`
			SELECT md.match_id, dc.card_id, MAX(dc.quantity) AS quantity
			FROM match_decks md
			JOIN deck_cards dc ON dc.deck_id = md.deck_id
			WHERE dc.section = 'main'
			  AND md.match_id IN (%s)
			GROUP BY md.match_id, dc.card_id
		`, strings.Join(placeholders, ","))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("list match deck card quantities: %w", err)
		}

		for rows.Next() {
			var matchID int64
			var cardID int64
			var quantity int64
			if err := rows.Scan(&matchID, &cardID, &quantity); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan match deck card quantity: %w", err)
			}
			if _, ok := out[matchID]; !ok {
				out[matchID] = make(map[int64]int64)
			}
			out[matchID][cardID] = quantity
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate match deck card quantities: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

func (s *Store) ListMatchOpponentCardQuantities(ctx context.Context, matchIDs []int64) (map[int64]map[int64]int64, error) {
	out := make(map[int64]map[int64]int64)
	for _, batch := range int64Batches(matchIDs, sqliteInClauseBatchSize) {
		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, matchID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, matchID)
		}

		query := fmt.Sprintf(`
			WITH per_game AS (
				SELECT
					match_id,
					card_id,
					game_number,
					COUNT(*) AS quantity_in_game
				FROM match_opponent_card_instances
				WHERE match_id IN (%s)
				GROUP BY match_id, card_id, game_number
			)
			SELECT
				match_id,
				card_id,
				MAX(quantity_in_game) AS quantity
			FROM per_game
			GROUP BY match_id, card_id
		`, strings.Join(placeholders, ","))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("list opponent observed card quantities: %w", err)
		}

		for rows.Next() {
			var matchID int64
			var cardID int64
			var quantity int64
			if err := rows.Scan(&matchID, &cardID, &quantity); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan opponent observed card quantity: %w", err)
			}
			if _, ok := out[matchID]; !ok {
				out[matchID] = make(map[int64]int64)
			}
			out[matchID][cardID] = quantity
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate opponent observed card quantities: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

func (s *Store) GetMatchDetail(ctx context.Context, matchID int64) (model.MatchDetail, error) {
	var out model.MatchDetail

	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			%s,
			%s,
			COALESCE(m.opponent_name, ''),
			COALESCE(m.started_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(m.win_reason, ''),
			COALESCE(
				m.turn_count,
				(
					SELECT SUM(game_turns)
					FROM (
						SELECT MAX(cp.turn_number) AS game_turns
						FROM match_card_plays cp
						WHERE cp.match_id = m.id AND cp.turn_number IS NOT NULL
						GROUP BY cp.game_number
					)
				)
			),
			COALESCE(
				m.seconds_count,
				CASE
					WHEN m.started_at IS NOT NULL AND m.ended_at IS NOT NULL THEN
						CAST(ROUND((julianday(m.ended_at) - julianday(m.started_at)) * 86400.0) AS INTEGER)
					ELSE NULL
				END
			),
			d.id,
			d.name
		FROM matches m
		LEFT JOIN match_decks md ON md.match_id = m.id
		LEFT JOIN decks d ON d.id = md.deck_id
		WHERE m.id = ?
		LIMIT 1
	`, matchBestOfSQL, matchPlayDrawSQL)

	err := s.db.QueryRowContext(ctx, query, matchID).Scan(
		&out.Match.ID,
		&out.Match.ArenaMatchID,
		&out.Match.EventName,
		&out.Match.BestOf,
		&out.Match.PlayDraw,
		&out.Match.Opponent,
		&out.Match.StartedAt,
		&out.Match.EndedAt,
		&out.Match.Result,
		&out.Match.WinReason,
		&out.Match.TurnCount,
		&out.Match.SecondsCount,
		&out.Match.DeckID,
		&out.Match.DeckName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return out, sql.ErrNoRows
	}
	if err != nil {
		return out, fmt.Errorf("get match detail: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH per_game AS (
			SELECT
				oc.card_id,
				oc.game_number,
				COUNT(*) AS quantity_in_game
			FROM match_opponent_card_instances oc
			WHERE oc.match_id = ?
			GROUP BY oc.card_id, oc.game_number
		)
		SELECT
			pg.card_id,
			MAX(pg.quantity_in_game) AS quantity,
			COALESCE(cc.name, '')
		FROM per_game pg
		LEFT JOIN card_catalog cc ON cc.arena_id = pg.card_id
		GROUP BY pg.card_id, cc.name
		ORDER BY quantity DESC, cc.name ASC, pg.card_id ASC
	`, matchID)
	if err != nil {
		return out, fmt.Errorf("get observed opponent cards: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var card model.OpponentObservedCardRow
		if err := rows.Scan(&card.CardID, &card.Quantity, &card.CardName); err != nil {
			return out, fmt.Errorf("scan observed opponent card: %w", err)
		}
		out.OpponentObservedCards = append(out.OpponentObservedCards, card)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate observed opponent cards: %w", err)
	}

	out.CardPlays, err = s.ListMatchCardPlays(ctx, matchID)
	if err != nil {
		return out, err
	}

	return out, nil
}

func (s *Store) ListDecks(ctx context.Context) ([]model.DeckSummaryRow, error) {
	return s.ListDecksByScope(ctx, "constructed")
}

func (s *Store) ListDecksByScope(ctx context.Context, scope string) ([]model.DeckSummaryRow, error) {
	scope = normalizeDeckScope(scope)

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			d.id,
			COALESCE(d.name, d.arena_deck_id) AS deck_name,
			COALESCE(d.format, ''),
			COALESCE(d.event_name, ''),
			COUNT(m.id) AS matches,
			SUM(CASE WHEN m.result = 'win' THEN 1 ELSE 0 END) AS wins,
			SUM(CASE WHEN m.result = 'loss' THEN 1 ELSE 0 END) AS losses,
			COALESCE(MIN(COALESCE(m.started_at, m.ended_at)), '') AS first_played_at,
			COALESCE(d.last_updated, d.created_at, '') AS last_updated_at
		FROM decks d
		LEFT JOIN match_decks md ON md.deck_id = d.id
		LEFT JOIN matches m ON m.id = md.match_id
		GROUP BY d.id, d.name, d.arena_deck_id, d.format, d.event_name, d.last_updated, d.created_at
		ORDER BY matches DESC, deck_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()

	var out []model.DeckSummaryRow
	for rows.Next() {
		var r model.DeckSummaryRow
		if err := rows.Scan(
			&r.DeckID,
			&r.DeckName,
			&r.Format,
			&r.EventName,
			&r.Matches,
			&r.Wins,
			&r.Losses,
			&r.FirstPlayedAt,
			&r.LastUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan deck summary: %w", err)
		}
		if !deckInScope(scope, r.Format, r.EventName) {
			continue
		}
		if r.Matches > 0 {
			r.WinRate = float64(r.Wins) / float64(r.Matches)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decks: %w", err)
	}
	return out, nil
}

func (s *Store) GetDeckDetail(ctx context.Context, deckID int64, matchLimit int64) (model.DeckDetail, error) {
	var out model.DeckDetail
	if matchLimit <= 0 {
		matchLimit = 50
	}

	err := s.db.QueryRowContext(ctx, `
		SELECT id, arena_deck_id, COALESCE(name, ''), COALESCE(format, ''), COALESCE(event_name, '')
		FROM decks
		WHERE id = ?
	`, deckID).Scan(&out.DeckID, &out.ArenaDeckID, &out.Name, &out.Format, &out.EventName)
	if err != nil {
		return out, fmt.Errorf("get deck: %w", err)
	}

	cardRows, err := s.db.QueryContext(ctx, `
		SELECT dc.section, dc.card_id, dc.quantity, COALESCE(cc.name, '')
		FROM deck_cards dc
		LEFT JOIN card_catalog cc ON cc.arena_id = dc.card_id
		WHERE deck_id = ?
		ORDER BY dc.section, dc.card_id
	`, deckID)
	if err != nil {
		return out, fmt.Errorf("get deck cards: %w", err)
	}
	defer cardRows.Close()

	for cardRows.Next() {
		var c model.DeckCardRow
		if err := cardRows.Scan(&c.Section, &c.CardID, &c.Quantity, &c.CardName); err != nil {
			return out, fmt.Errorf("scan deck card: %w", err)
		}
		out.Cards = append(out.Cards, c)
	}
	if err := cardRows.Err(); err != nil {
		return out, fmt.Errorf("iterate deck cards: %w", err)
	}

	matchRows, err := s.db.QueryContext(ctx, `
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			COALESCE(m.opponent_name, ''),
			COALESCE(m.started_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(m.win_reason, ''),
			COALESCE(
				m.turn_count,
				(
					SELECT SUM(game_turns)
					FROM (
						SELECT MAX(cp.turn_number) AS game_turns
						FROM match_card_plays cp
						WHERE cp.match_id = m.id AND cp.turn_number IS NOT NULL
						GROUP BY cp.game_number
					)
				)
			),
			COALESCE(
				m.seconds_count,
				CASE
					WHEN m.started_at IS NOT NULL AND m.ended_at IS NOT NULL THEN
						CAST(ROUND((julianday(m.ended_at) - julianday(m.started_at)) * 86400.0) AS INTEGER)
					ELSE NULL
				END
			)
		FROM matches m
		JOIN match_decks md ON md.match_id = m.id
		WHERE md.deck_id = ?
		ORDER BY COALESCE(m.started_at, m.ended_at, m.updated_at) DESC
		LIMIT ?
	`, deckID, matchLimit)
	if err != nil {
		return out, fmt.Errorf("get deck matches: %w", err)
	}
	defer matchRows.Close()

	for matchRows.Next() {
		var m model.MatchRow
		if err := matchRows.Scan(
			&m.ID,
			&m.ArenaMatchID,
			&m.EventName,
			&m.Opponent,
			&m.StartedAt,
			&m.EndedAt,
			&m.Result,
			&m.WinReason,
			&m.TurnCount,
			&m.SecondsCount,
		); err != nil {
			return out, fmt.Errorf("scan deck match row: %w", err)
		}
		out.Matches = append(out.Matches, m)
	}
	if err := matchRows.Err(); err != nil {
		return out, fmt.Errorf("iterate deck matches: %w", err)
	}

	return out, nil
}

func (s *Store) LookupCardNames(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return names, nil
	}

	placeholders := make([]string, 0, len(cardIDs))
	args := make([]any, 0, len(cardIDs))
	for _, id := range cardIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT arena_id, name
		FROM card_catalog
		WHERE arena_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup card names: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan card name: %w", err)
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate card names: %w", err)
	}

	return names, nil
}

func (s *Store) UpsertCardNames(ctx context.Context, names map[int64]string) error {
	if len(names) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin card catalog tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO card_catalog (arena_id, name, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(arena_id) DO UPDATE SET
			name = excluded.name,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare card catalog upsert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for id, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id, name, now); err != nil {
			return fmt.Errorf("upsert card catalog row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card catalog tx: %w", err)
	}
	return nil
}

func (s *Store) ListDraftSessions(ctx context.Context) ([]model.DraftSessionRow, error) {
	if err := s.RepairDraftDataFromRawEvents(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ds.id,
			COALESCE(ds.event_name, ''),
			ds.draft_id,
			ds.is_bot_draft,
			COALESCE(ds.started_at, ''),
			COALESCE(ds.completed_at, ''),
			COUNT(dp.id) AS picks
		FROM draft_sessions ds
		LEFT JOIN draft_picks dp ON dp.draft_session_id = ds.id
		GROUP BY ds.id, ds.event_name, ds.draft_id, ds.is_bot_draft, ds.started_at, ds.completed_at
		ORDER BY ds.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list draft sessions: %w", err)
	}
	defer rows.Close()

	var out []model.DraftSessionRow
	for rows.Next() {
		var row model.DraftSessionRow
		var isBotInt int64
		if err := rows.Scan(&row.ID, &row.EventName, &row.DraftID, &isBotInt, &row.StartedAt, &row.CompletedAt, &row.Picks); err != nil {
			return nil, fmt.Errorf("scan draft session row: %w", err)
		}
		row.IsBotDraft = isBotInt == 1
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft sessions: %w", err)
	}

	if err := s.enrichDraftSessionsWithDeckResults(ctx, out); err != nil {
		return nil, err
	}

	return out, nil
}

type draftDeckCandidate struct {
	DeckTS        time.Time
	FirstPlayedAt time.Time
	LastPlayedAt  time.Time
	Wins          int64
	Losses        int64
}

func (s *Store) enrichDraftSessionsWithDeckResults(ctx context.Context, sessions []model.DraftSessionRow) error {
	for idx := range sessions {
		wins, losses, ok, err := s.resolveDraftSessionDeckResults(ctx, sessions[idx].EventName, sessions[idx].StartedAt, sessions[idx].CompletedAt)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		sessions[idx].Wins = nullableInt64Ptr(wins)
		sessions[idx].Losses = nullableInt64Ptr(losses)
	}
	return nil
}

func nullableInt64Ptr(value int64) *int64 {
	out := value
	return &out
}

func parseStoredTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func chooseDraftDeckCandidate(candidates []draftDeckCandidate, startedAt, completedAt string) (draftDeckCandidate, bool) {
	if len(candidates) == 0 {
		return draftDeckCandidate{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}

	anchor, ok := parseStoredTime(completedAt)
	if !ok {
		anchor, ok = parseStoredTime(startedAt)
	}
	if !ok {
		return draftDeckCandidate{}, false
	}

	bestIdx := -1
	bestPriority := 99
	var bestDiff time.Duration
	for idx, candidate := range candidates {
		reference := candidate.DeckTS
		if reference.IsZero() && !candidate.FirstPlayedAt.IsZero() {
			reference = candidate.FirstPlayedAt
		}
		if reference.IsZero() {
			continue
		}

		priority := 2
		if !candidate.DeckTS.IsZero() && !candidate.DeckTS.Before(anchor) {
			priority = 0
		} else if !candidate.FirstPlayedAt.IsZero() && !candidate.FirstPlayedAt.Before(anchor) {
			priority = 1
		}

		diff := reference.Sub(anchor)
		if diff < 0 {
			diff = -diff
		}

		if bestIdx == -1 || priority < bestPriority || (priority == bestPriority && diff < bestDiff) {
			bestIdx = idx
			bestPriority = priority
			bestDiff = diff
		}
	}

	if bestIdx == -1 {
		return draftDeckCandidate{}, false
	}

	return candidates[bestIdx], true
}

func (s *Store) resolveDraftSessionDeckResults(ctx context.Context, eventName, startedAt, completedAt string) (int64, int64, bool, error) {
	eventName = strings.TrimSpace(eventName)
	if eventName == "" {
		return 0, 0, false, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(d.last_updated, d.created_at, ''),
			COALESCE(MIN(COALESCE(m.started_at, m.ended_at)), ''),
			COALESCE(MAX(COALESCE(m.started_at, m.ended_at)), ''),
			COALESCE(SUM(CASE WHEN m.result = 'win' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN m.result = 'loss' THEN 1 ELSE 0 END), 0)
		FROM decks d
		LEFT JOIN match_decks md ON md.deck_id = d.id
		LEFT JOIN matches m ON m.id = md.match_id
		WHERE d.event_name = ?
		  AND (
			LOWER(COALESCE(d.format, '')) = 'draft'
			OR LOWER(COALESCE(d.event_name, '')) LIKE '%draft%'
		  )
		GROUP BY d.id, d.last_updated, d.created_at
	`, eventName)
	if err != nil {
		return 0, 0, false, fmt.Errorf("resolve draft session deck results: %w", err)
	}
	defer rows.Close()

	var candidates []draftDeckCandidate
	for rows.Next() {
		var deckTSRaw, firstPlayedRaw, lastPlayedRaw string
		var candidate draftDeckCandidate
		if err := rows.Scan(&deckTSRaw, &firstPlayedRaw, &lastPlayedRaw, &candidate.Wins, &candidate.Losses); err != nil {
			return 0, 0, false, fmt.Errorf("scan draft deck candidate: %w", err)
		}
		if parsed, ok := parseStoredTime(deckTSRaw); ok {
			candidate.DeckTS = parsed
		}
		if parsed, ok := parseStoredTime(firstPlayedRaw); ok {
			candidate.FirstPlayedAt = parsed
		}
		if parsed, ok := parseStoredTime(lastPlayedRaw); ok {
			candidate.LastPlayedAt = parsed
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, false, fmt.Errorf("iterate draft deck candidates: %w", err)
	}

	candidate, ok := chooseDraftDeckCandidate(candidates, startedAt, completedAt)
	if !ok {
		return 0, 0, false, nil
	}
	return candidate.Wins, candidate.Losses, true, nil
}

func (s *Store) ListDraftPicks(ctx context.Context, draftSessionID int64) ([]model.DraftPickRow, error) {
	if err := s.RepairDraftDataFromRawEvents(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pack_number, pick_number, picked_card_ids, COALESCE(pack_card_ids, '[]'), COALESCE(pick_ts, '')
		FROM draft_picks
		WHERE draft_session_id = ?
		ORDER BY pack_number, pick_number
	`, draftSessionID)
	if err != nil {
		return nil, fmt.Errorf("list draft picks: %w", err)
	}
	defer rows.Close()

	var out []model.DraftPickRow
	for rows.Next() {
		var r model.DraftPickRow
		if err := rows.Scan(&r.ID, &r.PackNumber, &r.PickNumber, &r.PickedCardIDs, &r.PackCardIDs, &r.PickTs); err != nil {
			return nil, fmt.Errorf("scan draft pick row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft picks: %w", err)
	}

	return out, nil
}

func (s *Store) ListMatchCardPlays(ctx context.Context, matchID int64) ([]model.MatchCardPlayRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			cp.id,
			cp.game_number,
			cp.instance_id,
			cp.card_id,
			COALESCE(cc.name, ''),
			cp.owner_seat_id,
			CASE
				WHEN m.player_seat_id IS NOT NULL AND cp.owner_seat_id = m.player_seat_id THEN 'self'
				WHEN cp.owner_seat_id IS NOT NULL AND cp.owner_seat_id > 0 THEN 'opponent'
				ELSE 'unknown'
			END AS player_side,
			COALESCE(cp.first_public_zone, ''),
			cp.turn_number,
			COALESCE(cp.phase, ''),
			COALESCE(cp.played_at, '')
		FROM match_card_plays cp
		JOIN matches m ON m.id = cp.match_id
		LEFT JOIN card_catalog cc ON cc.arena_id = cp.card_id
		WHERE cp.match_id = ?
		ORDER BY cp.game_number ASC, COALESCE(cp.turn_number, 1000000) ASC, COALESCE(cp.played_at, '') ASC, cp.id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match card plays: %w", err)
	}
	defer rows.Close()

	out := make([]model.MatchCardPlayRow, 0)
	for rows.Next() {
		var row model.MatchCardPlayRow
		var gameNo sql.NullInt64
		var ownerSeat sql.NullInt64
		var turnNo sql.NullInt64
		if err := rows.Scan(
			&row.ID,
			&gameNo,
			&row.InstanceID,
			&row.CardID,
			&row.CardName,
			&ownerSeat,
			&row.PlayerSide,
			&row.FirstPublicZone,
			&turnNo,
			&row.Phase,
			&row.PlayedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match card play row: %w", err)
		}
		if gameNo.Valid {
			v := gameNo.Int64
			row.GameNumber = &v
		}
		if ownerSeat.Valid {
			v := ownerSeat.Int64
			row.OwnerSeatID = &v
		}
		if turnNo.Valid {
			v := turnNo.Int64
			row.TurnNumber = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match card plays: %w", err)
	}

	return out, nil
}

func (s *Store) ListRankHistory(ctx context.Context) ([]model.RankHistoryPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			COALESCE(m.opponent_name, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(mrs.observed_at, ''),
			COALESCE(m.ended_at, ''),
			mrs.constructed_season_ordinal,
			COALESCE(mrs.constructed_rank_class, ''),
			mrs.constructed_level,
			mrs.constructed_step,
			mrs.constructed_matches_won,
			mrs.constructed_matches_lost,
			mrs.limited_season_ordinal,
			COALESCE(mrs.limited_rank_class, ''),
			mrs.limited_level,
			mrs.limited_step,
			mrs.limited_matches_won,
			mrs.limited_matches_lost
		FROM match_rank_snapshots mrs
		JOIN matches m ON m.id = mrs.match_id
		ORDER BY COALESCE(mrs.observed_at, m.ended_at, m.started_at, m.updated_at) ASC, mrs.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list rank history: %w", err)
	}
	defer rows.Close()

	var out []model.RankHistoryPoint
	for rows.Next() {
		var row model.RankHistoryPoint
		var constructedSeasonOrdinal sql.NullInt64
		var constructedLevel sql.NullInt64
		var constructedStep sql.NullInt64
		var constructedMatchesWon sql.NullInt64
		var constructedMatchesLost sql.NullInt64
		var limitedSeasonOrdinal sql.NullInt64
		var limitedLevel sql.NullInt64
		var limitedStep sql.NullInt64
		var limitedMatchesWon sql.NullInt64
		var limitedMatchesLost sql.NullInt64

		if err := rows.Scan(
			&row.MatchID,
			&row.ArenaMatchID,
			&row.EventName,
			&row.Opponent,
			&row.Result,
			&row.ObservedAt,
			&row.EndedAt,
			&constructedSeasonOrdinal,
			&row.Constructed.RankClass,
			&constructedLevel,
			&constructedStep,
			&constructedMatchesWon,
			&constructedMatchesLost,
			&limitedSeasonOrdinal,
			&row.Limited.RankClass,
			&limitedLevel,
			&limitedStep,
			&limitedMatchesWon,
			&limitedMatchesLost,
		); err != nil {
			return nil, fmt.Errorf("scan rank history row: %w", err)
		}

		row.Constructed.SeasonOrdinal = nullInt64Ptr(constructedSeasonOrdinal)
		row.Constructed.Level = nullInt64Ptr(constructedLevel)
		row.Constructed.Step = nullInt64Ptr(constructedStep)
		row.Constructed.MatchesWon = nullInt64Ptr(constructedMatchesWon)
		row.Constructed.MatchesLost = nullInt64Ptr(constructedMatchesLost)

		row.Limited.SeasonOrdinal = nullInt64Ptr(limitedSeasonOrdinal)
		row.Limited.Level = nullInt64Ptr(limitedLevel)
		row.Limited.Step = nullInt64Ptr(limitedStep)
		row.Limited.MatchesWon = nullInt64Ptr(limitedMatchesWon)
		row.Limited.MatchesLost = nullInt64Ptr(limitedMatchesLost)

		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rank history: %w", err)
	}

	return out, nil
}

func (s *Store) ReplaceMatchReplayFrame(
	ctx context.Context,
	tx *sql.Tx,
	arenaMatchID string,
	gameNumber, gameStateID, prevGameStateID, turnNumber int64,
	gameStateType, gameStage, phase, winningPlayerSide, winReason, recordedAt, source string,
	playerLifeTotalsJSON, actionsJSON, annotationsJSON []byte,
	objects []model.MatchReplayFrameObjectRow,
) (int64, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" || gameStateID <= 0 {
		return 0, nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	actionsText := strings.TrimSpace(string(actionsJSON))
	annotationsText := strings.TrimSpace(string(annotationsJSON))
	playerLifeTotalsText := strings.TrimSpace(string(playerLifeTotalsJSON))

	_, err := tx.ExecContext(ctx, `
		INSERT INTO match_replay_frames (
			match_id,
			game_number,
			game_state_id,
			prev_game_state_id,
			game_state_type,
			game_stage,
			turn_number,
			phase,
			player_life_totals_json,
			winning_player_side,
			win_reason,
			source,
			recorded_at,
			actions_json,
			annotations_json,
			created_at
		)
		SELECT
			m.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM matches m
		WHERE m.arena_match_id = ?
		ON CONFLICT(match_id, game_number, game_state_id) DO UPDATE SET
			prev_game_state_id = COALESCE(excluded.prev_game_state_id, match_replay_frames.prev_game_state_id),
			game_state_type = COALESCE(excluded.game_state_type, match_replay_frames.game_state_type),
			game_stage = COALESCE(excluded.game_stage, match_replay_frames.game_stage),
			turn_number = COALESCE(excluded.turn_number, match_replay_frames.turn_number),
			phase = COALESCE(excluded.phase, match_replay_frames.phase),
			player_life_totals_json = COALESCE(excluded.player_life_totals_json, match_replay_frames.player_life_totals_json),
			winning_player_side = COALESCE(excluded.winning_player_side, match_replay_frames.winning_player_side),
			win_reason = COALESCE(excluded.win_reason, match_replay_frames.win_reason),
			source = COALESCE(excluded.source, match_replay_frames.source),
			recorded_at = COALESCE(excluded.recorded_at, match_replay_frames.recorded_at),
			actions_json = COALESCE(excluded.actions_json, match_replay_frames.actions_json),
			annotations_json = COALESCE(excluded.annotations_json, match_replay_frames.annotations_json)
	`,
		gameNumber,
		gameStateID,
		nullableInt(prevGameStateID),
		nullIfEmpty(strings.TrimSpace(gameStateType)),
		nullIfEmpty(strings.TrimSpace(gameStage)),
		nullableInt(turnNumber),
		nullIfEmpty(strings.TrimSpace(phase)),
		nullIfEmpty(playerLifeTotalsText),
		nullIfEmpty(strings.TrimSpace(winningPlayerSide)),
		nullIfEmpty(strings.TrimSpace(winReason)),
		nullIfEmpty(strings.TrimSpace(source)),
		nullIfEmpty(normalizeTS(recordedAt)),
		nullIfEmpty(actionsText),
		nullIfEmpty(annotationsText),
		nowUTC(),
		arenaMatchID,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert match replay frame: %w", err)
	}

	var frameID int64
	err = tx.QueryRowContext(ctx, `
		SELECT f.id
		FROM match_replay_frames f
		JOIN matches m ON m.id = f.match_id
		WHERE m.arena_match_id = ?
			AND f.game_number = ?
			AND f.game_state_id = ?
		LIMIT 1
	`, arenaMatchID, gameNumber, gameStateID).Scan(&frameID)
	if err != nil {
		return 0, fmt.Errorf("lookup match replay frame: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM match_replay_frame_objects WHERE frame_id = ?`, frameID); err != nil {
		return 0, fmt.Errorf("clear match replay frame objects: %w", err)
	}

	for _, obj := range objects {
		if obj.InstanceID <= 0 || obj.CardID <= 0 || strings.TrimSpace(obj.ZoneType) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO match_replay_frame_objects (
				frame_id,
				instance_id,
				card_id,
				owner_seat_id,
				controller_seat_id,
				zone_id,
				zone_type,
				zone_position,
				visibility,
				power,
				toughness,
				is_tapped,
				has_summoning_sickness,
				attack_state,
				attack_target_id,
				block_state,
				block_attacker_ids_json,
				counter_summary_json,
				details_json,
				is_token,
				created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			frameID,
			obj.InstanceID,
			obj.CardID,
			nullableReplayInt(obj.OwnerSeatID),
			nullableReplayInt(obj.ControllerSeatID),
			nullableReplayInt(obj.ZoneID),
			strings.TrimSpace(obj.ZoneType),
			nullableReplayInt(obj.ZonePosition),
			nullIfEmpty(strings.TrimSpace(obj.Visibility)),
			nullableReplayInt(obj.Power),
			nullableReplayInt(obj.Toughness),
			boolToInt(obj.IsTapped),
			boolToInt(obj.HasSummoningSickness),
			nullIfEmpty(strings.TrimSpace(obj.AttackState)),
			nullableReplayInt(obj.AttackTargetID),
			nullIfEmpty(strings.TrimSpace(obj.BlockState)),
			nullIfEmpty(strings.TrimSpace(obj.BlockAttackerIDsJSON)),
			nullIfEmpty(strings.TrimSpace(obj.CounterSummaryJSON)),
			nullIfEmpty(strings.TrimSpace(obj.DetailsJSON)),
			boolToInt(obj.IsToken),
			nowUTC(),
		); err != nil {
			return 0, fmt.Errorf("insert match replay frame object: %w", err)
		}
	}

	return frameID, nil
}

func (s *Store) LoadLatestMatchReplayFrameState(
	ctx context.Context,
	tx *sql.Tx,
	arenaMatchID string,
	gameNumber int64,
) (int64, int64, []model.MatchReplayFrameObjectRow, map[int64]int64, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return 0, 0, nil, nil, nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	var (
		frameID             int64
		gameStateID         sql.NullInt64
		turnNumber          sql.NullInt64
		playerLifeTotalsRaw sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT f.id, f.game_state_id, f.turn_number, f.player_life_totals_json
		FROM match_replay_frames f
		JOIN matches m ON m.id = f.match_id
		WHERE m.arena_match_id = ?
			AND f.game_number = ?
		ORDER BY COALESCE(f.game_state_id, 0) DESC, f.id DESC
		LIMIT 1
	`, arenaMatchID, gameNumber).Scan(&frameID, &gameStateID, &turnNumber, &playerLifeTotalsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil, nil, nil
	}
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("lookup latest match replay frame: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			o.id,
			o.frame_id,
			o.instance_id,
			o.card_id,
			o.owner_seat_id,
			o.controller_seat_id,
			o.zone_id,
			COALESCE(o.zone_type, ''),
			o.zone_position,
			COALESCE(o.visibility, ''),
			o.power,
			o.toughness,
			o.is_tapped,
			o.has_summoning_sickness,
			COALESCE(o.attack_state, ''),
			o.attack_target_id,
			COALESCE(o.block_state, ''),
			COALESCE(o.block_attacker_ids_json, ''),
			COALESCE(o.counter_summary_json, ''),
			COALESCE(o.details_json, ''),
			o.is_token
		FROM match_replay_frame_objects o
		WHERE o.frame_id = ?
		ORDER BY COALESCE(o.zone_id, 0) ASC, COALESCE(o.zone_position, 1000000) ASC, o.instance_id ASC
	`, frameID)
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("load latest match replay frame objects: %w", err)
	}
	defer rows.Close()

	objects := make([]model.MatchReplayFrameObjectRow, 0)
	for rows.Next() {
		var (
			row                  model.MatchReplayFrameObjectRow
			ownerSeatID          sql.NullInt64
			controllerSeatID     sql.NullInt64
			zoneID               sql.NullInt64
			zonePosition         sql.NullInt64
			power                sql.NullInt64
			toughness            sql.NullInt64
			attackTargetID       sql.NullInt64
			isTapped             int64
			hasSummoningSickness int64
			isToken              int64
		)
		if err := rows.Scan(
			&row.ID,
			&row.FrameID,
			&row.InstanceID,
			&row.CardID,
			&ownerSeatID,
			&controllerSeatID,
			&zoneID,
			&row.ZoneType,
			&zonePosition,
			&row.Visibility,
			&power,
			&toughness,
			&isTapped,
			&hasSummoningSickness,
			&row.AttackState,
			&attackTargetID,
			&row.BlockState,
			&row.BlockAttackerIDsJSON,
			&row.CounterSummaryJSON,
			&row.DetailsJSON,
			&isToken,
		); err != nil {
			return 0, 0, nil, nil, fmt.Errorf("scan latest match replay frame object: %w", err)
		}
		row.OwnerSeatID = replayPtrFromNullInt64(ownerSeatID)
		row.ControllerSeatID = replayPtrFromNullInt64(controllerSeatID)
		row.ZoneID = replayPtrFromNullInt64(zoneID)
		row.ZonePosition = replayPtrFromNullInt64(zonePosition)
		row.Power = replayPtrFromNullInt64(power)
		row.Toughness = replayPtrFromNullInt64(toughness)
		row.AttackTargetID = replayPtrFromNullInt64(attackTargetID)
		row.IsTapped = isTapped > 0
		row.HasSummoningSickness = hasSummoningSickness > 0
		row.IsToken = isToken > 0
		objects = append(objects, row)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, nil, nil, fmt.Errorf("iterate latest match replay frame objects: %w", err)
	}

	return gameStateID.Int64, turnNumber.Int64, objects, parseReplayPlayerLifeTotalsJSON(playerLifeTotalsRaw.String), nil
}

func (s *Store) ListMatchReplayFrames(ctx context.Context, matchID int64) ([]model.MatchReplayFrameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			f.id,
			f.game_number,
			f.game_state_id,
			f.prev_game_state_id,
			COALESCE(f.game_state_type, ''),
			COALESCE(f.game_stage, ''),
			f.turn_number,
			COALESCE(f.phase, ''),
			COALESCE(f.player_life_totals_json, ''),
			COALESCE(f.winning_player_side, ''),
			COALESCE(f.win_reason, ''),
			m.player_seat_id,
			COALESCE(f.recorded_at, ''),
			COALESCE(f.actions_json, ''),
			COALESCE(f.annotations_json, '')
		FROM match_replay_frames f
		JOIN matches m ON m.id = f.match_id
		WHERE f.match_id = ?
		ORDER BY f.game_number ASC, COALESCE(f.game_state_id, 0) ASC, f.id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match replay frames: %w", err)
	}
	defer rows.Close()

	frames := make([]model.MatchReplayFrameRow, 0)
	frameIndexByID := make(map[int64]int)
	for rows.Next() {
		var (
			row             model.MatchReplayFrameRow
			gameNumber      sql.NullInt64
			gameStateID     sql.NullInt64
			prevGameStateID sql.NullInt64
			turnNumber      sql.NullInt64
			playerSeatID    sql.NullInt64
			playerLifeRaw   string
		)
		if err := rows.Scan(
			&row.ID,
			&gameNumber,
			&gameStateID,
			&prevGameStateID,
			&row.GameStateType,
			&row.GameStage,
			&turnNumber,
			&row.Phase,
			&playerLifeRaw,
			&row.WinningPlayerSide,
			&row.WinReason,
			&playerSeatID,
			&row.RecordedAt,
			&row.ActionsJSON,
			&row.AnnotationsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan match replay frame: %w", err)
		}
		row.GameNumber = replayPtrFromNullInt64(gameNumber)
		row.GameStateID = replayPtrFromNullInt64(gameStateID)
		row.PrevGameStateID = replayPtrFromNullInt64(prevGameStateID)
		row.TurnNumber = replayPtrFromNullInt64(turnNumber)
		row.SelfLifeTotal, row.OpponentLifeTotal = replayFrameLifeTotals(
			parseReplayPlayerLifeTotalsJSON(playerLifeRaw),
			replayPtrFromNullInt64(playerSeatID),
		)
		frameIndexByID[row.ID] = len(frames)
		frames = append(frames, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match replay frames: %w", err)
	}
	if len(frames) == 0 {
		return frames, nil
	}

	objectRows, err := s.db.QueryContext(ctx, `
		SELECT
			o.id,
			o.frame_id,
			o.instance_id,
			o.card_id,
			COALESCE(cc.name, ''),
			o.owner_seat_id,
			o.controller_seat_id,
			CASE
				WHEN m.player_seat_id IS NOT NULL AND COALESCE(o.controller_seat_id, o.owner_seat_id) = m.player_seat_id THEN 'self'
				WHEN COALESCE(o.controller_seat_id, o.owner_seat_id) IS NOT NULL AND COALESCE(o.controller_seat_id, o.owner_seat_id) > 0 THEN 'opponent'
				ELSE 'unknown'
			END AS player_side,
			o.zone_id,
			COALESCE(o.zone_type, ''),
			o.zone_position,
			COALESCE(o.visibility, ''),
			o.power,
			o.toughness,
			o.is_tapped,
			o.has_summoning_sickness,
			COALESCE(o.attack_state, ''),
			o.attack_target_id,
			COALESCE(o.block_state, ''),
			COALESCE(o.block_attacker_ids_json, ''),
			COALESCE(o.counter_summary_json, ''),
			COALESCE(o.details_json, ''),
			o.is_token
		FROM match_replay_frame_objects o
		JOIN match_replay_frames f ON f.id = o.frame_id
		JOIN matches m ON m.id = f.match_id
		LEFT JOIN card_catalog cc ON cc.arena_id = o.card_id
		WHERE f.match_id = ?
		ORDER BY
			f.game_number ASC,
			COALESCE(f.game_state_id, 0) ASC,
			f.id ASC,
			COALESCE(o.zone_id, 0) ASC,
			COALESCE(o.zone_position, 1000000) ASC,
			o.instance_id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match replay frame objects: %w", err)
	}
	defer objectRows.Close()

	for objectRows.Next() {
		var (
			row                  model.MatchReplayFrameObjectRow
			ownerSeatID          sql.NullInt64
			controllerSeatID     sql.NullInt64
			zoneID               sql.NullInt64
			zonePosition         sql.NullInt64
			power                sql.NullInt64
			toughness            sql.NullInt64
			attackTargetID       sql.NullInt64
			isTapped             int64
			hasSummoningSickness int64
			isToken              int64
		)
		if err := objectRows.Scan(
			&row.ID,
			&row.FrameID,
			&row.InstanceID,
			&row.CardID,
			&row.CardName,
			&ownerSeatID,
			&controllerSeatID,
			&row.PlayerSide,
			&zoneID,
			&row.ZoneType,
			&zonePosition,
			&row.Visibility,
			&power,
			&toughness,
			&isTapped,
			&hasSummoningSickness,
			&row.AttackState,
			&attackTargetID,
			&row.BlockState,
			&row.BlockAttackerIDsJSON,
			&row.CounterSummaryJSON,
			&row.DetailsJSON,
			&isToken,
		); err != nil {
			return nil, fmt.Errorf("scan match replay frame object: %w", err)
		}
		row.OwnerSeatID = replayPtrFromNullInt64(ownerSeatID)
		row.ControllerSeatID = replayPtrFromNullInt64(controllerSeatID)
		row.ZoneID = replayPtrFromNullInt64(zoneID)
		row.ZonePosition = replayPtrFromNullInt64(zonePosition)
		row.Power = replayPtrFromNullInt64(power)
		row.Toughness = replayPtrFromNullInt64(toughness)
		row.AttackTargetID = replayPtrFromNullInt64(attackTargetID)
		row.IsTapped = isTapped > 0
		row.HasSummoningSickness = hasSummoningSickness > 0
		row.IsToken = isToken > 0

		index, ok := frameIndexByID[row.FrameID]
		if !ok {
			continue
		}
		frames[index].Objects = append(frames[index].Objects, row)
	}
	if err := objectRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match replay frame objects: %w", err)
	}

	populateReplayFrameChanges(frames)
	return frames, nil
}

func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func nullableReplayInt(v *int64) any {
	if v == nil {
		return nil
	}
	return nullableInt(*v)
}

func replayPtrFromNullInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func parseReplayPlayerLifeTotalsJSON(raw string) map[int64]int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed := make(map[string]int64)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}

	out := make(map[int64]int64, len(parsed))
	for key, lifeTotal := range parsed {
		seatID, err := strconv.ParseInt(strings.TrimSpace(key), 10, 64)
		if err != nil || seatID <= 0 {
			continue
		}
		out[seatID] = lifeTotal
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func replayFrameLifeTotals(lifeTotals map[int64]int64, selfSeatID *int64) (*int64, *int64) {
	if len(lifeTotals) == 0 {
		return nil, nil
	}

	var selfLifeTotal *int64
	if selfSeatID != nil && *selfSeatID > 0 {
		if value, ok := lifeTotals[*selfSeatID]; ok {
			out := value
			selfLifeTotal = &out
		}
	}

	var opponentLifeTotal *int64
	seatIDs := make([]int, 0, len(lifeTotals))
	for seatID := range lifeTotals {
		seatIDs = append(seatIDs, int(seatID))
	}
	sort.Ints(seatIDs)
	for _, rawSeatID := range seatIDs {
		seatID := int64(rawSeatID)
		if selfSeatID != nil && seatID == *selfSeatID {
			continue
		}
		value := lifeTotals[seatID]
		out := value
		opponentLifeTotal = &out
		break
	}

	return selfLifeTotal, opponentLifeTotal
}

func populateReplayFrameChanges(frames []model.MatchReplayFrameRow) {
	if len(frames) == 0 {
		return
	}

	var (
		prevGameNumber int64
		prevObjects    map[int64]model.MatchReplayFrameObjectRow
		havePrev       bool
	)

	for i := range frames {
		gameNumber := int64(1)
		if frames[i].GameNumber != nil && *frames[i].GameNumber > 0 {
			gameNumber = *frames[i].GameNumber
		}
		if !havePrev || gameNumber != prevGameNumber {
			prevObjects = nil
			havePrev = true
			prevGameNumber = gameNumber
		}

		currentObjects := make(map[int64]model.MatchReplayFrameObjectRow, len(frames[i].Objects))
		for _, obj := range frames[i].Objects {
			currentObjects[obj.InstanceID] = obj
		}

		if prevObjects != nil {
			for _, obj := range frames[i].Objects {
				prev, ok := prevObjects[obj.InstanceID]
				if !ok {
					frames[i].Changes = append(frames[i].Changes, model.MatchReplayChangeRow{
						InstanceID:     obj.InstanceID,
						CardID:         obj.CardID,
						CardName:       obj.CardName,
						OwnerSeatID:    obj.OwnerSeatID,
						PlayerSide:     obj.PlayerSide,
						Action:         "enter_public",
						ToZoneID:       obj.ZoneID,
						ToZoneType:     obj.ZoneType,
						ToZonePosition: obj.ZonePosition,
						IsToken:        obj.IsToken,
					})
					continue
				}
				if !sameReplayZone(prev, obj) {
					frames[i].Changes = append(frames[i].Changes, model.MatchReplayChangeRow{
						InstanceID:       obj.InstanceID,
						CardID:           obj.CardID,
						CardName:         obj.CardName,
						OwnerSeatID:      obj.OwnerSeatID,
						PlayerSide:       obj.PlayerSide,
						Action:           "move_public",
						FromZoneID:       prev.ZoneID,
						FromZoneType:     prev.ZoneType,
						FromZonePosition: prev.ZonePosition,
						ToZoneID:         obj.ZoneID,
						ToZoneType:       obj.ZoneType,
						ToZonePosition:   obj.ZonePosition,
						IsToken:          obj.IsToken,
					})
				}
				if !sameReplayIntPtr(prev.ControllerSeatID, obj.ControllerSeatID) {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "controller_change"))
				}
				if prev.IsTapped != obj.IsTapped {
					action := "untap"
					if obj.IsTapped {
						action = "tap"
					}
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, action))
				}
				if !sameReplayCombatState(prev, obj) {
					action := "combat_change"
					switch {
					case !replayHasCombatRole(prev) && replayHasCombatRole(obj) && replayIsBlocking(obj):
						action = "block"
					case !replayHasCombatRole(prev) && replayHasCombatRole(obj):
						action = "attack"
					case replayHasCombatRole(prev) && !replayHasCombatRole(obj) && replayIsBlocking(prev):
						action = "stop_block"
					case replayHasCombatRole(prev) && !replayHasCombatRole(obj):
						action = "stop_attack"
					case replayIsBlocking(obj):
						action = "block"
					default:
						action = "attack"
					}
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, action))
				}
				if prev.HasSummoningSickness != obj.HasSummoningSickness {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "summoning_sickness_change"))
				}
				if !sameReplayIntPtr(prev.Power, obj.Power) || !sameReplayIntPtr(prev.Toughness, obj.Toughness) {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "stat_change"))
				}
				if !sameReplayText(prev.CounterSummaryJSON, obj.CounterSummaryJSON) {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "counters_change"))
				}
			}
			for _, obj := range prevObjects {
				if _, ok := currentObjects[obj.InstanceID]; ok {
					continue
				}
				frames[i].Changes = append(frames[i].Changes, model.MatchReplayChangeRow{
					InstanceID:       obj.InstanceID,
					CardID:           obj.CardID,
					CardName:         obj.CardName,
					OwnerSeatID:      obj.OwnerSeatID,
					PlayerSide:       obj.PlayerSide,
					Action:           "leave_public",
					FromZoneID:       obj.ZoneID,
					FromZoneType:     obj.ZoneType,
					FromZonePosition: obj.ZonePosition,
					IsToken:          obj.IsToken,
				})
			}
		}

		prevObjects = currentObjects
	}
}

func sameReplayZone(a, b model.MatchReplayFrameObjectRow) bool {
	return sameReplayIntPtr(a.ZoneID, b.ZoneID) &&
		strings.EqualFold(strings.TrimSpace(a.ZoneType), strings.TrimSpace(b.ZoneType)) &&
		sameReplayIntPtr(a.ZonePosition, b.ZonePosition)
}

func sameReplayCombatState(a, b model.MatchReplayFrameObjectRow) bool {
	return sameReplayText(a.AttackState, b.AttackState) &&
		sameReplayIntPtr(a.AttackTargetID, b.AttackTargetID) &&
		sameReplayText(a.BlockState, b.BlockState) &&
		sameReplayText(a.BlockAttackerIDsJSON, b.BlockAttackerIDsJSON)
}

func replayHasCombatRole(object model.MatchReplayFrameObjectRow) bool {
	return replayIsAttacking(object) || replayIsBlocking(object)
}

func replayIsAttacking(object model.MatchReplayFrameObjectRow) bool {
	return strings.TrimSpace(object.AttackState) != ""
}

func replayIsBlocking(object model.MatchReplayFrameObjectRow) bool {
	return strings.TrimSpace(object.BlockState) != ""
}

func replayStateChangeRow(object model.MatchReplayFrameObjectRow, action string) model.MatchReplayChangeRow {
	return model.MatchReplayChangeRow{
		InstanceID:     object.InstanceID,
		CardID:         object.CardID,
		CardName:       object.CardName,
		OwnerSeatID:    object.OwnerSeatID,
		PlayerSide:     object.PlayerSide,
		Action:         action,
		ToZoneID:       object.ZoneID,
		ToZoneType:     object.ZoneType,
		ToZonePosition: object.ZonePosition,
		IsToken:        object.IsToken,
	}
}

func sameReplayIntPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameReplayText(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
