package ai

import (
	"strings"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestBuildGameReviewPromptGroundsCoachingInObservedDecisions(t *testing.T) {
	turn := int64(6)
	selfLife := int64(14)
	opponentLife := int64(9)
	input := GameReviewInput{
		Match: model.MatchRow{ID: 42, Format: "Standard", EventName: "Ranked"},
		Game: model.GameRow{
			GameNumber: 1,
			Result:     "loss",
			PlayDraw:   "play",
			Flags: []model.GameFlagRow{{
				Flag:       "missed_land_drop",
				TurnNumber: &turn,
				Detail:     "land remained visible in hand",
				Confidence: "high",
			}},
		},
		DeckCards: []model.DeckCardRow{{Section: "main", CardID: 100, CardName: "Test Mage", Quantity: 4}},
		Frames: []model.MatchReplayFrameRow{{
			TurnNumber:        &turn,
			Phase:             "main1",
			SelfLifeTotal:     &selfLife,
			OpponentLifeTotal: &opponentLife,
			ActionsJSON:       `[{"seatId":1,"action":{"actionType":"ActionType_Cast","instanceId":7}}]`,
			AnnotationsJSON:   `{"annotations":[{"type":["AnnotationType_UserActionTaken"]}]}`,
			Changes: []model.MatchReplayChangeRow{{
				CardID:       100,
				CardName:     "Test Mage",
				PlayerSide:   "self",
				Action:       "zone_transfer",
				FromZoneType: "hand",
				ToZoneType:   "battlefield",
			}},
			Objects: []model.MatchReplayFrameObjectRow{{
				CardID:     100,
				CardName:   "Test Mage",
				PlayerSide: "self",
				ZoneType:   "battlefield",
			}},
		}},
	}

	prompt := BuildGameReviewPrompt(input)
	for _, expected := range []string{
		"Format: Standard",
		"4x Test Mage",
		"missed_land_drop at Arena turn 6",
		"Zone change: self Test Mage hand -> battlefield",
		"Actions Arena offered before the recorded user action (not actions necessarily taken)",
		"Every claimed mistake must cite the Arena turn and phase",
		`## Misplays and Better Lines`,
		`Confidence: High, Medium, or Low`,
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q", expected)
		}
	}

	originalHash := GameReviewSourceHash(input)
	input.Frames[0].Phase = "combat"
	if changedHash := GameReviewSourceHash(input); changedHash == originalHash {
		t.Fatal("source hash did not change with replay evidence")
	}
}

func TestBuildGameReviewPromptLabelsWhoseTurnItIs(t *testing.T) {
	pilotTurn := true
	opponentTurn := false
	landHeld := true
	turnOne := int64(1)
	turnTwo := int64(2)
	input := GameReviewInput{
		Match: model.MatchRow{ID: 42},
		Game: model.GameRow{
			GameNumber: 1,
			PlayDraw:   "draw",
			TurnStats: []model.GameTurnStatRow{
				{TurnNumber: 1, IsPlayerTurn: &opponentTurn, LandsPlayed: 0, LandInHand: &landHeld},
				{TurnNumber: 2, IsPlayerTurn: &pilotTurn, LandsPlayed: 1, LandInHand: &landHeld},
			},
		},
		Frames: []model.MatchReplayFrameRow{
			{TurnNumber: &turnOne, Phase: "main1"},
			{TurnNumber: &turnTwo, Phase: "main1"},
		},
	}

	prompt := BuildGameReviewPrompt(input)
	for _, expected := range []string{
		"Arena turn 1 (opponent turn): pilot lands played 0",
		"Arena turn 2 (pilot turn): pilot lands played 1",
		"Step 1 — Arena turn 1 (opponent turn), main1:",
		"Step 2 — Arena turn 2 (pilot turn), main1:",
		"Never treat zero pilot lands or spells on an opponent turn as a missed action or pass",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q", expected)
		}
	}
}

func TestBuildGameReviewPromptDoesNotPresentOfferedActionsAsTaken(t *testing.T) {
	input := GameReviewInput{
		Match: model.MatchRow{ID: 1},
		Game:  model.GameRow{GameNumber: 1},
		Frames: []model.MatchReplayFrameRow{{
			ActionsJSON: `[{"action":{"actionType":"ActionType_Cast"}}]`,
		}},
	}

	if prompt := BuildGameReviewPrompt(input); strings.Contains(prompt, "ActionType_Cast") {
		t.Fatal("prompt included offered actions without a recorded user action")
	}
}

func TestBuildGameReviewPromptSkipsUneventfulFramesAndBookkeeping(t *testing.T) {
	turn := int64(3)
	life := int64(20)
	pilotSeat := int64(1)
	opponentSeat := int64(2)
	board := []model.MatchReplayFrameObjectRow{
		{InstanceID: 200, CardID: 10, CardName: "Shock", PlayerSide: "self", ControllerSeatID: &pilotSeat, ZoneType: "stack"},
		{InstanceID: 300, CardID: 20, CardName: "Grizzly Bears", PlayerSide: "opponent", ControllerSeatID: &opponentSeat, ZoneType: "battlefield"},
	}
	frame := func(annotations string, changes ...model.MatchReplayChangeRow) model.MatchReplayFrameRow {
		return model.MatchReplayFrameRow{
			TurnNumber:        &turn,
			Phase:             "main1",
			SelfLifeTotal:     &life,
			OpponentLifeTotal: &life,
			WinningPlayerSide: "unknown",
			AnnotationsJSON:   annotations,
			Objects:           board,
			Changes:           changes,
		}
	}
	castFrame := frame(`{"annotations":[{"type":["AnnotationType_ObjectIdChanged"],"details":[{"key":"orig_id","valueInt32":[150]},{"key":"new_id","valueInt32":[200]}]},{"affectorId":1,"affectedIds":[200],"details":[{"key":"actionType","valueInt32":[1]}],"type":["AnnotationType_UserActionTaken"]}],` +
		`"persistentAnnotations":[{"id":9,"affectorId":200,"affectedIds":[300],"type":["AnnotationType_TargetSpec"]}]}`)
	castFrame.ActionsJSON = `[{"seatId":1,"action":{"actionType":"ActionType_Cast","instanceId":150,"manaCost":[{"color":["ManaColor_Generic"],"count":1},{"color":["ManaColor_Red"],"count":1}]}},` +
		`{"seatId":1,"action":{"actionType":"ActionType_Cast","instanceId":200,"manaCost":[{"color":["ManaColor_Generic"],"count":1},{"color":["ManaColor_Red"],"count":1}]}},` +
		`{"seatId":1,"action":{"actionType":"ActionType_Activate_Mana","instanceId":400}}]`
	input := GameReviewInput{
		Match: model.MatchRow{ID: 1},
		Game:  model.GameRow{GameNumber: 1},
		Frames: []model.MatchReplayFrameRow{
			frame(""),
			frame("", model.MatchReplayChangeRow{CardName: "Grizzly Bears", PlayerSide: "opponent", FromZoneType: "battlefield", ToZoneType: "battlefield", Action: "move_public"}),
			frame(`{"annotations":[{"affectorId":1,"affectedIds":[400],"details":[{"key":"actionType","valueInt32":[4]}],"type":["AnnotationType_UserActionTaken"]}]}`),
			castFrame,
			frame(`{"annotations":[{"affectorId":200,"affectedIds":[300],"details":[{"key":"damage","valueInt32":[2]}],"type":["AnnotationType_DamageDealt"]}],` +
				`"persistentAnnotations":[{"id":9,"affectorId":200,"affectedIds":[300],"type":["AnnotationType_TargetSpec"]}]}`),
			frame(`{"annotations":[{"affectorId":500,"affectedIds":[501],"type":["AnnotationType_AbilityInstanceCreated"]},` +
				`{"affectorId":500,"affectedIds":[501],"type":["AnnotationType_AbilityInstanceDeleted"]}]}`),
			frame(""),
		},
	}
	ended := frame("")
	ended.WinningPlayerSide = "opponent"
	ended.WinReason = "Concede"
	afterEnd := frame("")
	afterEnd.WinReason = "Concede"
	input.Frames = append(input.Frames, ended, afterEnd)

	prompt := BuildGameReviewPrompt(input)
	for _, expected := range []string{
		"- Game end: opponent won; reason Concede",
		"(not actions necessarily taken): pilot cast: Shock {1}{R}\n",
		"pilot cast Shock",
		"Shock targets Grizzly Bears",
		"Shock dealt 2 damage to Grizzly Bears",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q", expected)
		}
	}
	for _, unexpected := range []string{
		"unknown won",
		"battlefield -> battlefield",
		"Step 2 ",
		"Step 3 ",
		"Step 6 ",
		"ability put on the stack",
		"AnnotationType_",
		"ActionType_",
		"object 400",
		"object 150",
	} {
		if strings.Contains(prompt, unexpected) {
			t.Errorf("prompt unexpectedly contains %q", unexpected)
		}
	}
	if count := strings.Count(prompt, "targets Grizzly Bears"); count != 1 {
		t.Errorf("persistent target reported %d times, want 1", count)
	}
	if count := strings.Count(prompt, "- Opponent battlefield:"); count != 2 {
		t.Errorf("unchanged board repeated %d times, want first and last step only", count)
	}
}
