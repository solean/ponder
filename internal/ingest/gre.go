package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

type replayCardState struct {
	InstanceID           int64
	CardID               int64
	OwnerSeatID          int64
	ControllerSeatID     int64
	ZoneID               int64
	Visibility           string
	Power                *int64
	Toughness            *int64
	AttackTargetID       int64
	BlockAttackerIDsJSON string
	CounterSummaryJSON   string
	DetailsJSON          string
	AttackState          string
	BlockState           string
	IsToken              bool
	IsTapped             bool
	HasSummoningSickness bool
	SummoningSickTurn    int64
	SummoningSickSeatID  int64
}

type replayPublicState struct {
	LastGameStateID   int64
	Objects           map[int64]replayCardState
	PublicZoneMembers map[int64][]int64
	PublicZoneTypes   map[int64]string
	PlayerLifeTotals  map[int64]int64
}

func newReplayPublicState() *replayPublicState {
	return &replayPublicState{
		Objects:           make(map[int64]replayCardState),
		PublicZoneMembers: make(map[int64][]int64),
		PublicZoneTypes:   make(map[int64]string),
		PlayerLifeTotals:  make(map[int64]int64),
	}
}

func clearExpiredReplaySummoningSickness(replay *replayPublicState, turnNumber, activePlayer int64) {
	if replay == nil || turnNumber <= 0 || activePlayer <= 0 {
		return
	}

	for instanceID, current := range replay.Objects {
		if !current.HasSummoningSickness {
			continue
		}
		controllerSeatID := current.SummoningSickSeatID
		if controllerSeatID <= 0 {
			controllerSeatID = current.ControllerSeatID
		}
		if controllerSeatID <= 0 || controllerSeatID != activePlayer {
			continue
		}
		if current.SummoningSickTurn <= 0 || turnNumber <= current.SummoningSickTurn {
			continue
		}

		current.HasSummoningSickness = false
		current.SummoningSickTurn = 0
		current.SummoningSickSeatID = 0
		replay.Objects[instanceID] = current
	}
}

func replayStateKey(matchID string, gameNumber int64) string {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return ""
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}
	return fmt.Sprintf("%s:%d", matchID, gameNumber)
}

func (s *parseState) replayState(matchID string, gameNumber int64) *replayPublicState {
	key := replayStateKey(matchID, gameNumber)
	if key == "" || s.replayByMatchGame == nil {
		return nil
	}
	return s.replayByMatchGame[key]
}

func (s *parseState) rememberReplayState(matchID string, gameNumber int64, replay *replayPublicState) {
	key := replayStateKey(matchID, gameNumber)
	if key == "" || replay == nil {
		return
	}
	if s.replayByMatchGame == nil {
		s.replayByMatchGame = make(map[string]*replayPublicState)
	}
	s.replayByMatchGame[key] = replay
}

type greEnvelope struct {
	Timestamp        string `json:"timestamp"`
	GREToClientEvent *struct {
		Messages []greMessage `json:"greToClientMessages"`
	} `json:"greToClientEvent"`
}

type greMessage struct {
	SystemSeatIDs    []int64          `json:"systemSeatIds"`
	GameStateMessage *greGameStateMsg `json:"gameStateMessage"`
	ConnectResp      *struct {
		DeckMessage greDeck `json:"deckMessage"`
	} `json:"connectResp"`
}

type greDeck struct {
	DeckCards      []int64 `json:"deckCards"`
	SideboardCards []int64 `json:"sideboardCards"`
}

type greGameStateMsg struct {
	Type            string `json:"type"`
	GameStateID     int64  `json:"gameStateId"`
	PrevGameStateID int64  `json:"prevGameStateId"`
	GameInfo        *struct {
		MatchID    string            `json:"matchID"`
		GameNumber int64             `json:"gameNumber"`
		Stage      string            `json:"stage"`
		Results    []roomResultEntry `json:"results"`
	} `json:"gameInfo"`
	TurnInfo               *greTurnInfo    `json:"turnInfo"`
	Players                []grePlayer     `json:"players"`
	Zones                  []greZone       `json:"zones"`
	GameObjects            []greGameObject `json:"gameObjects"`
	DiffDeletedInstanceIDs []int64         `json:"diffDeletedInstanceIds"`
	Actions                json.RawMessage `json:"actions"`
	Annotations            json.RawMessage `json:"annotations"`
	PersistentAnnotations  json.RawMessage `json:"persistentAnnotations"`
}

type greTurnInfo struct {
	TurnNumber   int64  `json:"turnNumber"`
	Phase        string `json:"phase"`
	Step         string `json:"step"`
	ActivePlayer int64  `json:"activePlayer"`
}

type grePlayer struct {
	LifeTotal        int64 `json:"lifeTotal"`
	SystemSeatNumber int64 `json:"systemSeatNumber"`
	TeamID           int64 `json:"teamId"`
}

type greZone struct {
	ZoneID            int64   `json:"zoneId"`
	Type              string  `json:"type"`
	Visibility        string  `json:"visibility"`
	OwnerSeatID       int64   `json:"ownerSeatId"`
	ObjectInstanceIDs []int64 `json:"objectInstanceIds"`
}

type greObjectValue struct {
	Value int64 `json:"value"`
}

type greAttackInfo struct {
	TargetID int64 `json:"targetId"`
}

type greBlockInfo struct {
	AttackerIDs []int64 `json:"attackerIds"`
}

type greGameObject struct {
	Raw                  json.RawMessage `json:"-"`
	InstanceID           int64           `json:"instanceId"`
	GrpID                int64           `json:"grpId"`
	OverlayGrpID         int64           `json:"overlayGrpId"`
	Type                 string          `json:"type"`
	ZoneID               int64           `json:"zoneId"`
	Visibility           string          `json:"visibility"`
	OwnerSeatID          int64           `json:"ownerSeatId"`
	ControllerSeatID     *int64          `json:"controllerSeatId"`
	Power                *greObjectValue `json:"power"`
	Toughness            *greObjectValue `json:"toughness"`
	IsTapped             *bool           `json:"isTapped"`
	HasSummoningSickness *bool           `json:"hasSummoningSickness"`
	AttackState          *string         `json:"attackState"`
	AttackInfo           *greAttackInfo  `json:"attackInfo"`
	BlockState           *string         `json:"blockState"`
	BlockInfo            *greBlockInfo   `json:"blockInfo"`
	IsToken              bool            `json:"isToken"`
}

func (o *greGameObject) UnmarshalJSON(data []byte) error {
	type greGameObjectAlias greGameObject

	var alias greGameObjectAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	*o = greGameObject(alias)
	o.Raw = append(o.Raw[:0], data...)
	return nil
}

func isReplayTrackableObjectType(objectType string) bool {
	objectType = strings.TrimSpace(objectType)
	return strings.EqualFold(objectType, "GameObjectType_Card") ||
		strings.EqualFold(objectType, "GameObjectType_Token")
}

type greAnnotation struct {
	ID          int64                 `json:"id"`
	AffectorID  int64                 `json:"affectorId"`
	AffectedIDs []int64               `json:"affectedIds"`
	Type        []string              `json:"type"`
	Details     []greAnnotationDetail `json:"details"`
}

type greAnnotationDetail struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	ValueInt32  []int64  `json:"valueInt32"`
	ValueString []string `json:"valueString"`
}

func normalizeGREPhase(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "Phase_")
	raw = strings.TrimPrefix(raw, "Step_")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.ToLower(raw)
}

func normalizeGREGameStateType(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "GameStateType_")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.ToLower(raw)
}

func normalizeGREGameStage(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "GameStage_")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.ToLower(raw)
}

func replayWinningPlayerSide(players []grePlayer, selfSeat, winningTeamID int64) string {
	if selfSeat <= 0 || winningTeamID <= 0 {
		return "unknown"
	}

	var selfTeamID int64
	for _, player := range players {
		if player.SystemSeatNumber == selfSeat && player.TeamID > 0 {
			selfTeamID = player.TeamID
			break
		}
	}
	if selfTeamID <= 0 {
		return "unknown"
	}
	if selfTeamID == winningTeamID {
		return "self"
	}
	return "opponent"
}

func normalizeGREZoneType(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "ZoneType_")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.ToLower(raw)
}

func normalizeGREVisibility(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "Visibility_")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.ToLower(raw)
}

func normalizeGREAttackState(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "AttackState_")
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "None") {
		return ""
	}
	return strings.ToLower(raw)
}

func normalizeGREBlockState(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "BlockState_")
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "None") {
		return ""
	}
	return strings.ToLower(raw)
}

func isPublicGREVisibility(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(normalizeGREVisibility(raw)), "public")
}

func replayZoneVisibility(matchID string, zoneID int64, state *parseState) string {
	if state == nil || zoneID <= 0 {
		return ""
	}
	return normalizeGREVisibility(state.zoneVisibility(matchID, zoneID))
}

func isReplaySelfHandZone(matchID string, zoneID, selfSeat int64, state *parseState) bool {
	if state == nil || zoneID <= 0 || selfSeat <= 0 {
		return false
	}
	if strings.TrimSpace(state.zoneType(matchID, zoneID)) != "hand" {
		return false
	}
	return state.zoneOwnerSeat(matchID, zoneID) == selfSeat
}

func isTimelinePlayableZone(zoneType string) bool {
	zoneType = strings.TrimSpace(strings.ToLower(zoneType))
	return zoneType == "stack" || zoneType == "battlefield"
}

func fallbackGREZoneType(zoneID int64) string {
	switch zoneID {
	case 27:
		return "stack"
	case 28:
		return "battlefield"
	default:
		return ""
	}
}

func (p *Parser) handleGREJSON(ctx context.Context, tx *sql.Tx, line string, state *parseState) error {
	var env greEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return nil
	}
	if env.GREToClientEvent == nil {
		return nil
	}

	eventTS := parseRoomTimestamp(env.Timestamp)
	var connectDeck *greDeck
	for _, msg := range env.GREToClientEvent.Messages {
		if msg.ConnectResp != nil && len(msg.ConnectResp.DeckMessage.DeckCards) > 0 {
			deck := msg.ConnectResp.DeckMessage
			connectDeck = &greDeck{
				DeckCards:      append([]int64(nil), deck.DeckCards...),
				SideboardCards: append([]int64(nil), deck.SideboardCards...),
			}
		}
		if msg.GameStateMessage == nil {
			continue
		}

		matchID := strings.TrimSpace(state.activeMatchID)
		if msg.GameStateMessage.GameInfo != nil {
			if strings.TrimSpace(msg.GameStateMessage.GameInfo.MatchID) != "" {
				matchID = strings.TrimSpace(msg.GameStateMessage.GameInfo.MatchID)
			}
			if matchID != "" && msg.GameStateMessage.GameInfo.GameNumber > 0 {
				state.rememberGameNumber(matchID, msg.GameStateMessage.GameInfo.GameNumber)
			}
		}
		if msg.GameStateMessage.GameInfo != nil && strings.TrimSpace(msg.GameStateMessage.GameInfo.MatchID) != "" {
			selfSeat := state.selfSeat(matchID)
			if selfSeat <= 0 && len(msg.SystemSeatIDs) == 1 && msg.SystemSeatIDs[0] > 0 {
				selfSeat = msg.SystemSeatIDs[0]
			}
			if _, err := p.store.UpsertMatchStart(ctx, tx, matchID, "", selfSeat, eventTS); err != nil {
				return err
			}
			state.activateMatch(matchID)
			state.rememberSelfSeat(matchID, selfSeat)
		}
		if matchID == "" {
			continue
		}
		// ConnectResp does not identify its match directly. Bind it to the
		// first game state in the same GRE envelope, after GameInfo has supplied
		// the exact match ID. This also supports parsing a log that begins at the
		// connect response before any room-state record was retained.
		if connectDeck != nil {
			state.rememberPendingGameDeckSnapshot(*connectDeck, eventTS, "gre_connect", matchID, 0)
			connectDeck = nil
		}

		if msg.GameStateMessage.TurnInfo != nil {
			state.rememberTurn(matchID, msg.GameStateMessage.TurnInfo.TurnNumber)
			state.rememberActivePlayer(matchID, msg.GameStateMessage.TurnInfo.ActivePlayer)
			phaseValue := msg.GameStateMessage.TurnInfo.Phase
			if strings.TrimSpace(phaseValue) == "" {
				phaseValue = msg.GameStateMessage.TurnInfo.Step
			}
			state.rememberPhase(matchID, phaseValue)
		}
		for _, zone := range msg.GameStateMessage.Zones {
			state.rememberZoneType(matchID, zone.ZoneID, zone.Type)
			state.rememberZoneVisibility(matchID, zone.ZoneID, zone.Visibility)
			state.rememberZoneOwnerSeat(matchID, zone.ZoneID, zone.OwnerSeatID)
		}

		selfSeat := state.selfSeat(matchID)
		if selfSeat <= 0 && len(msg.SystemSeatIDs) == 1 && msg.SystemSeatIDs[0] > 0 {
			selfSeat = msg.SystemSeatIDs[0]
			state.rememberSelfSeat(matchID, selfSeat)
		}
		turnNumber := state.turn(matchID)
		activePlayer := state.activePlayer(matchID)
		phase := state.phase(matchID)
		gameNumber := state.gameNumber(matchID)
		if gameNumber <= 0 {
			gameNumber = 1
		}

		// ConnectResp carries game 1's submitted list, and SubmitDeckResp
		// carries the next submitted list. Wait for the following full state so
		// the snapshot is paired with Arena's exact match and game number.
		if state.pendingGameDeckMatches(matchID, gameNumber) &&
			normalizeGREGameStateType(msg.GameStateMessage.Type) == "full" &&
			msg.GameStateMessage.GameInfo != nil &&
			msg.GameStateMessage.GameInfo.GameNumber > 0 {
			pending := state.pendingGameDeckSnapshot
			observedAt := pending.ObservedAt
			if strings.TrimSpace(observedAt) == "" {
				observedAt = eventTS
			}
			if err := p.store.ReplaceMatchGameDeckSnapshot(
				ctx,
				tx,
				matchID,
				gameNumber,
				observedAt,
				pending.Source,
				pending.MainCardIDs,
				pending.SideboardCardIDs,
			); err != nil {
				return err
			}
			state.pendingGameDeckSnapshot = nil
		}

		replayState, err := p.replayStateForGame(ctx, tx, state, matchID, gameNumber, msg.GameStateMessage.Type)
		if err != nil {
			return err
		}

		for _, player := range msg.GameStateMessage.Players {
			if player.SystemSeatNumber <= 0 {
				continue
			}
			replayState.PlayerLifeTotals[player.SystemSeatNumber] = player.LifeTotal
		}
		clearExpiredReplaySummoningSickness(replayState, turnNumber, activePlayer)

		_, previousPublicByInstance := buildReplayPublicSnapshot(matchID, replayState, state, selfSeat)
		if phase != "combat" {
			clearReplayCombatState(replayState)
		}

		for _, instanceID := range msg.GameStateMessage.DiffDeletedInstanceIDs {
			delete(replayState.Objects, instanceID)
			for zoneID, members := range replayState.PublicZoneMembers {
				replayState.PublicZoneMembers[zoneID] = removeReplayInstance(members, instanceID)
			}
		}

		for _, obj := range msg.GameStateMessage.GameObjects {
			if obj.InstanceID <= 0 {
				continue
			}
			if !isReplayTrackableObjectType(obj.Type) {
				continue
			}

			current := replayState.Objects[obj.InstanceID]
			previousControllerSeatID := current.ControllerSeatID
			current.InstanceID = obj.InstanceID
			cardID := obj.GrpID
			if cardID <= 0 && obj.OverlayGrpID > 0 {
				cardID = obj.OverlayGrpID
			}
			if cardID > 0 {
				current.CardID = cardID
			}
			if obj.OwnerSeatID > 0 {
				current.OwnerSeatID = obj.OwnerSeatID
			}
			if obj.ControllerSeatID != nil && *obj.ControllerSeatID > 0 {
				current.ControllerSeatID = *obj.ControllerSeatID
			}
			if obj.ZoneID > 0 {
				current.ZoneID = obj.ZoneID
			}
			if visibility := normalizeGREVisibility(obj.Visibility); visibility != "" {
				current.Visibility = visibility
			}
			if obj.Power != nil {
				current.Power = replayIntPtr(obj.Power.Value)
			}
			if obj.Toughness != nil {
				current.Toughness = replayIntPtr(obj.Toughness.Value)
			}
			if obj.IsTapped != nil {
				current.IsTapped = *obj.IsTapped
			}
			if obj.HasSummoningSickness != nil {
				current.HasSummoningSickness = *obj.HasSummoningSickness
				if current.HasSummoningSickness {
					current.SummoningSickTurn = turnNumber
					current.SummoningSickSeatID = current.ControllerSeatID
				} else {
					current.SummoningSickTurn = 0
					current.SummoningSickSeatID = 0
				}
			} else if current.HasSummoningSickness &&
				previousControllerSeatID > 0 &&
				current.ControllerSeatID > 0 &&
				current.ControllerSeatID != previousControllerSeatID &&
				turnNumber > 0 {
				current.SummoningSickTurn = turnNumber
				current.SummoningSickSeatID = current.ControllerSeatID
			}
			if obj.AttackState != nil {
				current.AttackState = normalizeGREAttackState(*obj.AttackState)
				if current.AttackState == "" {
					current.AttackTargetID = 0
				}
			}
			if obj.AttackInfo != nil {
				if obj.AttackInfo.TargetID > 0 {
					current.AttackTargetID = obj.AttackInfo.TargetID
				} else if current.AttackState == "" {
					current.AttackTargetID = 0
				}
			}
			if obj.BlockState != nil {
				current.BlockState = normalizeGREBlockState(*obj.BlockState)
				if current.BlockState == "" {
					current.BlockAttackerIDsJSON = ""
				}
			}
			if obj.BlockInfo != nil {
				current.BlockAttackerIDsJSON = encodeReplayIntSliceJSON(obj.BlockInfo.AttackerIDs)
				if current.BlockState == "" && len(obj.BlockInfo.AttackerIDs) == 0 {
					current.BlockAttackerIDsJSON = ""
				}
			}
			if obj.IsToken || strings.EqualFold(strings.TrimSpace(obj.Type), "GameObjectType_Token") {
				current.IsToken = true
			}
			if detailsJSON := strings.TrimSpace(string(obj.Raw)); detailsJSON != "" {
				current.DetailsJSON = detailsJSON
				current.CounterSummaryJSON = extractReplayCounterSummaryJSON(obj.Raw)
			}
			replayState.Objects[obj.InstanceID] = current

			if current.ZoneID > 0 && isReplayTrackedZone(matchID, current.ZoneID, replayState, state, selfSeat) {
				zoneType := state.zoneType(matchID, current.ZoneID)
				if zoneType == "" {
					zoneType = fallbackGREZoneType(current.ZoneID)
				}
				if current.Visibility == "" {
					current.Visibility = replayZoneVisibility(matchID, current.ZoneID, state)
				}
				if !isReplayVisibleObject(matchID, current, zoneType, state, selfSeat) {
					removeReplayInstanceFromAllZones(replayState, obj.InstanceID)
				} else {
					removeReplayInstanceFromOtherZones(replayState, current.ZoneID, obj.InstanceID)
					replayState.PublicZoneMembers[current.ZoneID] = appendReplayInstance(replayState.PublicZoneMembers[current.ZoneID], obj.InstanceID)
					if zoneType != "" {
						replayState.PublicZoneTypes[current.ZoneID] = zoneType
					}
					replayState.Objects[obj.InstanceID] = current
				}
			}
		}
		applyReplayAnnotations(replayState, msg.GameStateMessage.Annotations)

		for _, zone := range msg.GameStateMessage.Zones {
			if !isReplayTrackedZone(matchID, zone.ZoneID, replayState, state, selfSeat) || zone.ObjectInstanceIDs == nil {
				continue
			}

			zoneType := state.zoneType(matchID, zone.ZoneID)
			if zoneType == "" {
				zoneType = fallbackGREZoneType(zone.ZoneID)
			}
			if zoneType != "" {
				replayState.PublicZoneTypes[zone.ZoneID] = zoneType
			}

			members := make([]int64, 0, len(zone.ObjectInstanceIDs))
			for _, instanceID := range zone.ObjectInstanceIDs {
				current := replayState.Objects[instanceID]
				current.InstanceID = instanceID
				current.ZoneID = zone.ZoneID
				if current.Visibility == "" {
					current.Visibility = replayZoneVisibility(matchID, zone.ZoneID, state)
				}
				replayState.Objects[instanceID] = current
				if !isReplayVisibleObject(matchID, current, zoneType, state, selfSeat) {
					removeReplayInstanceFromAllZones(replayState, instanceID)
					continue
				}
				removeReplayInstanceFromOtherZones(replayState, zone.ZoneID, instanceID)
				members = append(members, instanceID)
			}
			replayState.PublicZoneMembers[zone.ZoneID] = members
		}

		replayState.LastGameStateID = msg.GameStateMessage.GameStateID
		snapshotObjects, currentPublicByInstance := buildReplayPublicSnapshot(matchID, replayState, state, selfSeat)
		gameStage := ""
		var winningTeamID int64
		gameWinReason := ""
		if msg.GameStateMessage.GameInfo != nil {
			gameStage = normalizeGREGameStage(msg.GameStateMessage.GameInfo.Stage)
			winningTeamID, gameWinReason = chooseGameResult(msg.GameStateMessage.GameInfo.Results)
		}
		winningPlayerSide := replayWinningPlayerSide(msg.GameStateMessage.Players, selfSeat, winningTeamID)
		if _, err := p.store.ReplaceMatchReplayFrame(
			ctx,
			tx,
			matchID,
			gameNumber,
			msg.GameStateMessage.GameStateID,
			msg.GameStateMessage.PrevGameStateID,
			turnNumber,
			normalizeGREGameStateType(msg.GameStateMessage.Type),
			gameStage,
			phase,
			winningPlayerSide,
			gameWinReason,
			eventTS,
			"gre_public_replay",
			encodeReplayPlayerLifeTotals(replayState.PlayerLifeTotals),
			msg.GameStateMessage.Actions,
			combineGREAnnotationPayload(msg.GameStateMessage),
			snapshotObjects,
		); err != nil {
			return err
		}

		for instanceID, current := range currentPublicByInstance {
			if _, alreadyPublic := previousPublicByInstance[instanceID]; alreadyPublic {
				continue
			}
			ownerSeatID := int64(0)
			if current.OwnerSeatID != nil {
				ownerSeatID = *current.OwnerSeatID
			}

			if !current.IsToken && ownerSeatID > 0 && isTimelinePlayableZone(current.ZoneType) {
				if err := p.store.UpsertMatchCardPlay(ctx, tx, matchID, gameNumber, current.InstanceID, current.CardID, ownerSeatID, turnNumber, phase, current.ZoneType, eventTS, "gre_public_replay"); err != nil {
					return err
				}
			}

			if selfSeat <= 0 || current.IsToken || ownerSeatID <= 0 || ownerSeatID == selfSeat {
				continue
			}
			if err := p.store.UpsertMatchOpponentCardInstance(ctx, tx, matchID, gameNumber, current.InstanceID, current.CardID, eventTS, "gre_public_replay"); err != nil {
				return err
			}
		}
	}
	if connectDeck != nil {
		// Some reconnect payloads place the full state in a later envelope. A
		// known active match still provides an exact boundary; otherwise discard
		// the unbound deck rather than risking cross-match attribution.
		state.rememberPendingGameDeckSnapshot(*connectDeck, eventTS, "gre_connect", state.activeMatchID, 0)
	}

	return nil
}

func (p *Parser) replayStateForGame(
	ctx context.Context,
	tx *sql.Tx,
	state *parseState,
	matchID string,
	gameNumber int64,
	gameStateType string,
) (*replayPublicState, error) {
	gameStateType = normalizeGREGameStateType(gameStateType)
	if gameStateType == "full" {
		replay := newReplayPublicState()
		state.rememberReplayState(matchID, gameNumber, replay)
		return replay, nil
	}

	if replay := state.replayState(matchID, gameNumber); replay != nil {
		return replay, nil
	}

	replay := newReplayPublicState()
	lastGameStateID, latestTurnNumber, objects, playerLifeTotals, err := p.store.LoadLatestMatchReplayFrameState(ctx, tx, matchID, gameNumber)
	if err != nil {
		return nil, err
	}
	replay.LastGameStateID = lastGameStateID
	hydrateReplayStateFromFrameObjects(replay, latestTurnNumber, objects, playerLifeTotals)
	state.rememberReplayState(matchID, gameNumber, replay)
	return replay, nil
}

func hydrateReplayStateFromFrameObjects(replay *replayPublicState, latestTurnNumber int64, objects []model.MatchReplayFrameObjectRow, playerLifeTotals map[int64]int64) {
	if replay == nil {
		return
	}
	for seatID, lifeTotal := range playerLifeTotals {
		if seatID <= 0 {
			continue
		}
		replay.PlayerLifeTotals[seatID] = lifeTotal
	}
	for _, obj := range objects {
		if obj.InstanceID <= 0 || obj.CardID <= 0 {
			continue
		}

		zoneID := int64(0)
		if obj.ZoneID != nil {
			zoneID = *obj.ZoneID
		}

		current := replayCardState{
			InstanceID:           obj.InstanceID,
			CardID:               obj.CardID,
			OwnerSeatID:          replayIntValue(obj.OwnerSeatID),
			ControllerSeatID:     replayIntValue(obj.ControllerSeatID),
			ZoneID:               zoneID,
			Visibility:           normalizeGREVisibility(obj.Visibility),
			Power:                copyReplayIntPtr(obj.Power),
			Toughness:            copyReplayIntPtr(obj.Toughness),
			AttackTargetID:       replayIntValue(obj.AttackTargetID),
			BlockAttackerIDsJSON: strings.TrimSpace(obj.BlockAttackerIDsJSON),
			CounterSummaryJSON:   strings.TrimSpace(obj.CounterSummaryJSON),
			DetailsJSON:          strings.TrimSpace(obj.DetailsJSON),
			AttackState:          strings.TrimSpace(obj.AttackState),
			BlockState:           strings.TrimSpace(obj.BlockState),
			IsToken:              obj.IsToken,
			IsTapped:             obj.IsTapped,
			HasSummoningSickness: obj.HasSummoningSickness,
		}
		if obj.HasSummoningSickness {
			current.SummoningSickTurn = latestTurnNumber
			current.SummoningSickSeatID = replayIntValue(obj.ControllerSeatID)
		}
		replay.Objects[obj.InstanceID] = current
		if zoneID <= 0 {
			continue
		}
		replay.PublicZoneMembers[zoneID] = appendReplayInstance(replay.PublicZoneMembers[zoneID], obj.InstanceID)
		if strings.TrimSpace(obj.ZoneType) != "" {
			replay.PublicZoneTypes[zoneID] = strings.TrimSpace(obj.ZoneType)
		}
	}
}

func buildReplayPublicSnapshot(
	matchID string,
	replay *replayPublicState,
	state *parseState,
	selfSeat int64,
) ([]model.MatchReplayFrameObjectRow, map[int64]model.MatchReplayFrameObjectRow) {
	if replay == nil {
		return nil, map[int64]model.MatchReplayFrameObjectRow{}
	}

	zoneIDs := make([]int, 0, len(replay.PublicZoneMembers))
	for zoneID := range replay.PublicZoneMembers {
		zoneIDs = append(zoneIDs, int(zoneID))
	}
	sort.Ints(zoneIDs)

	out := make([]model.MatchReplayFrameObjectRow, 0)
	byInstance := make(map[int64]model.MatchReplayFrameObjectRow)
	for _, rawZoneID := range zoneIDs {
		zoneID := int64(rawZoneID)
		zoneType := strings.TrimSpace(replay.PublicZoneTypes[zoneID])
		if zoneType == "" {
			zoneType = state.zoneType(matchID, zoneID)
		}
		if zoneType == "" {
			zoneType = fallbackGREZoneType(zoneID)
		}

		members := replay.PublicZoneMembers[zoneID]
		for idx, instanceID := range members {
			current, ok := replay.Objects[instanceID]
			if !ok || current.CardID <= 0 || !isReplayVisibleObject(matchID, current, zoneType, state, selfSeat) {
				continue
			}
			if _, duplicate := byInstance[instanceID]; duplicate {
				continue
			}

			zoneIDCopy := zoneID
			zonePosition := int64(idx + 1)
			row := model.MatchReplayFrameObjectRow{
				InstanceID:           instanceID,
				CardID:               current.CardID,
				ZoneID:               &zoneIDCopy,
				ZoneType:             zoneType,
				ZonePosition:         &zonePosition,
				Visibility:           normalizeGREVisibility(current.Visibility),
				Power:                copyReplayIntPtr(current.Power),
				Toughness:            copyReplayIntPtr(current.Toughness),
				AttackState:          strings.TrimSpace(current.AttackState),
				BlockState:           strings.TrimSpace(current.BlockState),
				BlockAttackerIDsJSON: strings.TrimSpace(current.BlockAttackerIDsJSON),
				CounterSummaryJSON:   strings.TrimSpace(current.CounterSummaryJSON),
				DetailsJSON:          strings.TrimSpace(current.DetailsJSON),
				IsToken:              current.IsToken,
				IsTapped:             current.IsTapped,
				HasSummoningSickness: current.HasSummoningSickness,
			}
			if current.OwnerSeatID > 0 {
				ownerSeatID := current.OwnerSeatID
				row.OwnerSeatID = &ownerSeatID
			}
			if current.ControllerSeatID > 0 {
				controllerSeatID := current.ControllerSeatID
				row.ControllerSeatID = &controllerSeatID
			}
			if current.AttackTargetID > 0 {
				attackTargetID := current.AttackTargetID
				row.AttackTargetID = &attackTargetID
			}

			out = append(out, row)
			byInstance[instanceID] = row
		}
	}

	return out, byInstance
}

func combineGREAnnotationPayload(msg *greGameStateMsg) []byte {
	if msg == nil {
		return nil
	}
	payload := make(map[string]json.RawMessage)
	if len(msg.Annotations) > 0 {
		payload["annotations"] = msg.Annotations
	}
	if len(msg.PersistentAnnotations) > 0 {
		payload["persistentAnnotations"] = msg.PersistentAnnotations
	}
	if len(payload) == 0 {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return encoded
}

func encodeReplayPlayerLifeTotals(values map[int64]int64) []byte {
	if len(values) == 0 {
		return nil
	}

	filtered := make(map[int64]int64, len(values))
	for seatID, lifeTotal := range values {
		if seatID <= 0 {
			continue
		}
		filtered[seatID] = lifeTotal
	}
	if len(filtered) == 0 {
		return nil
	}

	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil
	}
	return encoded
}

func isReplayTrackedZone(matchID string, zoneID int64, replay *replayPublicState, state *parseState, selfSeat int64) bool {
	if zoneID <= 0 {
		return false
	}
	if replay != nil {
		if _, ok := replay.PublicZoneMembers[zoneID]; ok {
			return true
		}
		if strings.TrimSpace(replay.PublicZoneTypes[zoneID]) != "" {
			return true
		}
	}
	if isPublicGREVisibility(replayZoneVisibility(matchID, zoneID, state)) {
		return true
	}

	zoneType := state.zoneType(matchID, zoneID)
	if zoneType == "" {
		zoneType = fallbackGREZoneType(zoneID)
	}
	if zoneType == "hand" && isReplaySelfHandZone(matchID, zoneID, selfSeat, state) {
		return true
	}
	switch zoneType {
	case "revealed", "suppressed", "pending", "command", "stack", "battlefield", "exile", "limbo", "graveyard":
		return true
	default:
		return false
	}
}

func isReplayVisibleObject(
	matchID string,
	current replayCardState,
	zoneType string,
	state *parseState,
	selfSeat int64,
) bool {
	if isPublicGREVisibility(current.Visibility) {
		return true
	}
	if strings.TrimSpace(zoneType) != "hand" {
		return false
	}
	ownerSeatID := current.OwnerSeatID
	if ownerSeatID <= 0 && current.ZoneID > 0 {
		ownerSeatID = state.zoneOwnerSeat(matchID, current.ZoneID)
	}
	return selfSeat > 0 && ownerSeatID > 0 && ownerSeatID == selfSeat
}

func appendReplayInstance(values []int64, target int64) []int64 {
	if target <= 0 {
		return values
	}
	for _, value := range values {
		if value == target {
			return values
		}
	}
	return append(values, target)
}

func removeReplayInstance(values []int64, target int64) []int64 {
	if len(values) == 0 || target <= 0 {
		return values
	}
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value == target {
			continue
		}
		out = append(out, value)
	}
	return out
}

func removeReplayInstanceFromAllZones(replay *replayPublicState, instanceID int64) {
	if replay == nil || instanceID <= 0 {
		return
	}
	for zoneID, members := range replay.PublicZoneMembers {
		replay.PublicZoneMembers[zoneID] = removeReplayInstance(members, instanceID)
	}
}

func removeReplayInstanceFromOtherZones(replay *replayPublicState, keepZoneID, instanceID int64) {
	if replay == nil || instanceID <= 0 {
		return
	}
	for zoneID, members := range replay.PublicZoneMembers {
		if zoneID == keepZoneID {
			continue
		}
		replay.PublicZoneMembers[zoneID] = removeReplayInstance(members, instanceID)
	}
}

func copyReplayInstances(values []int64) []int64 {
	if len(values) == 0 {
		return []int64{}
	}
	out := make([]int64, len(values))
	copy(out, values)
	return out
}

func copyReplayIntPtr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func replayIntPtr(value int64) *int64 {
	out := value
	return &out
}

func replayIntValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func clearReplayCombatState(replay *replayPublicState) {
	if replay == nil {
		return
	}
	for instanceID, current := range replay.Objects {
		current.AttackState = ""
		current.AttackTargetID = 0
		current.BlockState = ""
		current.BlockAttackerIDsJSON = ""
		replay.Objects[instanceID] = current
	}
}

func applyReplayAnnotations(replay *replayPublicState, payload json.RawMessage) {
	if replay == nil || len(payload) == 0 {
		return
	}

	var annotations []greAnnotation
	if err := json.Unmarshal(payload, &annotations); err != nil {
		return
	}

	for _, annotation := range annotations {
		if !hasGREAnnotationType(annotation.Type, "tappeduntappedpermanent") {
			continue
		}
		tapped := annotationDetailInt(annotation.Details, "tapped")
		for _, instanceID := range annotation.AffectedIDs {
			current, ok := replay.Objects[instanceID]
			if !ok {
				continue
			}
			current.IsTapped = tapped > 0
			replay.Objects[instanceID] = current
		}
	}
}

func hasGREAnnotationType(values []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	if target == "" {
		return false
	}
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimPrefix(value, "AnnotationType_"))
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func annotationDetailInt(details []greAnnotationDetail, key string) int64 {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return 0
	}
	for _, detail := range details {
		if !strings.EqualFold(strings.TrimSpace(detail.Key), key) {
			continue
		}
		if len(detail.ValueInt32) > 0 {
			return detail.ValueInt32[0]
		}
	}
	return 0
}

func encodeReplayIntSliceJSON(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(encoded))
}

type replayCounterSummaryEntry struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

func extractReplayCounterSummaryJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}

	counts := make(map[string]int64)
	collectReplayCounterSummary(payload, "", counts, 0)
	if len(counts) == 0 {
		return ""
	}

	entries := make([]replayCounterSummaryEntry, 0, len(counts))
	for label, count := range counts {
		if strings.TrimSpace(label) == "" || count == 0 {
			continue
		}
		entries = append(entries, replayCounterSummaryEntry{
			Label: label,
			Count: count,
		})
	}
	if len(entries) == 0 {
		return ""
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Label < entries[j].Label
	})

	encoded, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(encoded))
}

func collectReplayCounterSummary(value any, parentKey string, counts map[string]int64, depth int) {
	if depth > 8 || value == nil {
		return
	}

	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.Contains(strings.ToLower(key), "counter") {
				collectReplayCounterValue(key, child, counts, depth+1)
			}
			collectReplayCounterSummary(child, key, counts, depth+1)
		}
	case []any:
		for _, child := range typed {
			collectReplayCounterSummary(child, parentKey, counts, depth+1)
		}
	}
}

func collectReplayCounterValue(key string, value any, counts map[string]int64, depth int) {
	if depth > 8 || value == nil {
		return
	}

	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			collectReplayCounterValue(key, child, counts, depth+1)
		}
	case map[string]any:
		label := ""
		for _, candidate := range []string{"counterType", "type", "name"} {
			if rawLabel, ok := typed[candidate].(string); ok {
				label = normalizeReplayCounterLabel(rawLabel)
				if label != "" {
					break
				}
			}
		}
		if label == "" {
			label = normalizeReplayCounterLabel(key)
		}
		if count, ok := replayAnyInt64(typed["count"]); ok && label != "" {
			counts[label] += count
			return
		}
		if count, ok := replayAnyInt64(typed["value"]); ok && label != "" {
			counts[label] += count
			return
		}
		for childKey, childValue := range typed {
			if count, ok := replayAnyInt64(childValue); ok {
				childLabel := normalizeReplayCounterLabel(childKey)
				if childLabel != "" && childLabel != "Counter" && childLabel != "Counters" {
					counts[childLabel] += count
				}
			}
		}
		for childKey, childValue := range typed {
			collectReplayCounterValue(childKey, childValue, counts, depth+1)
		}
	default:
		if count, ok := replayAnyInt64(value); ok {
			label := normalizeReplayCounterLabel(key)
			if label != "" && label != "Counter" && label != "Counters" {
				counts[label] += count
			}
		}
	}
}

func normalizeReplayCounterLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "CounterType_")
	raw = strings.TrimPrefix(raw, "counterType_")
	raw = strings.TrimPrefix(raw, "Counter_")
	raw = strings.TrimPrefix(raw, "counter_")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	switch strings.ToUpper(raw) {
	case "P1P1":
		return "+1/+1"
	case "M1M1":
		return "-1/-1"
	}

	normalized := strings.ReplaceAll(raw, "_", " ")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return ""
	}

	parts := strings.Fields(normalized)
	for idx, part := range parts {
		lower := strings.ToLower(part)
		if len(lower) == 0 {
			continue
		}
		parts[idx] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}

func replayAnyInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		out, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return out, true
	default:
		return 0, false
	}
}
