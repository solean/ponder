package db

// Shared by match detail and batched colour/matchup queries so every consumer
// uses replay estimates instead of counting a recast card's stale instance IDs.
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
	), live_per_frame AS (
		SELECT rf.match_id, o.card_id,
			COUNT(DISTINCT CASE
				WHEN LOWER(TRIM(COALESCE(o.zone_type, ''))) IN ('battlefield', 'hand') THEN o.instance_id
			END) AS copies
		FROM match_replay_frames rf
		JOIN requested r ON r.id = rf.match_id
		JOIN matches m ON m.id = rf.match_id
		JOIN match_replay_frame_objects o ON o.frame_id = rf.id
		WHERE m.player_seat_id > 0 AND o.card_id > 0 AND o.instance_id > 0
			AND o.is_token = 0 AND o.is_copy = 0
			AND COALESCE(o.controller_seat_id, o.owner_seat_id) > 0
			AND COALESCE(o.controller_seat_id, o.owner_seat_id) <> m.player_seat_id
		GROUP BY rf.match_id, rf.id, o.card_id
	), live AS (
		SELECT match_id, card_id, MAX(1, MAX(copies)) AS quantity
		FROM live_per_frame GROUP BY match_id, card_id
	), derived AS (
		SELECT c.match_id, c.card_id, c.max_copies AS quantity
		FROM match_opponent_card_counts c JOIN requested r ON r.id = c.match_id
	), cards AS (
		SELECT match_id, card_id FROM fallback
		UNION SELECT match_id, card_id FROM live
		UNION SELECT match_id, card_id FROM derived
	), opponent_quantities AS (
		SELECT c.match_id, c.card_id,
			CASE
				WHEN l.quantity IS NULL THEN COALESCE(d.quantity, f.quantity)
				WHEN d.quantity IS NULL THEN l.quantity
				ELSE MAX(l.quantity, d.quantity)
			END AS quantity
		FROM cards c
		LEFT JOIN fallback f ON f.match_id = c.match_id AND f.card_id = c.card_id
		LEFT JOIN live l ON l.match_id = c.match_id AND l.card_id = c.card_id
		LEFT JOIN derived d ON d.match_id = c.match_id AND d.card_id = c.card_id
	)
`
