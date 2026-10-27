package ingest

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSpellCopiesRetainTimelineWithoutInflatingCards(t *testing.T) {
	ctx := context.Background()
	database, store, logPath := checkpointDatabase(t)
	objects := []map[string]any{}
	for _, spec := range []struct {
		id, card, seat int
		copy, token    bool
	}{
		{101, 5001, 1, false, false}, // real opponent card
		{102, 5001, 1, true, false},  // copied same spell
		{103, 5002, 1, true, false},  // prepared opponent spell, actually cast
		{104, 5003, 1, false, false}, // copy identity arrives in a later diff
		{201, 6001, 2, true, false},  // self copy placed on stack
		{202, 6002, 2, true, false},  // self copy actually cast
		{203, 6003, 2, false, false}, // real self spell
		{204, 6004, 2, true, false},  // prepared copy initially visible in exile
		{301, 7001, 1, false, true},  // token behavior remains unchanged
	} {
		zone := 27
		if spec.id == 204 {
			zone = 29
		}
		objects = append(objects, map[string]any{
			"instanceId": spec.id, "grpId": spec.card, "type": "GameObjectType_Card",
			"ownerSeatId": spec.seat, "controllerSeatId": spec.seat, "zoneId": zone,
			"visibility": "Visibility_Public", "isCopy": spec.copy, "isToken": spec.token,
		})
	}
	annotations := []map[string]any{}
	for _, id := range []int{103, 202, 203} {
		annotations = append(annotations, map[string]any{
			"type": []string{"AnnotationType_ZoneTransfer"}, "affectedIds": []int{id},
			"details": []map[string]any{{"key": "category", "valueString": []string{"CastSpell"}}},
		})
	}
	message := map[string]any{
		"type": "GameStateType_Full", "gameStateId": 1,
		"gameInfo": map[string]any{"matchID": "match-context", "gameNumber": 1, "stage": "GameStage_Playing"},
		"turnInfo": map[string]any{"turnNumber": 3, "phase": "Phase_Main1", "activePlayer": 2},
		"zones": []map[string]any{
			{"zoneId": 27, "type": "ZoneType_Stack", "visibility": "Visibility_Public", "objectInstanceIds": []int{101, 102, 103, 104, 201, 202, 203, 301}},
			{"zoneId": 29, "type": "ZoneType_Exile", "visibility": "Visibility_Public", "objectInstanceIds": []int{204}},
		},
		"gameObjects": objects, "annotations": annotations,
	}
	greLine := func(msg map[string]any) string {
		wire, err := json.Marshal(map[string]any{"greToClientEvent": map[string]any{"greToClientMessages": []map[string]any{{"systemSeatIds": []int{2}, "gameStateMessage": msg}}}})
		if err != nil {
			t.Fatal(err)
		}
		return string(wire)
	}
	lines := append(checkpointFixture()[:2], greLine(message))
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureMatchAnalytics(ctx, 1); err != nil {
		t.Fatal(err)
	}
	// Resume from the durable checkpoint. Sparse details must not erase the
	// copy flags, and a late flag must remove earlier physical-card evidence.
	diff := greLine(map[string]any{
		"type": "GameStateType_Diff", "gameStateId": 2, "prevGameStateId": 1,
		"gameObjects": []map[string]any{
			{"instanceId": 102, "type": "GameObjectType_Card", "zoneId": 27},
			{"instanceId": 104, "type": "GameObjectType_Card", "isCopy": true},
			{"instanceId": 204, "type": "GameObjectType_Card", "zoneId": 27},
		},
		"zones": []map[string]any{
			{"zoneId": 27, "objectInstanceIds": []int{101, 102, 103, 104, 201, 202, 203, 204, 301}},
			{"zoneId": 29, "objectInstanceIds": []int{}},
		},
		"annotations": []map[string]any{{
			"type": []string{"AnnotationType_ZoneTransfer"}, "affectedIds": []int{204},
			"details": []map[string]any{{"key": "category", "valueString": []string{"CastSpell"}}},
		}},
	})
	if err := writeLogLines(logPath, []string{diff}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	checkCounts := func() {
		t.Helper()
		quantities, err := store.ListMatchOpponentCardQuantities(ctx, []int64{1})
		if err != nil {
			t.Fatal(err)
		}
		if len(quantities[1]) != 1 || quantities[1][5001] != 1 {
			t.Fatalf("opponent counts include copies: %v", quantities)
		}
	}
	checkCounts()
	selfCounts, available, err := store.GetLiveKnownSelfCardCounts(ctx, 1, 1)
	if err != nil || !available || len(selfCounts) != 1 || selfCounts[6003] != 1 {
		t.Fatalf("self deck counts include copies: %v, available=%v, err=%v", selfCounts, available, err)
	}
	if err := writeLogLines(logPath, []string{checkpointCompletedMatch("match-context", "self-user")}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	checkCounts()
	plays, err := store.ListMatchCardPlays(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 8 {
		t.Fatalf("timeline has %d entries; want 8 including copies, excluding token", len(plays))
	}
	for _, play := range plays {
		if play.InstanceID == 102 && (!play.IsCopy || play.CastConfirmed) {
			t.Fatalf("copied stack spell misclassified: %+v", play)
		}
		if (play.InstanceID == 202 || play.InstanceID == 204) && (!play.IsCopy || !play.CastConfirmed) {
			t.Fatalf("cast copy misclassified: %+v", play)
		}
	}
	var spells, copyStats int
	if err := database.QueryRow(`SELECT SUM(spells_cast) FROM game_turn_stats`).Scan(&spells); err != nil {
		t.Fatal(err)
	}
	if spells != 3 {
		t.Fatalf("spells cast = %d, want real spell + two confirmed cast copies", spells)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM game_card_stats WHERE card_id IN (6001, 6002, 6004)`).Scan(&copyStats); err != nil {
		t.Fatal(err)
	}
	if copyStats != 0 {
		t.Fatal("generated copies polluted deck-card performance stats")
	}
	frames, err := store.ListMatchReplayFrames(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		for _, object := range frame.Objects {
			if object.InstanceID == 102 && !object.IsCopy {
				t.Fatal("copy identity lost through sparse diffs or archival")
			}
		}
	}
}
