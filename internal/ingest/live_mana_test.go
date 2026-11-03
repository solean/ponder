package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestLiveManaTracksTapPoolAndRestart(t *testing.T) {
	ctx := context.Background()
	database, store, path := checkpointDatabase(t)
	lines := checkpointFixture()[:2]
	land := func(tapped bool) map[string]any {
		object := map[string]any{"instanceId": 101, "grpId": 12053, "type": "GameObjectType_Card", "zoneId": 28,
			"visibility": "Visibility_Public", "ownerSeatId": 2, "controllerSeatId": 2, "cardTypes": []string{"CardType_Land"},
			"uniqueAbilities": []map[string]int{{"grpId": 1002}}}
		if tapped {
			object["isTapped"] = true
		}
		return object
	}
	appendState := func(state int, object map[string]any, players []map[string]any) {
		t.Helper()
		msg := map[string]any{"type": "GameStateType_Diff", "gameStateId": state, "prevGameStateId": state - 1,
			"gameInfo": map[string]any{"matchID": "match-context", "gameNumber": 1},
			"zones":    []map[string]any{{"zoneId": 28, "type": "ZoneType_Battlefield", "visibility": "Visibility_Public", "objectInstanceIds": []int{101}}}}
		if state == 1 {
			msg["type"] = "GameStateType_Full"
		}
		if object != nil {
			msg["gameObjects"] = []map[string]any{object}
		}
		if players != nil {
			msg["players"] = players
		}
		envelope := map[string]any{"timestamp": fmt.Sprint(1772330782309 + state), "greToClientEvent": map[string]any{
			"greToClientMessages": []map[string]any{{"systemSeatIds": []int{2}, "gameStateMessage": msg}}}}
		raw, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
		if err := writeLogLines(path, lines, false); err != nil {
			t.Fatal(err)
		}
		// A fresh parser on each update exercises checkpoint resume as well.
		if _, err := NewParser(store).ParseFile(ctx, path, true); err != nil {
			t.Fatal(err)
		}
	}
	appendState(1, land(false), []map[string]any{{"systemSeatNumber": 2, "lifeTotal": 20}})
	var matchID int64
	if err := database.QueryRow("SELECT id FROM matches WHERE arena_match_id='match-context'").Scan(&matchID); err != nil {
		t.Fatal(err)
	}
	check := func(tapped bool, floating int64) {
		t.Helper()
		objects, pool, available, known, err := store.GetLiveManaState(ctx, matchID, 1)
		if err != nil || !available || !known || len(objects) != 1 || objects[0].IsTapped != tapped {
			t.Fatalf("state: %v %v %v %v %v", objects, pool, available, known, err)
		}
		var count int64
		for _, mana := range pool {
			count += mana.Count
		}
		if count != floating {
			t.Fatalf("pool = %v, want %d", pool, floating)
		}
	}
	check(false, 0)
	appendState(2, land(true), []map[string]any{{"systemSeatNumber": 2, "lifeTotal": 20, "manaPool": []map[string]any{{"color": "ManaColor_Blue", "count": 1}}}})
	check(true, 1)
	// Other players' updates must not clear our pool.
	appendState(3, nil, []map[string]any{{"systemSeatNumber": 1, "lifeTotal": 19}})
	check(true, 1)
	// Arena omits isTapped on the untapped object and manaPool on an empty pool.
	appendState(4, land(false), []map[string]any{{"systemSeatNumber": 2, "lifeTotal": 20}})
	check(false, 0)
}
