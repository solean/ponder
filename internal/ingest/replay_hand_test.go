package ingest

import (
	"github.com/solean/ponder/internal/model"
	"testing"
)

func TestReplayKnownHandReturnsRequirePublicSourceAndExplicitIDLink(t *testing.T) {
	state := &parseState{}
	state.rememberZoneType("match", 35, "hand")
	payload := []byte(`[{"type":["AnnotationType_ObjectIdChanged"],"details":[{"key":"orig_id","valueInt32":[101]},{"key":"new_id","valueInt32":[201]}]}]`)
	for _, zone := range []string{"battlefield", "hand", "library", "limbo"} {
		replay := &replayPublicState{Objects: map[int64]replayCardState{201: {InstanceID: 201, ZoneID: 35, Visibility: "private"}}}
		previous := map[int64]model.MatchReplayFrameObjectRow{101: {InstanceID: 101, CardID: 999, ZoneType: zone, Visibility: "public"}}
		applyReplayKnownHandReturns(replay, state, "match", previous, payload)
		if got := replay.Objects[201].CardID; (got == 999) != (zone == "battlefield") {
			t.Fatalf("source %s identity=%d", zone, got)
		}
	}
}
