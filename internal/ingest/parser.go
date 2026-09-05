package ingest

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
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
	collectingClientGREJSON   bool
	clientGREJSON             strings.Builder
	pendingGameDeckSnapshot   *pendingGameDeckSnapshot
}

type pendingGameDeckSnapshot struct {
	MainCardIDs      []int64
	SideboardCardIDs []int64
	ObservedAt       string
	Source           string
	ArenaMatchID     string
	ExpectedGame     int64
}

const maxClientGREJSONBytes = 4 * 1024 * 1024

func (s *parseState) beginClientGREJSON() {
	s.collectingClientGREJSON = true
	s.clientGREJSON.Reset()
}

func (s *parseState) clearClientGREJSON() {
	s.collectingClientGREJSON = false
	s.clientGREJSON.Reset()
}

func (s *parseState) ingestCheckpoint(byteOffset, lineNo int64) (int64, int64) {
	if s.collectingClientGREJSON || s.pendingGameDeckSnapshot != nil {
		// The match/game identity used to scope a deck submission also lives in
		// parseState. Rewind the durable cursor while a logical record is being
		// collected or is waiting for its matching full game state, so a process
		// restart rebuilds both that context and the pending deck before resuming.
		return 0, 0
	}
	return byteOffset, lineNo
}

func (s *parseState) rememberPendingGameDeckSnapshot(
	deck greDeck,
	observedAt, source, arenaMatchID string,
	expectedGame int64,
) {
	if len(deck.DeckCards) == 0 {
		return
	}
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return
	}
	s.pendingGameDeckSnapshot = &pendingGameDeckSnapshot{
		MainCardIDs:      append([]int64(nil), deck.DeckCards...),
		SideboardCardIDs: append([]int64(nil), deck.SideboardCards...),
		ObservedAt:       strings.TrimSpace(observedAt),
		Source:           strings.TrimSpace(source),
		ArenaMatchID:     arenaMatchID,
		ExpectedGame:     expectedGame,
	}
}

func (s *parseState) activateMatch(arenaMatchID string) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return
	}
	if pending := s.pendingGameDeckSnapshot; pending != nil && pending.ArenaMatchID != arenaMatchID {
		s.pendingGameDeckSnapshot = nil
	}
	s.activeMatchID = arenaMatchID
}

func (s *parseState) pendingGameDeckMatches(arenaMatchID string, gameNumber int64) bool {
	pending := s.pendingGameDeckSnapshot
	if pending == nil || strings.TrimSpace(arenaMatchID) != pending.ArenaMatchID {
		return false
	}
	return pending.ExpectedGame <= 0 || pending.ExpectedGame == gameNumber
}

func (s *parseState) clearPendingGameDeckForMatch(arenaMatchID string) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if pending := s.pendingGameDeckSnapshot; pending != nil && pending.ArenaMatchID == arenaMatchID {
		s.pendingGameDeckSnapshot = nil
	}
}

func isClientToGREMessageHeader(line string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(line)), " to match: clienttogremessage")
}

// appendClientGREJSON collects Arena's pretty-printed ClientToGRE payload.
// json.Valid is deliberately used as the terminator: brace counting is easy to
// fool with strings, while these payloads are small enough to validate per line.
func (s *parseState) appendClientGREJSON(line string) ([]byte, bool) {
	if !s.collectingClientGREJSON {
		return nil, false
	}
	trimmed := strings.TrimSpace(line)
	if s.clientGREJSON.Len() == 0 && !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	if s.clientGREJSON.Len() > 0 {
		s.clientGREJSON.WriteByte('\n')
	}
	s.clientGREJSON.WriteString(line)
	if s.clientGREJSON.Len() > maxClientGREJSONBytes {
		s.clearClientGREJSON()
		return nil, false
	}

	payload := []byte(s.clientGREJSON.String())
	if !json.Valid(payload) {
		return nil, false
	}
	out := append([]byte(nil), payload...)
	s.clearClientGREJSON()
	return out, true
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
	if s.gameNumberByMatch[matchID] != gameNumber {
		// Arena stops restating turnInfo between games, so a remembered turn,
		// phase, and active player describe the game that just ended. Dropping
		// them keeps the next game's pre-game frames (its mulligan sequence)
		// turnless instead of stamping them with the previous game's last turn.
		delete(s.turnByMatch, matchID)
		delete(s.phaseByMatch, matchID)
		delete(s.activePlayerByMatch, matchID)
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

type clientToGREEnvelope struct {
	Payload *struct {
		Type           string `json:"type"`
		SubmitDeckResp *struct {
			Deck greDeck `json:"deck"`
		} `json:"submitDeckResp"`
	} `json:"payload"`
}

const ingestSignatureWindowBytes int64 = 4096

// ingestFileSignature fingerprints the bytes immediately before a durable
// cursor. Appends leave that window unchanged; truncation or log rotation does
// not, even when the replacement file has already grown past the old offset.
func ingestFileSignature(file *os.File, offset int64) (string, error) {
	if offset <= 0 {
		return "", nil
	}
	windowSize := min(offset, ingestSignatureWindowBytes)
	start := offset - windowSize
	var window [ingestSignatureWindowBytes]byte
	n, err := file.ReadAt(window[:windowSize], start)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if int64(n) != windowSize {
		return "", io.ErrUnexpectedEOF
	}
	sum := sha256.Sum256(window[:n])
	return hex.EncodeToString(sum[:]), nil
}

func (p *Parser) ParseFile(ctx context.Context, logPath string, resume bool) (model.ParseStats, error) {
	stats := model.ParseStats{LogPath: logPath, StartedAt: time.Now().UTC()}

	startOffset := int64(0)
	startLine := int64(0)
	savedFileSignature := ""
	resetState := !resume
	if resume {
		ingestState, err := p.store.GetIngestState(ctx, logPath)
		if err != nil {
			return stats, err
		}
		if ingestState.Found {
			startOffset = ingestState.Offset
			startLine = ingestState.LineNo
			savedFileSignature = ingestState.FileSignature
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

	// MTGA rotates/truncates Player.log and replaces Player-prev.log. A size
	// regression catches truncation; the saved cursor-window signature catches
	// replacement files that have already grown beyond the previous offset.
	if startOffset > info.Size() {
		startOffset = 0
		startLine = 0
		resetState = true
	} else if startOffset > 0 {
		currentFileSignature, signatureErr := ingestFileSignature(file, startOffset)
		if signatureErr != nil {
			return stats, fmt.Errorf("fingerprint log file at offset %d: %w", startOffset, signatureErr)
		}
		if savedFileSignature == "" || currentFileSignature != savedFileSignature {
			startOffset = 0
			startLine = 0
			resetState = true
		}
	}

	// Only a validated non-zero cursor can skip parsing: zero may be pinned
	// for logical-record recovery. Check the signature above even at EOF so
	// same-size replacements are not mistaken for an unchanged log.
	if startOffset > 0 && startOffset == info.Size() {
		stats.CompletedAt = time.Now().UTC()
		return stats, nil
	}

	// A zero cursor can represent either a first import, a schema backfill, or
	// recovery of a pending logical record. Keep intermediate batch commits at
	// zero for this pass so a crash before the deck submission is reconstructed
	// cannot strand the next restart in the middle of the log. The final commit
	// advances to the safe tail once no collector or deck snapshot remains pending.
	pinCheckpointUntilFinalCommit := startOffset == 0 && startLine == 0

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
		checkpointOffset, checkpointLine := state.ingestCheckpoint(byteOffset, lineNo)
		if pinCheckpointUntilFinalCommit {
			checkpointOffset, checkpointLine = 0, 0
		}
		fileSignature, signatureErr := ingestFileSignature(file, checkpointOffset)
		if signatureErr != nil {
			return fmt.Errorf("fingerprint log checkpoint at offset %d: %w", checkpointOffset, signatureErr)
		}
		if err := p.store.SaveIngestState(ctx, tx, logPath, checkpointOffset, checkpointLine, fileSignature); err != nil {
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
		// A live read can land in the middle of Arena writing a line. Leave an
		// unterminated tail at its original cursor so the next poll rereads the
		// complete line instead of splitting a JSON token across parser calls.
		if resume && errors.Is(readErr, io.EOF) {
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

	checkpointOffset, checkpointLine := state.ingestCheckpoint(byteOffset, lineNo)
	fileSignature, err := ingestFileSignature(file, checkpointOffset)
	if err != nil {
		return stats, fmt.Errorf("fingerprint final log checkpoint at offset %d: %w", checkpointOffset, err)
	}
	if err := p.store.SaveIngestState(ctx, tx, logPath, checkpointOffset, checkpointLine, fileSignature); err != nil {
		return stats, err
	}
	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit final tx: %w", err)
	}

	// Raw events are only stored when draft repair can consume them, so their
	// presence is the trigger to backfill draft metadata. Running here keeps
	// the repair scans off the API read path.
	if stats.RawEventsStored > 0 || stats.DraftPicksAdded > 0 {
		if err := p.store.RepairDraftDataFromRawEvents(ctx); err != nil {
			return stats, fmt.Errorf("repair draft data after ingest: %w", err)
		}
		if err := p.store.RepairEventRunInstances(ctx); err != nil {
			return stats, fmt.Errorf("repair event runs after ingest: %w", err)
		}
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

	// ClientToGRE deck submissions are emitted as a header followed by a
	// pretty-printed JSON object. Abort an incomplete object at the next Unity
	// record so a truncated payload cannot swallow the rest of the log.
	if state.collectingClientGREJSON && strings.HasPrefix(line, "[UnityCrossThreadLogger]") {
		state.clearClientGREJSON()
	}
	if isClientToGREMessageHeader(line) {
		state.beginClientGREJSON()
		return nil
	}
	if state.collectingClientGREJSON {
		payload, complete := state.appendClientGREJSON(line)
		if !complete {
			return nil
		}
		return p.handleClientToGREJSON(payload, state)
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

	if strings.HasPrefix(line, "{") &&
		(strings.Contains(line, "\"InventoryInfo\"") || strings.Contains(line, "\"DTO_InventoryInfo\"")) {
		if err := p.handleEconomyJSON(ctx, tx, stats, state, logPath, lineNo, line); err != nil {
			return err
		}
		return nil
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
		if stored, err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "method_complete", m[1], m[2], nil, ""); err != nil {
			return err
		} else if stored {
			stats.RawEventsStored++
		}
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

func (p *Parser) handleClientToGREJSON(payload []byte, state *parseState) error {
	if state == nil || len(payload) == 0 {
		return nil
	}

	var env clientToGREEnvelope
	if err := json.Unmarshal(payload, &env); err != nil || env.Payload == nil || env.Payload.SubmitDeckResp == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(env.Payload.Type), "ClientMessageType_SubmitDeckResp") {
		return nil
	}

	expectedGame := state.gameNumber(state.activeMatchID)
	if expectedGame > 0 {
		expectedGame++
	}
	state.rememberPendingGameDeckSnapshot(
		env.Payload.SubmitDeckResp.Deck,
		state.lastUnityLogTimestamp,
		"gre_submit_deck",
		state.activeMatchID,
		expectedGame,
	)
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
		if stored, err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "outgoing_unparsed", method, "", nil, ""); err != nil {
			return err
		} else if stored {
			stats.RawEventsStored++
		}
		return nil
	}

	requestPayload, err := decodeRawRequest(env.Request)
	if err != nil {
		return fmt.Errorf("decode raw request for %s: %w", method, err)
	}

	if stored, err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "outgoing", method, env.ID, requestPayload, ""); err != nil {
		return err
	} else if stored {
		stats.RawEventsStored++
	}
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
			state.activateMatch(evt.MatchID)
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
			state.clearPendingGameDeckForMatch(evt.MatchID)
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
