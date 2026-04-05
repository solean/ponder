package ingest

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cschnabel/mtgdata/internal/db"
	"github.com/cschnabel/mtgdata/internal/model"
)

var (
	reOutgoing          = regexp.MustCompile(`^\[UnityCrossThreadLogger\]==>\s+([A-Za-z0-9_]+)\s+(.*)$`)
	reComplete          = regexp.MustCompile(`^<==\s+([A-Za-z0-9_]+)\(([^)]*)\)`)
	rePersonaPlain      = regexp.MustCompile(`"PersonaId":"([A-Za-z0-9_\-]+)"`)
	rePersonaEscaped    = regexp.MustCompile(`\\\"PersonaId\\\":\\\"([A-Za-z0-9_\-]+)\\\"`)
	rePersonaMatchTo    = regexp.MustCompile(`Match to ([A-Za-z0-9_\-]+):`)
	reClientID          = regexp.MustCompile(`"clientId"\s*:\s*"([A-Za-z0-9_\-]+)"`)
	reScreenName        = regexp.MustCompile(`"screenName"\s*:\s*"([^"]+)"`)
	reUnityLogTimestamp = regexp.MustCompile(`^\[UnityCrossThreadLogger\](\d{1,2}/\d{1,2}/\d{4} \d{1,2}:\d{2}:\d{2} (?:AM|PM))`)
)

type Parser struct {
	store                   *db.Store
	stateMu                 sync.Mutex
	stateByLog              map[string]*parseState
	personaID               string
	playerName              string
	pendingCompletedMatches []string
}

func NewParser(store *db.Store) *Parser {
	return &Parser{
		store:      store,
		stateByLog: make(map[string]*parseState),
	}
}

func (p *Parser) stateForLog(logPath string, reset bool) *parseState {
	key := strings.TrimSpace(logPath)
	if key == "" {
		return &parseState{
			personaID:  p.personaID,
			playerName: p.playerName,
		}
	}

	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	if reset {
		state := &parseState{
			personaID:  p.personaID,
			playerName: p.playerName,
		}
		p.stateByLog[key] = state
		return state
	}

	state, ok := p.stateByLog[key]
	if !ok || state == nil {
		state = &parseState{
			personaID:  p.personaID,
			playerName: p.playerName,
		}
		p.stateByLog[key] = state
	}
	return state
}

type parseState struct {
	personaID                 string
	playerName                string
	activeMatchID             string
	selfSeatByMatch           map[string]int64
	turnByMatch               map[string]int64
	activePlayerByMatch       map[string]int64
	phaseByMatch              map[string]string
	zoneTypeByMatch           map[string]map[int64]string
	zoneVisibilityByMatch     map[string]map[int64]string
	zoneOwnerSeatByMatch      map[string]map[int64]int64
	gameNumberByMatch         map[string]int64
	replayByMatchGame         map[string]*replayPublicState
	lastUnityLogTimestamp     string
	pendingResponseMethod     string
	pendingResponseRequestID  string
	pendingResponseObservedAt string
}

func (s *parseState) rememberSelfSeat(matchID string, seatID int64) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || seatID <= 0 {
		return
	}
	if s.selfSeatByMatch == nil {
		s.selfSeatByMatch = make(map[string]int64)
	}
	s.selfSeatByMatch[matchID] = seatID
}

func (s *parseState) selfSeat(matchID string) int64 {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || s.selfSeatByMatch == nil {
		return 0
	}
	return s.selfSeatByMatch[matchID]
}

func (s *parseState) rememberTurn(matchID string, turnNumber int64) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || turnNumber <= 0 {
		return
	}
	if s.turnByMatch == nil {
		s.turnByMatch = make(map[string]int64)
	}
	s.turnByMatch[matchID] = turnNumber
}

func (s *parseState) turn(matchID string) int64 {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || s.turnByMatch == nil {
		return 0
	}
	return s.turnByMatch[matchID]
}

func (s *parseState) rememberActivePlayer(matchID string, seatID int64) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || seatID <= 0 {
		return
	}
	if s.activePlayerByMatch == nil {
		s.activePlayerByMatch = make(map[string]int64)
	}
	s.activePlayerByMatch[matchID] = seatID
}

func (s *parseState) activePlayer(matchID string) int64 {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || s.activePlayerByMatch == nil {
		return 0
	}
	return s.activePlayerByMatch[matchID]
}

func (s *parseState) rememberPhase(matchID, phase string) {
	matchID = strings.TrimSpace(matchID)
	phase = normalizeGREPhase(phase)
	if matchID == "" || phase == "" {
		return
	}
	if s.phaseByMatch == nil {
		s.phaseByMatch = make(map[string]string)
	}
	s.phaseByMatch[matchID] = phase
}

func (s *parseState) phase(matchID string) string {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || s.phaseByMatch == nil {
		return ""
	}
	return s.phaseByMatch[matchID]
}

func (s *parseState) rememberZoneType(matchID string, zoneID int64, zoneType string) {
	matchID = strings.TrimSpace(matchID)
	zoneType = normalizeGREZoneType(zoneType)
	if matchID == "" || zoneID <= 0 || zoneType == "" {
		return
	}
	if s.zoneTypeByMatch == nil {
		s.zoneTypeByMatch = make(map[string]map[int64]string)
	}
	byZone, ok := s.zoneTypeByMatch[matchID]
	if !ok {
		byZone = make(map[int64]string)
		s.zoneTypeByMatch[matchID] = byZone
	}
	byZone[zoneID] = zoneType
}

func (s *parseState) zoneType(matchID string, zoneID int64) string {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || zoneID <= 0 || s.zoneTypeByMatch == nil {
		return ""
	}
	byZone := s.zoneTypeByMatch[matchID]
	if byZone == nil {
		return ""
	}
	return byZone[zoneID]
}

func (s *parseState) rememberZoneVisibility(matchID string, zoneID int64, visibility string) {
	matchID = strings.TrimSpace(matchID)
	visibility = normalizeGREVisibility(visibility)
	if matchID == "" || zoneID <= 0 || visibility == "" {
		return
	}
	if s.zoneVisibilityByMatch == nil {
		s.zoneVisibilityByMatch = make(map[string]map[int64]string)
	}
	byZone, ok := s.zoneVisibilityByMatch[matchID]
	if !ok {
		byZone = make(map[int64]string)
		s.zoneVisibilityByMatch[matchID] = byZone
	}
	byZone[zoneID] = visibility
}

func (s *parseState) zoneVisibility(matchID string, zoneID int64) string {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || zoneID <= 0 || s.zoneVisibilityByMatch == nil {
		return ""
	}
	byZone := s.zoneVisibilityByMatch[matchID]
	if byZone == nil {
		return ""
	}
	return byZone[zoneID]
}

func (s *parseState) rememberZoneOwnerSeat(matchID string, zoneID, ownerSeatID int64) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || zoneID <= 0 || ownerSeatID <= 0 {
		return
	}
	if s.zoneOwnerSeatByMatch == nil {
		s.zoneOwnerSeatByMatch = make(map[string]map[int64]int64)
	}
	byZone, ok := s.zoneOwnerSeatByMatch[matchID]
	if !ok {
		byZone = make(map[int64]int64)
		s.zoneOwnerSeatByMatch[matchID] = byZone
	}
	byZone[zoneID] = ownerSeatID
}

func (s *parseState) zoneOwnerSeat(matchID string, zoneID int64) int64 {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || zoneID <= 0 || s.zoneOwnerSeatByMatch == nil {
		return 0
	}
	byZone := s.zoneOwnerSeatByMatch[matchID]
	if byZone == nil {
		return 0
	}
	return byZone[zoneID]
}

func (s *parseState) rememberGameNumber(matchID string, gameNumber int64) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || gameNumber <= 0 {
		return
	}
	if s.gameNumberByMatch == nil {
		s.gameNumberByMatch = make(map[string]int64)
	}
	s.gameNumberByMatch[matchID] = gameNumber
}

func (s *parseState) gameNumber(matchID string) int64 {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || s.gameNumberByMatch == nil {
		return 0
	}
	return s.gameNumberByMatch[matchID]
}

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

func (s *parseState) clearPendingResponse() {
	s.pendingResponseMethod = ""
	s.pendingResponseRequestID = ""
	s.pendingResponseObservedAt = ""
}

func (p *Parser) rememberPersonaID(personaID string) {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.personaID = personaID
}

func (p *Parser) rememberPlayerName(playerName string) {
	playerName = strings.TrimSpace(playerName)
	if playerName == "" {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.playerName = playerName
}

func (p *Parser) enqueueCompletedMatch(arenaMatchID string) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	for _, pending := range p.pendingCompletedMatches {
		if pending == arenaMatchID {
			return
		}
	}
	p.pendingCompletedMatches = append(p.pendingCompletedMatches, arenaMatchID)
	if len(p.pendingCompletedMatches) > 32 {
		p.pendingCompletedMatches = append([]string(nil), p.pendingCompletedMatches[len(p.pendingCompletedMatches)-32:]...)
	}
}

func (p *Parser) dequeueCompletedMatch() string {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if len(p.pendingCompletedMatches) == 0 {
		return ""
	}
	matchID := p.pendingCompletedMatches[0]
	p.pendingCompletedMatches = p.pendingCompletedMatches[1:]
	return matchID
}

type outgoingEnvelope struct {
	ID      string          `json:"id"`
	Request json.RawMessage `json:"request"`
}

type eventJoinRequest struct {
	EventName         string `json:"EventName"`
	EntryCurrencyType string `json:"EntryCurrencyType"`
	EntryCurrencyPaid int64  `json:"EntryCurrencyPaid"`
}

type eventClaimPrizeRequest struct {
	EventName string `json:"EventName"`
}

type eventSetDeckRequest struct {
	EventName string `json:"EventName"`
	Summary   struct {
		DeckID     string `json:"DeckId"`
		Name       string `json:"Name"`
		Attributes []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"Attributes"`
	} `json:"Summary"`
	Deck struct {
		MainDeck []struct {
			CardID   int64 `json:"cardId"`
			Quantity int64 `json:"quantity"`
		} `json:"MainDeck"`
		Sideboard []struct {
			CardID   int64 `json:"cardId"`
			Quantity int64 `json:"quantity"`
		} `json:"Sideboard"`
		CommandZone []struct {
			CardID   int64 `json:"cardId"`
			Quantity int64 `json:"quantity"`
		} `json:"CommandZone"`
		Companions []struct {
			CardID   int64 `json:"cardId"`
			Quantity int64 `json:"quantity"`
		} `json:"Companions"`
	} `json:"Deck"`
}

type playerDraftPickRequest struct {
	DraftID string  `json:"DraftId"`
	GrpIDs  []int64 `json:"GrpIds"`
	Pack    int64   `json:"Pack"`
	Pick    int64   `json:"Pick"`
}

type botDraftPickRequest struct {
	EventName string `json:"EventName"`
	PickInfo  struct {
		CardIDs    []string `json:"CardIds"`
		PackNumber int64    `json:"PackNumber"`
		PickNumber int64    `json:"PickNumber"`
	} `json:"PickInfo"`
}

type draftCompleteRequest struct {
	EventName  string `json:"EventName"`
	IsBotDraft bool   `json:"IsBotDraft"`
}

type logBusinessEvent struct {
	EventType     int64   `json:"EventType"`
	EventTime     string  `json:"EventTime"`
	EventName     string  `json:"EventName"`
	EventID       string  `json:"EventId"`
	DraftID       string  `json:"DraftId"`
	MatchID       string  `json:"MatchId"`
	SeatID        int64   `json:"SeatId"`
	SeatNumber    int64   `json:"SeatNumber"`
	TeamID        int64   `json:"TeamId"`
	WinningTeamID int64   `json:"WinningTeamId"`
	WinningReason string  `json:"WinningReason"`
	TurnCount     int64   `json:"TurnCount"`
	SecondsCount  int64   `json:"SecondsCount"`
	PackNumber    int64   `json:"PackNumber"`
	PickNumber    int64   `json:"PickNumber"`
	PickGrpID     int64   `json:"PickGrpId"`
	CardsInPack   []int64 `json:"CardsInPack"`
}

type roomPlayer struct {
	UserID       string `json:"userId"`
	PlayerName   string `json:"playerName"`
	SystemSeatID int64  `json:"systemSeatId"`
	TeamID       int64  `json:"teamId"`
	EventID      string `json:"eventId"`
}

type roomResultEntry struct {
	Scope         string `json:"scope"`
	Result        string `json:"result"`
	WinningTeamID int64  `json:"winningTeamId"`
	Reason        string `json:"reason"`
}

type roomStateEnvelope struct {
	Timestamp                      string `json:"timestamp"`
	MatchGameRoomStateChangedEvent *struct {
		GameRoomInfo *struct {
			GameRoomConfig *struct {
				MatchID         string       `json:"matchId"`
				ReservedPlayers []roomPlayer `json:"reservedPlayers"`
			} `json:"gameRoomConfig"`
			StateType        string `json:"stateType"`
			FinalMatchResult *struct {
				MatchID              string            `json:"matchId"`
				MatchCompletedReason string            `json:"matchCompletedReason"`
				ResultList           []roomResultEntry `json:"resultList"`
			} `json:"finalMatchResult"`
			Players []roomPlayer `json:"players"`
		} `json:"gameRoomInfo"`
	} `json:"matchGameRoomStateChangedEvent"`
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

type combinedRankInfoResponse struct {
	ConstructedSeasonOrdinal *int64 `json:"constructedSeasonOrdinal"`
	ConstructedClass         string `json:"constructedClass"`
	ConstructedLevel         *int64 `json:"constructedLevel"`
	ConstructedStep          *int64 `json:"constructedStep"`
	ConstructedMatchesWon    *int64 `json:"constructedMatchesWon"`
	ConstructedMatchesLost   *int64 `json:"constructedMatchesLost"`
	LimitedSeasonOrdinal     *int64 `json:"limitedSeasonOrdinal"`
	LimitedClass             string `json:"limitedClass"`
	LimitedLevel             *int64 `json:"limitedLevel"`
	LimitedStep              *int64 `json:"limitedStep"`
	LimitedMatchesWon        *int64 `json:"limitedMatchesWon"`
	LimitedMatchesLost       *int64 `json:"limitedMatchesLost"`
}

func (p *Parser) ParseFile(ctx context.Context, logPath string, resume bool) (model.ParseStats, error) {
	stats := model.ParseStats{LogPath: logPath, StartedAt: time.Now().UTC()}

	startOffset := int64(0)
	startLine := int64(0)
	resetState := !resume
	if resume {
		ingestState, err := p.store.GetIngestState(ctx, logPath)
		if err != nil {
			return stats, err
		}
		if ingestState.Found {
			startOffset = ingestState.Offset
			startLine = ingestState.LineNo
			if startOffset == 0 && startLine == 0 {
				resetState = true
			}
		}
	}

	file, err := os.Open(logPath)
	if err != nil {
		return stats, fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return stats, fmt.Errorf("stat log file: %w", err)
	}

	// MTGA rotates/truncates Player.log. If our saved offset points past EOF,
	// restart from the beginning of the current file so tailing can recover.
	if startOffset > info.Size() {
		startOffset = 0
		startLine = 0
		resetState = true
	}

	state := p.stateForLog(logPath, resetState)

	if startOffset > 0 {
		if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
			return stats, fmt.Errorf("seek to offset %d: %w", startOffset, err)
		}
	}

	reader := bufio.NewReaderSize(file, 4*1024*1024)

	tx, err := p.store.BeginTx(ctx)
	if err != nil {
		return stats, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	const batchSize = int64(500)
	lineNo := startLine
	byteOffset := startOffset
	linesSinceCommit := int64(0)

	commit := func() error {
		if err := p.store.SaveIngestState(ctx, tx, logPath, byteOffset, lineNo); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}
		tx, err = p.store.BeginTx(ctx)
		if err != nil {
			return fmt.Errorf("begin new tx: %w", err)
		}
		linesSinceCommit = 0
		return nil
	}

	for {
		lineStartOffset := byteOffset
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return stats, fmt.Errorf("read line: %w", readErr)
		}
		if len(line) == 0 && errors.Is(readErr, io.EOF) {
			break
		}

		lineNo++
		byteOffset += int64(len(line))
		stats.LinesRead++
		stats.BytesRead += int64(len(line))
		linesSinceCommit++

		trimmed := strings.TrimRight(line, "\r\n")
		if err := p.processLine(ctx, tx, &stats, state, logPath, lineNo, lineStartOffset, trimmed); err != nil {
			return stats, fmt.Errorf("process line %d: %w", lineNo, err)
		}

		if linesSinceCommit >= batchSize {
			if err := commit(); err != nil {
				return stats, err
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	if err := p.store.SaveIngestState(ctx, tx, logPath, byteOffset, lineNo); err != nil {
		return stats, err
	}
	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit final tx: %w", err)
	}

	stats.CompletedAt = time.Now().UTC()
	return stats, nil
}

func (p *Parser) processLine(ctx context.Context, tx *sql.Tx, stats *model.ParseStats, state *parseState, logPath string, lineNo, byteOffset int64, line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	if ts := parseUnityLogTimestamp(line); ts != "" {
		state.lastUnityLogTimestamp = ts
	}

	if state.personaID == "" {
		match := rePersonaPlain.FindStringSubmatch(line)
		if len(match) != 2 {
			match = rePersonaEscaped.FindStringSubmatch(line)
		}
		if len(match) == 2 {
			id := match[1]
			if !strings.HasPrefix(id, "NoInstallID") {
				state.personaID = id
			}
		}
		if state.personaID == "" {
			if m := rePersonaMatchTo.FindStringSubmatch(line); len(m) == 2 {
				state.personaID = strings.TrimSpace(m[1])
			}
		}
		if state.personaID == "" {
			if m := reClientID.FindStringSubmatch(line); len(m) == 2 {
				state.personaID = strings.TrimSpace(m[1])
			}
		}
	}
	if state.personaID != "" {
		p.rememberPersonaID(state.personaID)
	}
	if state.playerName == "" {
		if m := reScreenName.FindStringSubmatch(line); len(m) == 2 {
			state.playerName = strings.TrimSpace(m[1])
		}
	}
	if state.playerName != "" {
		p.rememberPlayerName(state.playerName)
	}

	if state.pendingResponseMethod != "" && strings.HasPrefix(line, "{") {
		if err := p.handleMethodResponse(ctx, tx, stats, state, logPath, lineNo, byteOffset, line); err != nil {
			return err
		}
		return nil
	}

	if m := reOutgoing.FindStringSubmatch(line); len(m) == 3 {
		method := m[1]
		envelopeJSON := m[2]
		if err := p.handleOutgoing(ctx, tx, stats, state, logPath, lineNo, byteOffset, method, envelopeJSON); err != nil {
			return err
		}
		return nil
	}

	if m := reComplete.FindStringSubmatch(line); len(m) == 3 {
		if err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "method_complete", m[1], m[2], nil, ""); err != nil {
			return err
		}
		stats.RawEventsStored++
		if m[1] == "RankGetCombinedRankInfo" {
			state.pendingResponseMethod = m[1]
			state.pendingResponseRequestID = m[2]
			state.pendingResponseObservedAt = state.lastUnityLogTimestamp
		} else {
			state.clearPendingResponse()
		}
		return nil
	}

	if state.pendingResponseMethod != "" {
		state.clearPendingResponse()
	}

	if strings.HasPrefix(line, "{") {
		if strings.Contains(line, "\"matchGameRoomStateChangedEvent\"") {
			if err := p.handleRoomStateJSON(ctx, tx, stats, logPath, lineNo, byteOffset, line, state); err != nil {
				return err
			}
			return nil
		}
		if strings.Contains(line, "\"greToClientEvent\"") {
			if err := p.handleGREJSON(ctx, tx, line, state); err != nil {
				return err
			}
			return nil
		}
	}

	return nil
}

func parseUnityLogTimestamp(line string) string {
	m := reUnityLogTimestamp.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) != 2 {
		return ""
	}
	parsed, err := time.ParseInLocation("1/2/2006 3:04:05 PM", m[1], time.Local)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func (p *Parser) queueCompletedMatchIfRankPending(ctx context.Context, tx *sql.Tx, arenaMatchID, result string, terminalChange bool) error {
	if result != "win" && result != "loss" {
		return nil
	}
	if terminalChange {
		p.enqueueCompletedMatch(arenaMatchID)
		return nil
	}

	hasSnapshot, err := p.store.MatchHasRankSnapshot(ctx, tx, arenaMatchID)
	if err != nil {
		return err
	}
	if !hasSnapshot {
		p.enqueueCompletedMatch(arenaMatchID)
	}
	return nil
}

func (p *Parser) handleMethodResponse(ctx context.Context, tx *sql.Tx, stats *model.ParseStats, state *parseState, logPath string, lineNo, byteOffset int64, line string) error {
	method := state.pendingResponseMethod
	requestID := state.pendingResponseRequestID
	observedAt := state.pendingResponseObservedAt
	state.clearPendingResponse()

	if err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "method_result", method, requestID, []byte(line), ""); err != nil {
		return err
	}
	stats.RawEventsStored++

	if method != "RankGetCombinedRankInfo" {
		return nil
	}

	var payload combinedRankInfoResponse
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return nil
	}

	arenaMatchID := p.dequeueCompletedMatch()
	if arenaMatchID == "" {
		return nil
	}

	if err := p.store.UpsertMatchRankSnapshot(ctx, tx, arenaMatchID, db.MatchRankSnapshot{
		ObservedAt:               observedAt,
		PayloadJSON:              line,
		ConstructedSeasonOrdinal: payload.ConstructedSeasonOrdinal,
		ConstructedRankClass:     payload.ConstructedClass,
		ConstructedLevel:         payload.ConstructedLevel,
		ConstructedStep:          payload.ConstructedStep,
		ConstructedMatchesWon:    payload.ConstructedMatchesWon,
		ConstructedMatchesLost:   payload.ConstructedMatchesLost,
		LimitedSeasonOrdinal:     payload.LimitedSeasonOrdinal,
		LimitedRankClass:         payload.LimitedClass,
		LimitedLevel:             payload.LimitedLevel,
		LimitedStep:              payload.LimitedStep,
		LimitedMatchesWon:        payload.LimitedMatchesWon,
		LimitedMatchesLost:       payload.LimitedMatchesLost,
	}); err != nil {
		return err
	}
	stats.RankSnapshots++
	return nil
}

func decodeRawRequest(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	if strings.HasPrefix(trimmed, "\"") {
		var inner string
		if err := json.Unmarshal([]byte(trimmed), &inner); err != nil {
			return nil, fmt.Errorf("decode string request: %w", err)
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return nil, nil
		}
		if strings.HasPrefix(inner, "{") || strings.HasPrefix(inner, "[") {
			return []byte(inner), nil
		}
		return []byte(strconv.Quote(inner)), nil
	}

	return []byte(trimmed), nil
}

func formatFromAttributes(attrs []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}) string {
	for _, a := range attrs {
		if strings.EqualFold(strings.TrimSpace(a.Name), "Format") {
			return strings.Trim(strings.TrimSpace(a.Value), `"`)
		}
	}
	return ""
}

func cardSectionCards(section string, in []struct {
	CardID   int64 `json:"cardId"`
	Quantity int64 `json:"quantity"`
}) []db.DeckCard {
	out := make([]db.DeckCard, 0, len(in))
	for _, c := range in {
		if c.Quantity <= 0 {
			continue
		}
		out = append(out, db.DeckCard{Section: section, CardID: c.CardID, Quantity: c.Quantity})
	}
	return out
}

func parseStringIDsToInt64(in []string) []int64 {
	out := make([]int64, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseRoomTimestamp(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return ""
	}

	var ts time.Time
	switch {
	case v >= 1_000_000_000_000 && v < 10_000_000_000_000:
		ts = time.UnixMilli(v)
	case v >= 1_000_000_000 && v < 10_000_000_000:
		ts = time.Unix(v, 0)
	default:
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func roomEventName(players []roomPlayer) string {
	for _, pl := range players {
		eventID := strings.TrimSpace(pl.EventID)
		if eventID != "" {
			return eventID
		}
	}
	return ""
}

func normalizeWinningReason(reason string) string {
	reason = strings.TrimSpace(reason)
	reason = strings.TrimPrefix(reason, "ResultReason_")
	reason = strings.TrimPrefix(reason, "WinningReason_")
	return reason
}

func chooseMatchResult(results []roomResultEntry) (int64, string) {
	return chooseResultForScope(results, "MatchScope_Match")
}

func chooseGameResult(results []roomResultEntry) (int64, string) {
	return chooseResultForScope(results, "MatchScope_Game")
}

func chooseResultForScope(results []roomResultEntry, preferredScope string) (int64, string) {
	var preferredTeamID int64
	var preferredReason string
	var fallbackTeamID int64
	var fallbackReason string
	preferredScope = strings.TrimSpace(preferredScope)
	for _, r := range results {
		if r.WinningTeamID <= 0 {
			continue
		}
		reason := normalizeWinningReason(r.Reason)
		if preferredScope != "" && strings.EqualFold(strings.TrimSpace(r.Scope), preferredScope) {
			preferredTeamID = r.WinningTeamID
			preferredReason = reason
		}
		fallbackTeamID = r.WinningTeamID
		fallbackReason = reason
	}
	if preferredTeamID > 0 {
		return preferredTeamID, preferredReason
	}
	return fallbackTeamID, fallbackReason
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
	for _, msg := range env.GREToClientEvent.Messages {
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
			state.activeMatchID = matchID
			state.rememberSelfSeat(matchID, selfSeat)
		}
		if matchID == "" {
			continue
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

func (p *Parser) handleOutgoing(ctx context.Context, tx *sql.Tx, stats *model.ParseStats, state *parseState, logPath string, lineNo, byteOffset int64, method, envelopeJSON string) error {
	var env outgoingEnvelope
	if err := json.Unmarshal([]byte(envelopeJSON), &env); err != nil {
		if err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "outgoing_unparsed", method, "", nil, ""); err != nil {
			return err
		}
		stats.RawEventsStored++
		return nil
	}

	requestPayload, err := decodeRawRequest(env.Request)
	if err != nil {
		return fmt.Errorf("decode raw request for %s: %w", method, err)
	}

	if err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "outgoing", method, env.ID, requestPayload, ""); err != nil {
		return err
	}
	stats.RawEventsStored++
	observedAt := state.lastUnityLogTimestamp

	switch method {
	case "EventJoin":
		var req eventJoinRequest
		if err := json.Unmarshal(requestPayload, &req); err != nil {
			return nil
		}
		if req.EventName == "" {
			return nil
		}
		if err := p.store.UpsertEventRunJoin(ctx, tx, req.EventName, req.EntryCurrencyType, req.EntryCurrencyPaid, observedAt); err != nil {
			return err
		}
	case "EventClaimPrize":
		var req eventClaimPrizeRequest
		if err := json.Unmarshal(requestPayload, &req); err != nil {
			return nil
		}
		if req.EventName != "" {
			if err := p.store.MarkEventRunClaimed(ctx, tx, req.EventName, observedAt); err != nil {
				return err
			}
		}
	case "EventSetDeckV2":
		var req eventSetDeckRequest
		if err := json.Unmarshal(requestPayload, &req); err != nil {
			return nil
		}
		if req.Summary.DeckID == "" {
			return nil
		}
		cards := make([]db.DeckCard, 0, len(req.Deck.MainDeck)+len(req.Deck.Sideboard)+len(req.Deck.CommandZone)+len(req.Deck.Companions))
		cards = append(cards, cardSectionCards("main", req.Deck.MainDeck)...)
		cards = append(cards, cardSectionCards("sideboard", req.Deck.Sideboard)...)
		cards = append(cards, cardSectionCards("command", req.Deck.CommandZone)...)
		cards = append(cards, cardSectionCards("companion", req.Deck.Companions)...)

		format := formatFromAttributes(req.Summary.Attributes)
		lastUpdated := ""
		for _, a := range req.Summary.Attributes {
			if strings.EqualFold(strings.TrimSpace(a.Name), "LastUpdated") {
				lastUpdated = strings.Trim(strings.TrimSpace(a.Value), `"`)
				break
			}
		}

		_, err := p.store.UpsertDeck(ctx, tx, req.Summary.DeckID, req.EventName, req.Summary.Name, format, "event_set_deck", lastUpdated, cards)
		if err != nil {
			return err
		}
		stats.DecksUpserted++
	case "EventPlayerDraftMakePick":
		var req playerDraftPickRequest
		if err := json.Unmarshal(requestPayload, &req); err != nil {
			return nil
		}
		if req.DraftID == "" {
			return nil
		}
		draftID := req.DraftID
		sessionID, err := p.store.EnsureDraftSession(ctx, tx, "", &draftID, false, observedAt)
		if err != nil {
			return err
		}
		if err := p.store.InsertDraftPick(ctx, tx, sessionID, req.Pack, req.Pick, req.GrpIDs, nil, observedAt); err != nil {
			return err
		}
		stats.DraftPicksAdded++
	case "BotDraftDraftPick":
		var req botDraftPickRequest
		if err := json.Unmarshal(requestPayload, &req); err != nil {
			return nil
		}
		if req.EventName == "" {
			return nil
		}
		sessionID, err := p.store.EnsureDraftSession(ctx, tx, req.EventName, nil, true, observedAt)
		if err != nil {
			return err
		}
		picked := parseStringIDsToInt64(req.PickInfo.CardIDs)
		if err := p.store.InsertDraftPick(ctx, tx, sessionID, req.PickInfo.PackNumber, req.PickInfo.PickNumber, picked, nil, observedAt); err != nil {
			return err
		}
		stats.DraftPicksAdded++
	case "DraftCompleteDraft":
		var req draftCompleteRequest
		if err := json.Unmarshal(requestPayload, &req); err != nil {
			return nil
		}
		if err := p.store.CompleteDraftSession(ctx, tx, req.EventName, nil, req.IsBotDraft, observedAt); err != nil {
			return err
		}
	case "LogBusinessEvents":
		var evt logBusinessEvent
		if err := json.Unmarshal(requestPayload, &evt); err != nil {
			return nil
		}
		switch evt.EventType {
		case 24:
			if evt.DraftID == "" || evt.PackNumber <= 0 || evt.PickNumber <= 0 {
				return nil
			}

			eventName := evt.EventID
			if eventName == "" {
				eventName = evt.EventName
			}
			draftTS := evt.EventTime
			if strings.TrimSpace(draftTS) == "" {
				draftTS = observedAt
			}

			draftID := evt.DraftID
			sessionID, err := p.store.EnsureDraftSession(ctx, tx, eventName, &draftID, false, draftTS)
			if err != nil {
				return err
			}

			var picked []int64
			if evt.PickGrpID > 0 {
				picked = []int64{evt.PickGrpID}
			}
			if err := p.store.InsertDraftPick(ctx, tx, sessionID, evt.PackNumber, evt.PickNumber, picked, evt.CardsInPack, draftTS); err != nil {
				return err
			}
		case 3:
			if evt.MatchID == "" {
				return nil
			}
			eventName := evt.EventID
			if eventName == "" {
				eventName = evt.EventName
			}
			_, err := p.store.UpsertMatchStart(ctx, tx, evt.MatchID, eventName, evt.SeatID, evt.EventTime)
			if err != nil {
				return err
			}
			state.activeMatchID = strings.TrimSpace(evt.MatchID)
			state.rememberSelfSeat(evt.MatchID, evt.SeatID)
			_ = p.store.LinkMatchToLatestDeckByEvent(ctx, tx, evt.MatchID, eventName, "pre_match")
			stats.MatchesUpserted++
		case 4:
			if evt.MatchID == "" {
				return nil
			}
			_, result, changed, err := p.store.UpdateMatchEnd(ctx, tx, evt.MatchID, evt.TeamID, evt.WinningTeamID, evt.TurnCount, evt.SecondsCount, evt.WinningReason, evt.EventTime)
			if err != nil {
				return err
			}
			if err := p.queueCompletedMatchIfRankPending(ctx, tx, evt.MatchID, result, changed); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *Parser) handleRoomStateJSON(ctx context.Context, tx *sql.Tx, stats *model.ParseStats, logPath string, lineNo, byteOffset int64, line string, state *parseState) error {
	var env roomStateEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return nil
	}
	if env.MatchGameRoomStateChangedEvent == nil || env.MatchGameRoomStateChangedEvent.GameRoomInfo == nil || env.MatchGameRoomStateChangedEvent.GameRoomInfo.GameRoomConfig == nil {
		return nil
	}

	info := env.MatchGameRoomStateChangedEvent.GameRoomInfo
	config := info.GameRoomConfig
	if config.MatchID == "" {
		return nil
	}

	players := info.Players
	if len(config.ReservedPlayers) > 0 {
		players = config.ReservedPlayers
	}

	eventName := roomEventName(config.ReservedPlayers)
	if eventName == "" {
		eventName = roomEventName(players)
	}
	matchTS := parseRoomTimestamp(env.Timestamp)

	selfSeen := false
	var selfSeatID int64
	var selfTeamID int64
	opponentName := ""
	opponentUserID := ""
	personaID := strings.TrimSpace(state.personaID)

	for _, pl := range players {
		playerUserID := strings.TrimSpace(pl.UserID)
		playerName := strings.TrimSpace(pl.PlayerName)

		if personaID != "" && playerUserID == personaID {
			selfSeen = true
			if pl.SystemSeatID > 0 {
				selfSeatID = pl.SystemSeatID
			}
			if pl.TeamID > 0 {
				selfTeamID = pl.TeamID
			}
			if state.playerName == "" && playerName != "" {
				state.playerName = playerName
				p.rememberPlayerName(playerName)
			}
			continue
		}
		if opponentName == "" {
			// Avoid ever setting self as opponent by name when known.
			if state.playerName != "" && strings.EqualFold(playerName, strings.TrimSpace(state.playerName)) {
				continue
			}
			opponentName = playerName
			opponentUserID = playerUserID
		}
	}

	if _, err := p.store.UpsertMatchStart(ctx, tx, config.MatchID, eventName, selfSeatID, matchTS); err != nil {
		return err
	}
	state.activeMatchID = strings.TrimSpace(config.MatchID)
	state.rememberSelfSeat(config.MatchID, selfSeatID)
	if eventName != "" {
		_ = p.store.LinkMatchToLatestDeckByEvent(ctx, tx, config.MatchID, eventName, "room_state")
	}

	if selfSeen && (strings.TrimSpace(opponentName) != "" || strings.TrimSpace(opponentUserID) != "") {
		if err := p.store.UpdateMatchOpponent(ctx, tx, config.MatchID, opponentName, opponentUserID); err != nil {
			return err
		}
	}

	if strings.EqualFold(strings.TrimSpace(info.StateType), "MatchGameRoomStateType_MatchCompleted") && selfTeamID > 0 && info.FinalMatchResult != nil {
		winningTeamID, reason := chooseMatchResult(info.FinalMatchResult.ResultList)
		if winningTeamID > 0 {
			if _, result, changed, err := p.store.UpdateMatchEnd(ctx, tx, config.MatchID, selfTeamID, winningTeamID, 0, 0, reason, matchTS); err != nil {
				return err
			} else if err := p.queueCompletedMatchIfRankPending(ctx, tx, config.MatchID, result, changed); err != nil {
				return err
			}
		}
	}

	if err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "room_state", "matchGameRoomStateChangedEvent", "", nil, ""); err != nil {
		return err
	}
	stats.RawEventsStored++
	stats.MatchesUpserted++
	return nil
}
