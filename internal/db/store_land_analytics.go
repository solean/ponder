package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// These metrics deliberately use explicit draw/land-play annotations rather
// than treating every new hand ID or battlefield arrival as a draw/land drop.
func deriveLandAnalytics(frames []model.MatchReplayFrameRow, playDraw string, types map[int64]bool) *model.GameLandAnalytics {
	out := &model.GameLandAnalytics{Turns: []model.GameLandTurn{}}
	type observedTurn struct {
		row                                             model.GameLandTurn
		start, complete, main, handKnown, landAvailable bool
		ownerObserved                                   bool
		ambiguousLandEntry                              bool
	}
	turns := map[int64]*observedTurn{}
	seenDraw := map[int64]bool{}
	seenLandPlay := map[int64]bool{}
	var selfSeat int64
	for _, frame := range frames {
		for _, object := range frame.Objects {
			if object.PlayerSide == "self" && object.OwnerSeatID != nil {
				selfSeat = *object.OwnerSeatID
				break
			}
		}
	}
	var previousTurn int64
	var previousFrame *model.MatchReplayFrameRow
	for index := range frames {
		frame := &frames[index]
		if !replayFrameIsPlay(*frame) || frame.TurnNumber == nil || *frame.TurnNumber <= 0 {
			previousFrame = frame
			continue
		}
		turn := *frame.TurnNumber
		observed := turns[turn]
		if observed == nil {
			observed = &observedTurn{row: model.GameLandTurn{TurnNumber: turn, LandDropStatus: "unobserved"}, handKnown: true}
			observed.start = strings.Contains(strings.ToLower(frame.Phase), "begin") &&
				((previousTurn == turn-1 && previousTurn > 0) || (turn == 1 && previousFrame != nil && !replayFrameIsPlay(*previousFrame)))
			turns[turn] = observed
		}
		// A gap, resync, or terminal snapshot cannot establish a completed turn.
		contiguous := !strings.Contains(strings.ToLower(frame.GameStage), "over") && previousFrame != nil && frame.PrevGameStateID != nil && previousFrame.GameStateID != nil && *frame.PrevGameStateID == *previousFrame.GameStateID
		if !contiguous {
			observed.start = false
		}
		if previousTurn > 0 && turn != previousTurn {
			if prior := turns[previousTurn]; prior != nil {
				prior.complete = turn == previousTurn+1 && contiguous
			}
		}
		if previousTurn == turn && !contiguous {
			observed.handKnown = false
		}
		isOwn := (playDraw == "play" && turn%2 == 1) || (playDraw == "draw" && turn%2 == 0)
		if !observed.ownerObserved && (playDraw == "play" || playDraw == "draw") {
			observed.row.IsPlayerTurn = pointerBool(isOwn)
		}
		objects := map[int64]model.MatchReplayFrameObjectRow{}
		for _, object := range frame.Objects {
			objects[object.InstanceID] = object
		}
		// Deduplicate repeated annotations only while the same physical entry
		// remains in its destination. A later draw/play after leaving counts again.
		for id := range seenDraw {
			object, ok := objects[id]
			if !ok || !strings.EqualFold(object.ZoneType, "hand") {
				delete(seenDraw, id)
			}
		}
		for id := range seenLandPlay {
			object, ok := objects[id]
			if !ok || !strings.EqualFold(object.ZoneType, "battlefield") {
				delete(seenLandPlay, id)
			}
		}
		// The first gameplay hand is a baseline, not evidence of draws. This
		// also prevents repeated opening-deal annotations from becoming draws.
		if previousTurn == 0 {
			for id, object := range objects {
				if object.PlayerSide == "self" && strings.EqualFold(object.ZoneType, "hand") {
					seenDraw[id] = true
				}
			}
		}
		var payload struct {
			Annotations []struct {
				Type        []string `json:"type"`
				AffectedIDs []int64  `json:"affectedIds"`
				Details     []struct {
					Key         string   `json:"key"`
					ValueString []string `json:"valueString"`
				} `json:"details"`
			} `json:"annotations"`
		}
		_ = json.Unmarshal([]byte(frame.AnnotationsJSON), &payload)
		for _, annotation := range payload.Annotations {
			transfer := false
			for _, kind := range annotation.Type {
				if kind == "AnnotationType_PhaseOrStepModified" && selfSeat > 0 && len(annotation.AffectedIDs) == 1 {
					observed.row.IsPlayerTurn = pointerBool(annotation.AffectedIDs[0] == selfSeat)
					observed.ownerObserved = true
				}
				if kind == "AnnotationType_ZoneTransfer" {
					transfer = true
				}
			}
			if !transfer {
				continue
			}
			category := ""
			for _, detail := range annotation.Details {
				if detail.Key == "category" && len(detail.ValueString) > 0 {
					category = detail.ValueString[0]
				}
			}
			for _, id := range annotation.AffectedIDs {
				object, exists := objects[id]
				if !exists || object.PlayerSide != "self" || object.IsToken {
					continue
				}
				if category == "Draw" && previousTurn > 0 && !seenDraw[id] && strings.EqualFold(object.ZoneType, "hand") {
					seenDraw[id] = true
					if land, known := types[object.CardID]; !known {
						observed.row.UnknownDrawn++
					} else if land {
						observed.row.LandsDrawn++
					} else {
						observed.row.SpellsDrawn++
					}
				}
				if category == "PlayLand" && !seenLandPlay[id] {
					seenLandPlay[id] = true
					observed.row.LandsPlayed++
				}
			}
		}
		if strings.Contains(strings.ToLower(frame.Phase), "main") {
			observed.main = true
			handObserved := false
			for _, object := range frame.Objects {
				if object.PlayerSide == "self" && strings.EqualFold(object.ZoneType, "hand") {
					handObserved = true
				}
			}
			// Archives do not retain explicit empty-hand membership metadata.
			// An empty own-hand list alone cannot prove a complete snapshot.
			if !handObserved {
				observed.handKnown = false
			}
			for _, object := range frame.Objects {
				if object.PlayerSide != "self" || !strings.EqualFold(object.ZoneType, "hand") {
					continue
				}
				if land, known := types[object.CardID]; !known {
					observed.handKnown = false
				} else if land {
					observed.landAvailable = true
				}
			}
			// A known land on the battlefield during this turn may have been played in
			// an unannotated snapshot; never call that a no-land miss.
			if previousFrame != nil {
				previous := map[int64]bool{}
				for _, object := range previousFrame.Objects {
					if object.PlayerSide == "self" && object.ZoneType == "battlefield" {
						previous[object.InstanceID] = true
					}
				}
				for _, object := range frame.Objects {
					if object.PlayerSide == "self" && object.ZoneType == "battlefield" && !previous[object.InstanceID] {
						if land, known := types[object.CardID]; !known || land {
							observed.ambiguousLandEntry = true
						}
					}
				}
			}
		}
		previousTurn, previousFrame = turn, frame
	}
	ordered := make([]int64, 0, len(turns))
	for turn := range turns {
		ordered = append(ordered, turn)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, turn := range ordered {
		observed := turns[turn]
		switch {
		case observed.row.IsPlayerTurn == nil:
			observed.row.LandDropStatus = "unobserved"
		case !*observed.row.IsPlayerTurn:
			observed.row.LandDropStatus = "opponent_turn"
		case !observed.start || !observed.complete:
			observed.row.LandDropStatus = "incomplete"
		case observed.row.LandsPlayed > 0:
			observed.row.LandDropStatus = "land_played"
		case !observed.ownerObserved || !observed.main || !observed.handKnown || observed.ambiguousLandEntry:
			observed.row.LandDropStatus = "unobserved"
		case observed.landAvailable:
			observed.row.LandDropStatus = "land_available"
		default:
			observed.row.LandDropStatus = "no_land_available"
		}
		out.Turns = append(out.Turns, observed.row)
	}
	return out
}

func (s *Store) attachMatchLandAnalytics(ctx context.Context, matchID int64, games []model.GameRow) error {
	frames, err := s.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		if strings.Contains(err.Error(), "decode match replay archive") {
			return nil
		}
		return err
	}
	ids := []int64{}
	byGame := map[int64][]model.MatchReplayFrameRow{}
	for _, frame := range frames {
		byGame[replayFrameGameNumber(frame)] = append(byGame[replayFrameGameNumber(frame)], frame)
		for _, object := range frame.Objects {
			if object.PlayerSide == "self" {
				ids = append(ids, object.CardID)
			}
		}
	}
	types, err := s.loadLandKnowledge(ctx, ids)
	if err != nil {
		return fmt.Errorf("load land analytics types: %w", err)
	}
	for index := range games {
		if gameFrames := byGame[games[index].GameNumber]; len(gameFrames) > 0 {
			games[index].LandAnalytics = deriveLandAnalytics(normalizeReplayGameFrames(gameFrames), games[index].PlayDraw, types)
		}
	}
	return nil
}
