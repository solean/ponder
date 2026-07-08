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
	parser := &Parser{
		store:      store,
		stateByLog: make(map[string]*parseState),
	}

	if store != nil {
		if playerName, err := store.PlayerName(context.Background()); err == nil {
			parser.playerName = playerName
		}
	}

	return parser
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
	deckByEvent               map[string]string
	replayByMatchGame         map[string]*replayPublicState
	lastUnityLogTimestamp     string
	pendingResponseMethod     string
	pendingResponseRequestID  string
	pendingResponseObservedAt string
}

func (s *parseState) rememberEventDeck(eventName, arenaDeckID string) {
	eventName = strings.TrimSpace(eventName)
	arenaDeckID = strings.TrimSpace(arenaDeckID)
	if eventName == "" || arenaDeckID == "" {
		return
	}
	if s.deckByEvent == nil {
		s.deckByEvent = make(map[string]string)
	}
	s.deckByEvent[eventName] = arenaDeckID
}

func (s *parseState) eventDeck(eventName string) string {
	eventName = strings.TrimSpace(eventName)
	if eventName == "" || s.deckByEvent == nil {
		return ""
	}
	return s.deckByEvent[eventName]
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

func (p *Parser) rememberPlayerName(playerName string) bool {
	playerName = strings.TrimSpace(playerName)
	if playerName == "" {
		return false
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.playerName == playerName {
		return false
	}
	p.playerName = playerName
	return true
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
	if m := reScreenName.FindStringSubmatch(line); len(m) == 2 {
		playerName := strings.TrimSpace(m[1])
		if playerName != "" && playerName != state.playerName {
			state.playerName = playerName
		}
	}
	if state.playerName != "" {
		if p.rememberPlayerName(state.playerName) {
			if err := p.store.SavePlayerName(ctx, tx, state.playerName); err != nil {
				return err
			}
		}
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
	case "EventSetDeckV2", "EventSetDeckV3":
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
		state.rememberEventDeck(req.EventName, req.Summary.DeckID)
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
			linked := false
			if arenaDeckID := state.eventDeck(eventName); arenaDeckID != "" {
				linked, _ = p.store.LinkMatchToDeckByArenaDeckID(ctx, tx, evt.MatchID, arenaDeckID, "event_deck")
			}
			if !linked {
				_ = p.store.LinkMatchToLatestDeckByEvent(ctx, tx, evt.MatchID, eventName, "pre_match")
			}
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
			if err := p.archiveCompletedMatchReplay(ctx, tx, evt.MatchID, result); err != nil {
				return err
			}
		}
	}

	return nil
}
