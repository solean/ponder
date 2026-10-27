package model

import "testing"

func TestPopulateReplayCopyFlagsPreservesSparseEvidenceAndGameBoundaries(t *testing.T) {
	game1, game2 := int64(1), int64(2)
	frames := []MatchReplayFrameRow{
		{GameNumber: &game1, Objects: []MatchReplayFrameObjectRow{{InstanceID: 7}}},
		{GameNumber: &game1, AnnotationsJSON: `{"annotations":[{"type":["AnnotationType_ZoneTransfer"],"affectedIds":[7],"details":[{"key":"category","valueString":["CastSpell"]}]}]}`, Objects: []MatchReplayFrameObjectRow{{InstanceID: 7, DetailsJSON: `{"isCopy":true}`}}},
		{GameNumber: &game1, Objects: []MatchReplayFrameObjectRow{{InstanceID: 7, DetailsJSON: `{"instanceId":7}`}}},
		{GameNumber: &game2, Objects: []MatchReplayFrameObjectRow{{InstanceID: 7}}},
	}
	PopulateReplayCopyFlags(frames)
	for i := 0; i < 3; i++ {
		if !frames[i].Objects[0].IsCopy || !frames[i].Objects[0].CastConfirmed {
			t.Fatalf("frame %d lost copy/cast evidence: %+v", i, frames[i].Objects[0])
		}
	}
	if frames[3].Objects[0].IsCopy || frames[3].Objects[0].CastConfirmed {
		t.Fatal("copy identity leaked into another game")
	}
	if ReplayObjectIsCopy(`{"isCopy":false}`) || ReplayObjectIsCopy(`invalid`) {
		t.Fatal("missing or false identity must remain unknown/non-copy")
	}
}
