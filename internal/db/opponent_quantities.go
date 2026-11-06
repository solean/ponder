package db

// Shared by match detail and batched colour/matchup queries. It yields, per
// match and card, the replay-derived copy count (derived) and the
// distinct-instance-per-game count (fallback), which overcounts a card whose
// instance id churned as it moved between zones and is only used when no
// replay evidence exists. Live replay frames are counted in Go
// (liveOpponentCardCopies) and merged by mergeOpponentCardQuantity.
// The sole format parameter is the requested-match placeholder list.
const opponentCardQuantitiesCTE = `
	WITH requested AS (
		SELECT id FROM matches WHERE id IN (%s)
	), per_game AS (
		SELECT oc.match_id, oc.card_id, oc.game_number, COUNT(*) AS quantity
		FROM match_opponent_card_instances oc
		JOIN requested r ON r.id = oc.match_id
		GROUP BY oc.match_id, oc.card_id, oc.game_number
	), fallback AS (
		SELECT match_id, card_id, MAX(quantity) AS quantity
		FROM per_game GROUP BY match_id, card_id
	), derived AS (
		SELECT c.match_id, c.card_id, c.max_copies AS quantity
		FROM match_opponent_card_counts c JOIN requested r ON r.id = c.match_id
	), cards AS (
		SELECT match_id, card_id FROM fallback
		UNION SELECT match_id, card_id FROM derived
	), opponent_quantity_sources AS (
		SELECT c.match_id, c.card_id, d.quantity AS derived, f.quantity AS fallback
		FROM cards c
		LEFT JOIN fallback f ON f.match_id = c.match_id AND f.card_id = c.card_id
		LEFT JOIN derived d ON d.match_id = c.match_id AND d.card_id = c.card_id
	)
`

// mergeOpponentCardQuantity picks one card's copy count. Live frame lineages
// win, but never drop below analytics derived earlier from the same match;
// otherwise derived counts beat the inflated distinct-instance fallback.
func mergeOpponentCardQuantity(live, derived, fallback *int64) int64 {
	switch {
	case live != nil && derived != nil:
		return max(*live, *derived)
	case live != nil:
		return *live
	case derived != nil:
		return *derived
	case fallback != nil:
		return *fallback
	default:
		return 0
	}
}
