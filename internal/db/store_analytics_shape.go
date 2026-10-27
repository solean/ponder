package db

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// derivedTurnStat is one turn's game shape from the player's perspective.
// Nil pointers mean the underlying data was not observable that turn.
type derivedTurnStat struct {
	TurnNumber   int64
	IsPlayerTurn *bool
	SelfLife     *int64
	OpponentLife *int64
	SelfHandSize *int64
	LandsPlayed  int64
	SpellsCast   int64
	LandInHand   *bool
}

// selfCardPlay is one of the player's own card plays with the fields needed to
// classify it as a land drop or spell cast.
type selfCardPlay struct {
	GameNumber int64
	TurnNumber int64
	CardID     int64
	Zone       string
}

func pointerBool(value bool) *bool {
	copy := value
	return &copy
}

func nullableDerivedBool(value *bool) any {
	if value == nil {
		return nil
	}
	return boolToInt(*value)
}

// loadSelfCardPlays returns the player's own card plays with a known turn,
// ordered by game and turn.
func (s *Store) loadSelfCardPlays(ctx context.Context, matchID int64) ([]selfCardPlay, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cp.game_number, cp.turn_number, cp.card_id, COALESCE(cp.first_public_zone, '')
		FROM match_card_plays cp
		JOIN matches m ON m.id = cp.match_id
		WHERE cp.match_id = ?
		  AND cp.owner_seat_id IS NOT NULL
		  AND cp.owner_seat_id = m.player_seat_id
		  AND cp.turn_number IS NOT NULL
		  AND (cp.is_copy = 0 OR cp.cast_confirmed = 1)
		ORDER BY cp.game_number, cp.turn_number, cp.id
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("load self card plays: %w", err)
	}
	defer rows.Close()

	out := make([]selfCardPlay, 0)
	for rows.Next() {
		var play selfCardPlay
		if err := rows.Scan(&play.GameNumber, &play.TurnNumber, &play.CardID, &play.Zone); err != nil {
			return nil, fmt.Errorf("scan self card play: %w", err)
		}
		out = append(out, play)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate self card plays: %w", err)
	}
	return out, nil
}

// loadLandKnowledge maps card IDs to land-ness from the cached type lines.
// Cards without a cached type line are absent, which callers treat as unknown.
func (s *Store) loadLandKnowledge(ctx context.Context, cardIDs []int64) (map[int64]bool, error) {
	unique := make([]int64, 0, len(cardIDs))
	seen := make(map[int64]bool, len(cardIDs))
	for _, cardID := range cardIDs {
		if cardID <= 0 || seen[cardID] {
			continue
		}
		seen[cardID] = true
		unique = append(unique, cardID)
	}
	typeLines, err := s.LookupCardTypeLines(ctx, unique)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(typeLines))
	for cardID, typeLine := range typeLines {
		out[cardID] = strings.Contains(strings.ToLower(typeLine), "land")
	}
	return out, nil
}

// classifySelfPlay reports whether a play was a land drop or a spell cast.
// Lands are never cast, so a stack appearance is always a spell (this also
// keeps modal spell//land cards cast as spells out of the land count). A
// battlefield-first appearance is a land drop unless the type line says
// otherwise; nonland cards put straight onto the battlefield count as neither.
func classifySelfPlay(play selfCardPlay, landByCard map[int64]bool) (isLand, isSpell bool) {
	zone := strings.ToLower(strings.TrimSpace(play.Zone))
	if zone == "stack" {
		return false, true
	}
	if zone != "battlefield" {
		return false, false
	}
	if isLandCard, known := landByCard[play.CardID]; known {
		return isLandCard, false
	}
	return true, false
}

// copyCleanZones are the zones where a card sits as its own physical self and
// leaves when it moves: the battlefield and the hand. Instance IDs churn every
// time a card changes zones, and the transit/discard zones (graveyard, exile,
// limbo, stack) accumulate a recast card's stale instances, so only these
// zones give an honest copy count.
var copyCleanZones = map[string]struct{}{
	"battlefield": {},
	"hand":        {},
}

// deriveOpponentCardCopies estimates how many copies of each opponent card
// exist: the most non-token instances of that card the opponent had in the
// clean zones (battlefield plus hand) at once in any single frame. This is a
// true lower bound on copies and is not inflated by a single card churning
// through instance ids as it dies and is recast. Any opponent card seen at all
// floors to one so a spell only ever glimpsed in the graveyard still reports a
// copy rather than falling back to the inflated distinct-instance count.
func deriveOpponentCardCopies(frames []model.MatchReplayFrameRow) map[int64]int64 {
	maxByCard := make(map[int64]int64)
	seen := make(map[int64]bool)
	for _, frame := range frames {
		perCard := make(map[int64]map[int64]struct{})
		for _, object := range frame.Objects {
			if object.PlayerSide != "opponent" || object.IsToken || object.IsCopy {
				continue
			}
			if object.CardID <= 0 || object.InstanceID <= 0 {
				continue
			}
			seen[object.CardID] = true
			if _, clean := copyCleanZones[strings.ToLower(strings.TrimSpace(object.ZoneType))]; !clean {
				continue
			}
			instances := perCard[object.CardID]
			if instances == nil {
				instances = make(map[int64]struct{})
				perCard[object.CardID] = instances
			}
			instances[object.InstanceID] = struct{}{}
		}
		for cardID, instances := range perCard {
			if count := int64(len(instances)); count > maxByCard[cardID] {
				maxByCard[cardID] = count
			}
		}
	}
	for cardID := range seen {
		if maxByCard[cardID] < 1 {
			maxByCard[cardID] = 1
		}
	}
	return maxByCard
}

// deriveGameTurnStats reduces one game's replay frames and self card plays to
// per-turn shape rows. Life, hand size, and land-in-hand come from the last
// frame observed in each turn; a turn with plays but no frame still gets a row
// with those fields nil.
func deriveGameTurnStats(
	frames []model.MatchReplayFrameRow,
	plays []selfCardPlay,
	playDraw string,
	landByCard map[int64]bool,
) []derivedTurnStat {
	byTurn := make(map[int64]*derivedTurnStat)
	ensure := func(turn int64) *derivedTurnStat {
		stat, ok := byTurn[turn]
		if !ok {
			stat = &derivedTurnStat{TurnNumber: turn}
			byTurn[turn] = stat
		}
		return stat
	}

	for _, frame := range frames {
		if !replayFrameIsPlay(frame) || frame.TurnNumber == nil || *frame.TurnNumber <= 0 {
			continue
		}
		stat := ensure(*frame.TurnNumber)
		if frame.SelfLifeTotal != nil {
			stat.SelfLife = pointerInt64(*frame.SelfLifeTotal)
		}
		if frame.OpponentLifeTotal != nil {
			stat.OpponentLife = pointerInt64(*frame.OpponentLifeTotal)
		}
		if len(frame.Objects) == 0 {
			continue
		}
		hand := replaySelfHand(frame)
		stat.SelfHandSize = pointerInt64(int64(len(hand.ByInstance)))
		stat.LandInHand = landInHand(hand, landByCard)
	}

	for _, play := range plays {
		if play.TurnNumber <= 0 {
			continue
		}
		isLand, isSpell := classifySelfPlay(play, landByCard)
		stat := ensure(play.TurnNumber)
		if isLand {
			stat.LandsPlayed++
		}
		if isSpell {
			stat.SpellsCast++
		}
	}

	turns := make([]int64, 0, len(byTurn))
	for turn := range byTurn {
		turns = append(turns, turn)
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i] < turns[j] })

	out := make([]derivedTurnStat, 0, len(turns))
	for _, turn := range turns {
		stat := byTurn[turn]
		switch playDraw {
		case "play":
			stat.IsPlayerTurn = pointerBool(turn%2 == 1)
		case "draw":
			stat.IsPlayerTurn = pointerBool(turn%2 == 0)
		}
		out = append(out, *stat)
	}
	return out
}

// landInHand reports whether the hand holds a land: true when any card is a
// known land, false when every card is a known nonland, nil when unresolved
// type lines leave it ambiguous.
func landInHand(hand replayHandSnapshot, landByCard map[int64]bool) *bool {
	allKnown := true
	for _, cardID := range hand.ByInstance {
		isLand, known := landByCard[cardID]
		if !known {
			allKnown = false
			continue
		}
		if isLand {
			return pointerBool(true)
		}
	}
	if allKnown {
		return pointerBool(false)
	}
	return nil
}

const flagMissedLandDrop = "missed_land_drop"

// deriveGameFlags computes descriptive decision-review flags from turn stats.
// A missed land drop is an own turn that ended with no land played while a
// land sat in hand; the game's final turn is skipped because a concede or
// game-ending action can cut that turn short before a land could be played.
func deriveGameFlags(turnStats []model.GameTurnStatRow) []model.GameFlagRow {
	if len(turnStats) == 0 {
		return []model.GameFlagRow{}
	}
	maxTurn := int64(0)
	for _, stat := range turnStats {
		if stat.TurnNumber > maxTurn {
			maxTurn = stat.TurnNumber
		}
	}
	flags := make([]model.GameFlagRow, 0)
	for _, stat := range turnStats {
		if stat.IsPlayerTurn == nil || !*stat.IsPlayerTurn || stat.TurnNumber >= maxTurn {
			continue
		}
		if stat.LandsPlayed == 0 && stat.LandInHand != nil && *stat.LandInHand {
			flags = append(flags, model.GameFlagRow{
				Flag:       flagMissedLandDrop,
				TurnNumber: pointerInt64(stat.TurnNumber),
				Detail:     "No land played while holding at least one land",
				Confidence: "heuristic",
			})
		}
	}
	return flags
}

// ListMatchAnalyticsCardIDs returns the distinct cards this match played or
// held in hand, so callers can resolve their type lines before analytics
// classify land drops and lands in hand.
func (s *Store) ListMatchAnalyticsCardIDs(ctx context.Context, matchID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.card_id FROM game_card_stats s WHERE s.match_id = ?
		UNION
		SELECT cp.card_id FROM match_card_plays cp WHERE cp.match_id = ?
	`, matchID, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match analytics cards: %w", err)
	}
	defer rows.Close()

	cardIDs := make([]int64, 0)
	for rows.Next() {
		var cardID int64
		if err := rows.Scan(&cardID); err != nil {
			return nil, fmt.Errorf("scan match analytics card: %w", err)
		}
		cardIDs = append(cardIDs, cardID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match analytics cards: %w", err)
	}
	return cardIDs, nil
}

// ListGameTurnStats returns the stored per-turn shape rows for one game.
func (s *Store) listGameTurnStats(ctx context.Context, gameID int64) ([]model.GameTurnStatRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT turn_number, is_player_turn, self_life, opponent_life,
			self_hand_size, lands_played, spells_cast, land_in_hand
		FROM game_turn_stats
		WHERE game_id = ?
		ORDER BY turn_number
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list game turn stats: %w", err)
	}
	defer rows.Close()

	out := make([]model.GameTurnStatRow, 0)
	for rows.Next() {
		var stat model.GameTurnStatRow
		var isPlayerTurn, landInHand *int64
		if err := rows.Scan(&stat.TurnNumber, &isPlayerTurn, &stat.SelfLife, &stat.OpponentLife,
			&stat.SelfHandSize, &stat.LandsPlayed, &stat.SpellsCast, &landInHand); err != nil {
			return nil, fmt.Errorf("scan game turn stat: %w", err)
		}
		if isPlayerTurn != nil {
			stat.IsPlayerTurn = pointerBool(*isPlayerTurn != 0)
		}
		if landInHand != nil {
			stat.LandInHand = pointerBool(*landInHand != 0)
		}
		out = append(out, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game turn stats: %w", err)
	}
	return out, nil
}
