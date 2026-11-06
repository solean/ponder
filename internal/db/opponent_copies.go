package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// opponentCopyCounter counts physical opponent cards. Arena gives a card a new
// instance id on every zone change and records the move as an ObjectIdChanged
// annotation, so chaining those ids yields one lineage per physical card: a
// card cast, killed and reanimated is still one card, while two copies of an
// instant cast on different turns are two.
type opponentCopyCounter struct {
	games map[int64]*opponentCopyGame
}

type opponentCopyGame struct {
	parent map[int64]int64
	linked bool
	// instances holds every non-Limbo instance id seen per card. Limbo only
	// holds the stale ids a card left behind, which its lineage already covers.
	instances map[int64]map[int64]struct{}
	// cleanMax is the most instances seen at once in battlefield plus hand.
	// It stands in for lineages only in a game that recorded too few frames to
	// carry any id links; callers that never report it get lineage counts.
	cleanMax map[int64]int64
}

func newOpponentCopyCounter() *opponentCopyCounter {
	return &opponentCopyCounter{games: make(map[int64]*opponentCopyGame)}
}

func (c *opponentCopyCounter) game(gameNumber int64) *opponentCopyGame {
	g := c.games[gameNumber]
	if g == nil {
		g = &opponentCopyGame{
			parent:    make(map[int64]int64),
			instances: make(map[int64]map[int64]struct{}),
			cleanMax:  make(map[int64]int64),
		}
		c.games[gameNumber] = g
	}
	return g
}

func (g *opponentCopyGame) root(id int64) int64 {
	for {
		parent, ok := g.parent[id]
		if !ok || parent == id {
			return id
		}
		if grand, ok := g.parent[parent]; ok {
			g.parent[id] = grand
		}
		id = parent
	}
}

// link records that newID is the same physical card as origID.
func (c *opponentCopyCounter) link(gameNumber, origID, newID int64) {
	if origID <= 0 || newID <= 0 || origID == newID {
		return
	}
	g := c.game(gameNumber)
	g.linked = true
	origRoot, newRoot := g.root(origID), g.root(newID)
	if origRoot != newRoot {
		g.parent[newRoot] = origRoot
	}
}

func (c *opponentCopyCounter) linkAnnotations(gameNumber int64, annotationsJSON string) {
	if !strings.Contains(annotationsJSON, "ObjectIdChanged") {
		return
	}
	for newID, origID := range replayObjectIDReplacements(annotationsJSON) {
		c.link(gameNumber, origID, newID)
	}
}

func (c *opponentCopyCounter) observe(gameNumber, cardID, instanceID int64) {
	if cardID <= 0 || instanceID <= 0 {
		return
	}
	g := c.game(gameNumber)
	ids := g.instances[cardID]
	if ids == nil {
		ids = make(map[int64]struct{})
		g.instances[cardID] = ids
	}
	ids[instanceID] = struct{}{}
}

func (c *opponentCopyCounter) observeClean(gameNumber, cardID, copies int64) {
	g := c.game(gameNumber)
	if copies > g.cleanMax[cardID] {
		g.cleanMax[cardID] = copies
	}
}

// counts returns, per card, the most physical copies seen in any one game.
func (c *opponentCopyCounter) counts() map[int64]int64 {
	out := make(map[int64]int64)
	for _, g := range c.games {
		for cardID, ids := range g.instances {
			var copies int64
			if g.linked || len(g.cleanMax) == 0 {
				roots := make(map[int64]struct{}, len(ids))
				for id := range ids {
					roots[g.root(id)] = struct{}{}
				}
				copies = int64(len(roots))
			} else {
				// Without links, churned ids are indistinguishable from
				// copies; trust only what coexisted, floored at one.
				copies = max(g.cleanMax[cardID], 1)
			}
			if copies > out[cardID] {
				out[cardID] = copies
			}
		}
	}
	return out
}

// countableOpponentCard reports whether a replay object is a physical opponent
// card: not a token, not a spell copy, and not a stale Limbo id.
func countableOpponentCard(object model.MatchReplayFrameObjectRow) bool {
	return object.PlayerSide == "opponent" && !object.IsToken && !object.IsCopy &&
		object.CardID > 0 && object.InstanceID > 0 &&
		replayNormalizedZoneType(object.ZoneType) != "limbo"
}

// liveOpponentCardCopies counts opponent copies for matches whose replay frames
// are still relational rows (a match in progress), keyed by match then card.
func (s *Store) liveOpponentCardCopies(ctx context.Context, matchIDs []int64) (map[int64]map[int64]int64, error) {
	out := make(map[int64]map[int64]int64)
	if len(matchIDs) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(matchIDs)), ",")
	args := make([]any, len(matchIDs))
	for i, id := range matchIDs {
		args[i] = id
	}
	counters := make(map[int64]*opponentCopyCounter)
	counter := func(matchID int64) *opponentCopyCounter {
		c := counters[matchID]
		if c == nil {
			c = newOpponentCopyCounter()
			counters[matchID] = c
		}
		return c
	}

	// Each query is drained and closed before the next one opens: the store
	// may run on a single connection.
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT rf.match_id, COALESCE(rf.game_number, 1), o.card_id, o.instance_id
		FROM match_replay_frames rf
		JOIN matches m ON m.id = rf.match_id
		JOIN match_replay_frame_objects o ON o.frame_id = rf.id
		WHERE rf.match_id IN (`+placeholders+`)
			AND m.player_seat_id > 0 AND o.card_id > 0 AND o.instance_id > 0
			AND o.is_token = 0 AND o.is_copy = 0
			AND LOWER(TRIM(COALESCE(o.zone_type, ''))) <> 'limbo'
			AND COALESCE(o.controller_seat_id, o.owner_seat_id) > 0
			AND COALESCE(o.controller_seat_id, o.owner_seat_id) <> m.player_seat_id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list live opponent card instances: %w", err)
	}
	for rows.Next() {
		var matchID, game, cardID, instanceID int64
		if err := rows.Scan(&matchID, &game, &cardID, &instanceID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan live opponent card instance: %w", err)
		}
		counter(matchID).observe(game, cardID, instanceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate live opponent card instances: %w", err)
	}
	rows.Close()
	if len(counters) == 0 {
		return out, nil
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT match_id, COALESCE(game_number, 1), annotations_json
		FROM match_replay_frames
		WHERE match_id IN (`+placeholders+`)
			AND annotations_json LIKE '%ObjectIdChanged%'
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list live replay id links: %w", err)
	}
	for rows.Next() {
		var matchID, game int64
		var annotations string
		if err := rows.Scan(&matchID, &game, &annotations); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan live replay id links: %w", err)
		}
		if c := counters[matchID]; c != nil {
			c.linkAnnotations(game, annotations)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate live replay id links: %w", err)
	}
	rows.Close()

	// No clean-zone maxima are reported here: a live game is recorded frame by
	// frame, so one with no links yet has had no id churn and its distinct
	// instances are already distinct cards.
	for matchID, c := range counters {
		out[matchID] = c.counts()
	}
	return out, nil
}
