package db

import (
	"fmt"
	"github.com/solean/ponder/internal/model"
	"testing"
)

func landFrame(id, turn, owner int64, phase string, cards map[int64]int64, annotations string) model.MatchReplayFrameRow {
	frame := testShapeFrame(id, turn, 20, 20, cards)
	frame.PrevGameStateID = pointerInt64(id - 1)
	frame.Phase = phase
	for index := range frame.Objects {
		frame.Objects[index].OwnerSeatID = pointerInt64(1)
	}
	frame.AnnotationsJSON = fmt.Sprintf(`{"annotations":[{"type":["AnnotationType_PhaseOrStepModified"],"affectedIds":[%d]}%s]}`, owner, annotations)
	return frame
}
func transfer(category string, id int64) string {
	return fmt.Sprintf(`,{"type":["AnnotationType_ZoneTransfer"],"affectedIds":[%d],"details":[{"key":"category","valueString":[%q]}]}`, id, category)
}

func TestLandAnalyticsCountsOnlyExplicitGameplayDraws(t *testing.T) {
	frames := []model.MatchReplayFrameRow{
		landFrame(1, 1, 1, "beginning", map[int64]int64{1: 100}, transfer("Draw", 1)), // opening baseline
		landFrame(2, 1, 1, "main1", map[int64]int64{1: 100, 2: 101, 3: 102, 4: 103}, transfer("Draw", 2)+transfer("Draw", 3)+transfer("Draw", 4)),
		landFrame(3, 1, 1, "main2", map[int64]int64{1: 100, 2: 101, 3: 102, 4: 103, 5: 100}, transfer("Draw", 2)+transfer("Return", 5)),
	}
	got := deriveLandAnalytics(frames, "play", map[int64]bool{100: true, 101: true, 102: false}).Turns[0]
	if got.LandsDrawn != 1 || got.SpellsDrawn != 1 || got.UnknownDrawn != 1 {
		t.Fatalf("draws=%#v", got)
	}
	if got.LandDropStatus != "incomplete" {
		t.Fatalf("partial final turn=%s", got.LandDropStatus)
	}
}

func TestLandAnalyticsRequiresCompleteObservedOwnMainPhase(t *testing.T) {
	baseline := func() []model.MatchReplayFrameRow {
		return []model.MatchReplayFrameRow{
			landFrame(1, 1, 1, "ending", map[int64]int64{1: 102}, ""),
			// Explicit owner says self on even turn: extra turns must override parity.
			landFrame(2, 2, 1, "beginning", map[int64]int64{1: 102}, ""),
			landFrame(3, 2, 1, "main1", map[int64]int64{1: 102}, ""),
			landFrame(4, 3, 2, "beginning", map[int64]int64{1: 102}, ""),
		}
	}
	tests := []struct {
		name, want string
		change     func([]model.MatchReplayFrameRow)
	}{
		{"no land", "no_land_available", func([]model.MatchReplayFrameRow) {}},
		{"held land", "land_available", func(f []model.MatchReplayFrameRow) { f[2].Objects[0].CardID = 100 }},
		{"unknown type", "unobserved", func(f []model.MatchReplayFrameRow) { f[2].Objects[0].CardID = 999 }},
		{"recording gap", "incomplete", func(f []model.MatchReplayFrameRow) { f[2].PrevGameStateID = pointerInt64(99) }},
		{"missing start", "incomplete", func(f []model.MatchReplayFrameRow) { f[1].Phase = "main1" }},
		{"gap at end", "incomplete", func(f []model.MatchReplayFrameRow) { f[3].PrevGameStateID = pointerInt64(99) }},
		{"terminal snapshot", "incomplete", func(f []model.MatchReplayFrameRow) { f[3].GameStage = "GameStage_GameOver" }},
		{"not own turn", "opponent_turn", func(f []model.MatchReplayFrameRow) {
			f[1].AnnotationsJSON = `{"annotations":[]}`
			f[2].AnnotationsJSON = `{"annotations":[]}`
		}},
		{"unknown ownership", "unobserved", func(f []model.MatchReplayFrameRow) {
			f[1].AnnotationsJSON = `{"annotations":[]}`
			f[2].AnnotationsJSON = `{"annotations":[]}`
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := baseline()
			tt.change(frames)
			playDraw := "play"
			if tt.name == "unknown ownership" {
				playDraw = ""
			}
			got := deriveLandAnalytics(frames, playDraw, map[int64]bool{100: true, 102: false}).Turns[1]
			if got.LandDropStatus != tt.want {
				t.Fatalf("status=%s want %s", got.LandDropStatus, tt.want)
			}
		})
	}
}

func TestLandAnalyticsDoesNotMistakeLandPlayedForMiss(t *testing.T) {
	frames := []model.MatchReplayFrameRow{landFrame(1, 1, 1, "ending", map[int64]int64{1: 102}, ""), landFrame(2, 2, 1, "beginning", map[int64]int64{1: 102}, ""), landFrame(3, 2, 1, "main1", map[int64]int64{1: 102}, transfer("PlayLand", 9)), landFrame(4, 3, 2, "beginning", map[int64]int64{1: 102}, "")}
	frames[2].Objects = append(frames[2].Objects, model.MatchReplayFrameObjectRow{InstanceID: 9, CardID: 100, PlayerSide: "self", ZoneType: "battlefield", OwnerSeatID: pointerInt64(1)})
	got := deriveLandAnalytics(frames, "play", map[int64]bool{100: true, 102: false}).Turns[1]
	if got.LandsPlayed != 1 || got.LandDropStatus != "land_played" {
		t.Fatalf("land play=%#v", got)
	}
	frames[2].AnnotationsJSON = `{"annotations":[{"type":["AnnotationType_PhaseOrStepModified"],"affectedIds":[1]}]}`
	uncertain := deriveLandAnalytics(frames, "play", map[int64]bool{100: true, 102: false}).Turns[1]
	if uncertain.LandDropStatus != "unobserved" {
		t.Fatalf("unannotated battlefield entry=%#v", uncertain)
	}
}

func TestLandAnalyticsCountsSameIDAfterLeavingDestination(t *testing.T) {
	frames := []model.MatchReplayFrameRow{
		landFrame(1, 1, 1, "beginning", map[int64]int64{1: 102}, ""),
		landFrame(2, 1, 1, "main1", map[int64]int64{1: 102, 9: 100}, transfer("Draw", 9)),
		landFrame(3, 1, 1, "main1", map[int64]int64{1: 102, 9: 100}, transfer("Draw", 9)), // repeated event
		landFrame(4, 1, 1, "main1", map[int64]int64{1: 102}, ""),                          // returned to library
		landFrame(5, 1, 1, "main2", map[int64]int64{1: 102, 9: 100}, transfer("Draw", 9)),
		landFrame(6, 1, 1, "main2", map[int64]int64{1: 102}, transfer("PlayLand", 9)),
		landFrame(7, 2, 2, "beginning", map[int64]int64{1: 102}, ""), // bounced off battlefield
		landFrame(8, 3, 1, "beginning", map[int64]int64{1: 102}, ""),
		landFrame(9, 3, 1, "main1", map[int64]int64{1: 102}, transfer("PlayLand", 9)),
	}
	land := model.MatchReplayFrameObjectRow{InstanceID: 9, CardID: 100, PlayerSide: "self", ZoneType: "battlefield", OwnerSeatID: pointerInt64(1)}
	frames[5].Objects = append(frames[5].Objects, land)
	frames[8].Objects = append(frames[8].Objects, land)
	got := deriveLandAnalytics(frames, "play", map[int64]bool{100: true, 102: false}).Turns
	if got[0].LandsDrawn != 2 || got[0].LandsPlayed != 1 || got[2].LandsPlayed != 1 {
		t.Fatalf("reentries=%#v", got)
	}
}

func TestLandAnalyticsEmptyHandWithoutMembershipEvidenceIsUnknown(t *testing.T) {
	frames := []model.MatchReplayFrameRow{landFrame(1, 1, 1, "ending", map[int64]int64{1: 102}, ""), landFrame(2, 2, 1, "beginning", map[int64]int64{1: 102}, ""), landFrame(3, 2, 1, "main1", nil, ""), landFrame(4, 3, 2, "beginning", map[int64]int64{1: 102}, "")}
	frames[2].Objects = []model.MatchReplayFrameObjectRow{{InstanceID: 99, CardID: 100, PlayerSide: "opponent", ZoneType: "battlefield"}}
	got := deriveLandAnalytics(frames, "play", map[int64]bool{100: true, 102: false}).Turns[1]
	if got.LandDropStatus != "unobserved" {
		t.Fatalf("empty snapshot=%#v", got)
	}
}

func TestLandAnalyticsExcludesMulliganAndInheritedSetupTurns(t *testing.T) {
	frames := []model.MatchReplayFrameRow{
		landFrame(1, 11, 1, "main1", map[int64]int64{7: 100}, transfer("Draw", 7)),
		landFrame(2, 11, 1, "main1", map[int64]int64{8: 102}, transfer("Draw", 8)),
		landFrame(3, 1, 1, "beginning", map[int64]int64{9: 102}, transfer("Draw", 9)),
		landFrame(4, 1, 1, "main1", map[int64]int64{9: 102, 10: 100}, transfer("Draw", 10)),
	}
	frames[0].GameStage = "GameStage_Mulligan"
	frames[1].GameStage = "GameStage_Mulligan"
	got := deriveLandAnalytics(normalizeReplayGameFrames(frames), "play", map[int64]bool{100: true, 102: false}).Turns
	if len(got) != 1 || got[0].LandsDrawn != 1 || got[0].SpellsDrawn != 0 {
		t.Fatalf("setup draws=%#v", got)
	}
}
