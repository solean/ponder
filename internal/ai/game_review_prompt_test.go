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
