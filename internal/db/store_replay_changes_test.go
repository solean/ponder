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

func replayClassTestObject(instanceID int64, zoneType string) model.MatchReplayFrameObjectRow {
	obj := replayChangeTestObject(instanceID, 91611, 1, "self", zoneType, "public")
	obj.DetailsJSON = `{"cardTypes":["CardType_Enchantment"],"subtypes":["SubType_Class"]}`
	return obj
}

func TestPopulateReplayClassLevelsCarriesLevelForward(t *testing.T) {
	levelUp := `{"persistentAnnotations":[{"affectorId":401,"affectedIds":[401],"type":["AnnotationType_ClassLevel"],"details":[{"key":"Level","type":"KeyValuePairValueType_int32","valueInt32":[2]}]}]}`
	frames := []model.MatchReplayFrameRow{
		{Objects: []model.MatchReplayFrameObjectRow{replayClassTestObject(401, "stack")}},
		{Objects: []model.MatchReplayFrameObjectRow{replayClassTestObject(401, "battlefield")}},
		{AnnotationsJSON: levelUp, Objects: []model.MatchReplayFrameObjectRow{replayClassTestObject(401, "battlefield")}},
		{Objects: []model.MatchReplayFrameObjectRow{replayClassTestObject(401, "battlefield")}},
		{Objects: []model.MatchReplayFrameObjectRow{replayClassTestObject(401, "graveyard")}},
		{Objects: []model.MatchReplayFrameObjectRow{replayClassTestObject(401, "battlefield")}},
	}

	populateReplayClassLevels(frames)

	want := []int64{0, 1, 2, 2, 0, 1}
	for i, level := range want {
		got := frames[i].Objects[0].ClassLevel
		if level == 0 {
			if got != nil {
				t.Fatalf("frame %d class level = %d, want none off the battlefield", i, *got)
			}
			continue
		}
		if got == nil || *got != level {
			t.Fatalf("frame %d class level = %v, want %d", i, got, level)
		}
	}

	populateReplayFrameChanges(frames)

	var levelChanges []model.MatchReplayChangeRow
	for _, frame := range frames {
		for _, change := range frame.Changes {
			if change.Action == "class_level_change" {
				levelChanges = append(levelChanges, change)
			}
		}
	}
	if len(levelChanges) != 1 || levelChanges[0].ClassLevel == nil || *levelChanges[0].ClassLevel != 2 {
		t.Fatalf("class level changes = %#v, want one change to level 2", levelChanges)
	}
}

func TestPopulateReplayClassLevelsIgnoresNonClassObjects(t *testing.T) {
	obj := replayChangeTestObject(77, 1234, 1, "self", "battlefield", "public")
	obj.DetailsJSON = `{"cardTypes":["CardType_Creature"],"subtypes":["SubType_Cleric"]}`
	frames := []model.MatchReplayFrameRow{{Objects: []model.MatchReplayFrameObjectRow{obj}}}

	populateReplayClassLevels(frames)

	if frames[0].Objects[0].ClassLevel != nil {
		t.Fatalf("non-Class class level = %d, want none", *frames[0].Objects[0].ClassLevel)
	}
}
