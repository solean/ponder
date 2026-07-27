package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

const (
	gameDeckSectionMain      = "main"
	gameDeckSectionSideboard = "sideboard"
)

type matchGameDeckCard struct {
	Quantity int64
	CardName string
}

type matchGameDeckSnapshot struct {
	GameNumber int64
	ObservedAt string
	Source     string
	Main       map[int64]matchGameDeckCard
}

// ReplaceMatchGameDeckSnapshot stores the exact player deck configuration GRE
// reported when a game connected. Repeating the same observation is
// idempotent, while a later observation for the game replaces every prior card
// count so partially parsed snapshots cannot linger.
func (s *Store) ReplaceMatchGameDeckSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	arenaMatchID string,
	gameNumber int64,
	observedAt, source string,
	mainCardIDs, sideboardCardIDs []int64,
) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" || !hasPositiveGameDeckCardID(mainCardIDs) {
		return nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
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
		return fmt.Errorf("lookup game deck snapshot match: %w", err)
	}

	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO match_game_deck_snapshots (
			match_id, game_number, observed_at, source, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id, game_number) DO UPDATE SET
			observed_at = excluded.observed_at,
			source = excluded.source,
			updated_at = excluded.updated_at
	`, matchID, gameNumber, nullIfEmpty(normalizeTS(observedAt)),
		nullIfEmpty(strings.TrimSpace(source)), now, now); err != nil {
		return fmt.Errorf("upsert game deck snapshot: %w", err)
	}

	var snapshotID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM match_game_deck_snapshots
		WHERE match_id = ? AND game_number = ?
	`, matchID, gameNumber).Scan(&snapshotID); err != nil {
		return fmt.Errorf("lookup game deck snapshot: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM match_game_deck_snapshot_cards
		WHERE snapshot_id = ?
	`, snapshotID); err != nil {
		return fmt.Errorf("clear game deck snapshot cards: %w", err)
	}

	if err := insertMatchGameDeckSnapshotCards(ctx, tx, snapshotID, gameDeckSectionMain, mainCardIDs); err != nil {
		return err
	}
	if err := insertMatchGameDeckSnapshotCards(ctx, tx, snapshotID, gameDeckSectionSideboard, sideboardCardIDs); err != nil {
		return err
	}
	return nil
}

func hasPositiveGameDeckCardID(cardIDs []int64) bool {
	for _, cardID := range cardIDs {
		if cardID > 0 {
			return true
		}
	}
	return false
}

func insertMatchGameDeckSnapshotCards(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID int64,
	section string,
	cardIDs []int64,
) error {
	quantities := make(map[int64]int64, len(cardIDs))
	for _, cardID := range cardIDs {
		if cardID > 0 {
			quantities[cardID]++
		}
	}

	sortedCardIDs := make([]int64, 0, len(quantities))
	for cardID := range quantities {
		sortedCardIDs = append(sortedCardIDs, cardID)
	}
	sort.Slice(sortedCardIDs, func(i, j int) bool { return sortedCardIDs[i] < sortedCardIDs[j] })

	for _, cardID := range sortedCardIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO match_game_deck_snapshot_cards (
				snapshot_id, section, card_id, quantity
			) VALUES (?, ?, ?, ?)
		`, snapshotID, section, cardID, quantities[cardID]); err != nil {
			return fmt.Errorf("insert %s game deck snapshot card: %w", section, err)
		}
	}
	return nil
}

func (s *Store) attachMatchGameSideboardChanges(
	ctx context.Context,
	matchID int64,
	games []model.GameRow,
) error {
	snapshots, err := s.listMatchGameDeckSnapshots(ctx, matchID)
	if err != nil {
		return err
	}

	for index := range games {
		gameNumber := games[index].GameNumber
		if gameNumber <= 1 {
			continue
		}

		current, ok := snapshots[gameNumber]
		if !ok {
			continue
		}
		previous, ok := snapshots[gameNumber-1]
		if !ok {
			continue
		}

		games[index].SideboardChanges = diffMatchGameDeckSnapshots(previous, current)
	}
	return nil
}

func (s *Store) listMatchGameDeckSnapshots(
	ctx context.Context,
	matchID int64,
) (map[int64]*matchGameDeckSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.game_number,
			COALESCE(s.observed_at, ''),
			COALESCE(s.source, ''),
			c.section,
			c.card_id,
			c.quantity,
			COALESCE(cc.name, '')
		FROM match_game_deck_snapshots s
		LEFT JOIN match_game_deck_snapshot_cards c ON c.snapshot_id = s.id
		LEFT JOIN card_catalog cc ON cc.arena_id = c.card_id
		WHERE s.match_id = ?
		ORDER BY s.game_number, c.section, cc.name, c.card_id
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match game deck snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make(map[int64]*matchGameDeckSnapshot)
	for rows.Next() {
		var (
			gameNumber int64
			observedAt string
			source     string
			section    sql.NullString
			cardID     sql.NullInt64
			quantity   sql.NullInt64
			cardName   string
		)
		if err := rows.Scan(
			&gameNumber, &observedAt, &source, &section, &cardID, &quantity, &cardName,
		); err != nil {
			return nil, fmt.Errorf("scan match game deck snapshot: %w", err)
		}

		snapshot := snapshots[gameNumber]
		if snapshot == nil {
			snapshot = &matchGameDeckSnapshot{
				GameNumber: gameNumber,
				ObservedAt: observedAt,
				Source:     source,
				Main:       make(map[int64]matchGameDeckCard),
			}
			snapshots[gameNumber] = snapshot
		}
		if !section.Valid || section.String != gameDeckSectionMain ||
			!cardID.Valid || cardID.Int64 <= 0 || !quantity.Valid || quantity.Int64 <= 0 {
			continue
		}
		snapshot.Main[cardID.Int64] = matchGameDeckCard{
			Quantity: quantity.Int64,
			CardName: cardName,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match game deck snapshots: %w", err)
	}
	return snapshots, nil
}

func diffMatchGameDeckSnapshots(
	previous, current *matchGameDeckSnapshot,
) *model.GameSideboardChangesRow {
	changes := &model.GameSideboardChangesRow{
		CardsIn:    make([]model.SideboardCardRow, 0),
		CardsOut:   make([]model.SideboardCardRow, 0),
		ObservedAt: current.ObservedAt,
		Source:     current.Source,
	}

	cardIDs := make(map[int64]struct{}, len(previous.Main)+len(current.Main))
	for cardID := range previous.Main {
		cardIDs[cardID] = struct{}{}
	}
	for cardID := range current.Main {
		cardIDs[cardID] = struct{}{}
	}
	for cardID := range cardIDs {
		before := previous.Main[cardID]
		after := current.Main[cardID]
		delta := after.Quantity - before.Quantity
		if delta == 0 {
			continue
		}
		cardName := after.CardName
		if cardName == "" {
			cardName = before.CardName
		}
		card := model.SideboardCardRow{
			CardID:   cardID,
			Quantity: delta,
			CardName: cardName,
		}
		if delta > 0 {
			changes.CardsIn = append(changes.CardsIn, card)
		} else {
			card.Quantity = -delta
			changes.CardsOut = append(changes.CardsOut, card)
		}
	}

	sortSideboardCards(changes.CardsIn)
	sortSideboardCards(changes.CardsOut)
	return changes
}

func sortSideboardCards(cards []model.SideboardCardRow) {
	sort.Slice(cards, func(i, j int) bool {
		leftName := strings.ToLower(strings.TrimSpace(cards[i].CardName))
		rightName := strings.ToLower(strings.TrimSpace(cards[j].CardName))
		if leftName != rightName {
			return leftName < rightName
		}
		return cards[i].CardID < cards[j].CardID
	})
}
