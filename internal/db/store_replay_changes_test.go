package db

import (
	"testing"

	"github.com/solean/ponder/internal/model"
)

func replayChangeTestObject(instanceID, cardID, ownerSeatID int64, playerSide, zoneType, visibility string) model.MatchReplayFrameObjectRow {
	return model.MatchReplayFrameObjectRow{
		InstanceID:   instanceID,
		CardID:       cardID,
		OwnerSeatID:  &ownerSeatID,
		PlayerSide:   playerSide,
		ZoneType:     zoneType,
		Visibility:   visibility,
		ZonePosition: int64Ptr(1),
	}
}

func TestPopulateReplayFrameChangesIgnoresLimboCleanup(t *testing.T) {
	frames := []model.MatchReplayFrameRow{
		{Objects: []model.MatchReplayFrameObjectRow{
			replayChangeTestObject(300, 102639, 1, "self", "limbo", "public"),
		}},
		{Objects: []model.MatchReplayFrameObjectRow{}},
	}

	populateReplayFrameChanges(frames)

	if len(frames[1].Changes) != 0 {
		t.Fatalf("limbo cleanup changes = %#v, want none", frames[1].Changes)
	}
}

func TestPopulateReplayFrameChangesReconcilesReplacementInstanceID(t *testing.T) {
	previous := replayChangeTestObject(404, 68738, 1, "self", "hand", "private")
	current := replayChangeTestObject(410, 68738, 1, "self", "battlefield", "public")
	frames := []model.MatchReplayFrameRow{
		{Objects: []model.MatchReplayFrameObjectRow{previous}},
		{
			AnnotationsJSON: `{"annotations":[{"type":["AnnotationType_ObjectIdChanged"],"details":[{"key":"orig_id","valueInt32":[404]},{"key":"new_id","valueInt32":[410]}]}]}`,
			Objects:         []model.MatchReplayFrameObjectRow{current},
		},
	}

	populateReplayFrameChanges(frames)

	if len(frames[1].Changes) != 1 {
		t.Fatalf("replacement changes = %#v, want one move", frames[1].Changes)
	}
	change := frames[1].Changes[0]
	if change.Action != "move_public" || change.InstanceID != 410 || change.FromZoneType != "hand" || change.ToZoneType != "battlefield" {
		t.Fatalf("replacement change = %#v, want hand-to-battlefield move on current instance", change)
	}
}

func TestPopulateReplayFrameChangesKeepsRealVisibilityLoss(t *testing.T) {
	frames := []model.MatchReplayFrameRow{
		{Objects: []model.MatchReplayFrameObjectRow{
			replayChangeTestObject(501, 999, 2, "opponent", "hand", "public"),
		}},
		{Objects: []model.MatchReplayFrameObjectRow{}},
	}

	populateReplayFrameChanges(frames)

	if len(frames[1].Changes) != 1 || frames[1].Changes[0].Action != "leave_public" {
		t.Fatalf("visibility-loss changes = %#v, want one leave_public", frames[1].Changes)
	}
}
