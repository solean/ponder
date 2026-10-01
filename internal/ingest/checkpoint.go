package ingest

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

const parserCheckpointVersion = 1

// The live loop normally polls only Player.log. Save the renamed file's cursor
// before overwriting the original path's checkpoint, so a later startup can
// import Player-prev.log without replaying already-consumed rank responses.
func (p *Parser) preserveRotatedCheckpoint(ctx context.Context, tx *sql.Tx, logPath string, prior db.IngestState) error {
	if filepath.Base(logPath) != "Player.log" || prior.Offset <= 0 || prior.FileSignature == "" {
		return nil
	}
	previousPath := filepath.Join(filepath.Dir(logPath), "Player-prev.log")
	file, err := os.Open(previousPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open rotated log: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < prior.Offset {
		return nil
	}
	signature, err := ingestFileSignature(file, prior.Offset)
	if err != nil {
		return err
	}
	if signature != prior.FileSignature {
		return nil
	}
	previous, err := p.store.GetIngestCursor(ctx, previousPath)
	if err != nil {
		return err
	}
	if previous.Found && previous.Offset == 0 && previous.FileSignature != "" {
		// Keep explicit backfill requests intact.
		return nil
	}
	if previous.Offset >= prior.Offset && previous.Offset <= info.Size() {
		signature, err := ingestFileSignature(file, previous.Offset)
		if err != nil {
			return err
		}
		if signature == previous.FileSignature {
			return nil
		}
	}
	prior, err = p.store.GetIngestState(ctx, logPath)
	if err != nil {
		return err
	}
	return p.store.SaveIngestCheckpoint(ctx, tx, previousPath, prior.Offset, prior.LineNo, prior.FileSignature, prior.ContextJSON, "")
}

// parseCheckpoint includes every field used to interpret a later line. Replay
// internals include hidden objects and zone membership that cannot be recovered
// from public database frames alone. Cursor and context commit together.
type parseCheckpoint struct {
	Version                   int
	PersonaID                 string
	PlayerName                string
	ActiveMatchID             string
	SelfSeatByMatch           map[string]int64
	TurnByMatch               map[string]int64
	ActivePlayerByMatch       map[string]int64
	PhaseByMatch              map[string]string
	ZoneTypeByMatch           map[string]map[int64]string
	ZoneVisibilityByMatch     map[string]map[int64]string
	ZoneOwnerSeatByMatch      map[string]map[int64]int64
	GameNumberByMatch         map[string]int64
	DeckByEvent               map[string]string
	ReplayByMatchGame         map[string]*replayPublicState
	LastUnityLogTimestamp     string
	PendingResponseMethod     string
	PendingResponseRequestID  string
	PendingResponseObservedAt string
	CollectingClientGREJSON   bool
	PendingGameDeckSnapshot   *pendingGameDeckSnapshot
	ClientGREJSON             string
}

func encodeParseCheckpoint(s *parseState) (string, error) {
	wire := parseCheckpoint{
		Version:                   parserCheckpointVersion,
		PersonaID:                 s.personaID,
		PlayerName:                s.playerName,
		ActiveMatchID:             s.activeMatchID,
		SelfSeatByMatch:           s.selfSeatByMatch,
		TurnByMatch:               s.turnByMatch,
		ActivePlayerByMatch:       s.activePlayerByMatch,
		PhaseByMatch:              s.phaseByMatch,
		ZoneTypeByMatch:           s.zoneTypeByMatch,
		ZoneVisibilityByMatch:     s.zoneVisibilityByMatch,
		ZoneOwnerSeatByMatch:      s.zoneOwnerSeatByMatch,
		GameNumberByMatch:         s.gameNumberByMatch,
		DeckByEvent:               s.deckByEvent,
		ReplayByMatchGame:         s.replayByMatchGame,
		LastUnityLogTimestamp:     s.lastUnityLogTimestamp,
		PendingResponseMethod:     s.pendingResponseMethod,
		PendingResponseRequestID:  s.pendingResponseRequestID,
		PendingResponseObservedAt: s.pendingResponseObservedAt,
		CollectingClientGREJSON:   s.collectingClientGREJSON,
		PendingGameDeckSnapshot:   s.pendingGameDeckSnapshot,
		ClientGREJSON:             s.clientGREJSON.String(),
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if err := json.NewEncoder(writer).Encode(wire); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return "gzip:" + base64.StdEncoding.EncodeToString(compressed.Bytes()), nil
}

func decodeParseCheckpoint(data string) (*parseState, bool) {
	var wire parseCheckpoint
	payload := []byte(data)
	if strings.HasPrefix(data, "gzip:") {
		packed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, "gzip:"))
		if err != nil {
			return nil, false
		}
		reader, err := gzip.NewReader(bytes.NewReader(packed))
		if err != nil {
			return nil, false
		}
		defer reader.Close()
		payload, err = io.ReadAll(reader)
		if err != nil {
			return nil, false
		}
	}
	if json.Unmarshal(payload, &wire) != nil || wire.Version != parserCheckpointVersion {
		return nil, false
	}
	state := &parseState{
		personaID:                 wire.PersonaID,
		playerName:                wire.PlayerName,
		activeMatchID:             wire.ActiveMatchID,
		selfSeatByMatch:           wire.SelfSeatByMatch,
		turnByMatch:               wire.TurnByMatch,
		activePlayerByMatch:       wire.ActivePlayerByMatch,
		phaseByMatch:              wire.PhaseByMatch,
		zoneTypeByMatch:           wire.ZoneTypeByMatch,
		zoneVisibilityByMatch:     wire.ZoneVisibilityByMatch,
		zoneOwnerSeatByMatch:      wire.ZoneOwnerSeatByMatch,
		gameNumberByMatch:         wire.GameNumberByMatch,
		deckByEvent:               wire.DeckByEvent,
		replayByMatchGame:         wire.ReplayByMatchGame,
		lastUnityLogTimestamp:     wire.LastUnityLogTimestamp,
		pendingResponseMethod:     wire.PendingResponseMethod,
		pendingResponseRequestID:  wire.PendingResponseRequestID,
		pendingResponseObservedAt: wire.PendingResponseObservedAt,
		collectingClientGREJSON:   wire.CollectingClientGREJSON,
		pendingGameDeckSnapshot:   wire.PendingGameDeckSnapshot,
	}
	state.clientGREJSON.WriteString(wire.ClientGREJSON)
	return state, true
}

type parserCheckpoint struct {
	Version                 int
	PersonaID               string
	PlayerName              string
	PendingCompletedMatches []string
}

func (p *Parser) loadParserCheckpoint(ctx context.Context) error {
	data, err := p.store.ParserCheckpoint(ctx)
	if err != nil {
		return err
	}
	var wire parserCheckpoint
	if data == "" {
		wire.PlayerName, err = p.store.PlayerName(ctx)
		if err != nil {
			return err
		}
	}
	if data != "" {
		if err := json.Unmarshal([]byte(data), &wire); err != nil {
			return fmt.Errorf("decode parser checkpoint: %w", err)
		}
		if wire.Version != parserCheckpointVersion {
			return fmt.Errorf("unsupported parser checkpoint version %d", wire.Version)
		}
	}
	// Reload even on a reused parser: a cancelled transaction may have changed
	// its in-memory queue or identity after the last successful commit.
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.personaID = wire.PersonaID
	p.pendingCompletedMatches = wire.PendingCompletedMatches
	p.playerName = wire.PlayerName
	return nil
}

func (p *Parser) encodeParserCheckpoint() (string, error) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	data, err := json.Marshal(parserCheckpoint{
		Version:                 parserCheckpointVersion,
		PersonaID:               p.personaID,
		PlayerName:              p.playerName,
		PendingCompletedMatches: p.pendingCompletedMatches,
	})
	return string(data), err
}

// Old checkpoints contain only a cursor. Rebuild that prefix once under a
// rollback-only transaction; handlers can keep their usual database semantics
// without committing duplicate raw events or changing historical frames. A
// failed/cancelled reconstruction leaves the old durable checkpoint intact.
func (p *Parser) reconstructCheckpoint(ctx context.Context, file *os.File, logPath string, progress db.IngestState, state *parseState) error {
	tx, err := p.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin checkpoint reconstruction: %w", err)
	}
	defer tx.Rollback()
	durableGlobals, err := p.store.ParserCheckpoint(ctx)
	if err != nil {
		return err
	}
	p.stateMu.Lock()
	priorPersona, priorName := p.personaID, p.playerName
	priorQueue := append([]string(nil), p.pendingCompletedMatches...)
	// Historical responses must not consume pending matches from another file.
	p.pendingCompletedMatches = nil
	p.stateMu.Unlock()
	if durableGlobals != "" {
		defer func() {
			p.stateMu.Lock()
			defer p.stateMu.Unlock()
			p.personaID, p.playerName = priorPersona, priorName
			p.pendingCompletedMatches = priorQueue
		}()
	}
	state.reconstructingCheckpoint = true
	defer func() { state.reconstructingCheckpoint = false }()
	reader := bufio.NewReaderSize(io.NewSectionReader(file, 0, progress.Offset), 4*1024*1024)
	var offset, lineNo int64
	stats := model.ParseStats{}
	for offset < progress.Offset {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("read checkpoint prefix: %w", err)
		}
		if len(line) == 0 {
			return fmt.Errorf("checkpoint prefix ended before offset %d", progress.Offset)
		}
		lineNo++
		if err := p.processLine(ctx, tx, &stats, state, logPath, lineNo, offset, line); err != nil {
			return fmt.Errorf("reconstruct checkpoint line %d: %w", lineNo, err)
		}
		offset += int64(len(line))
	}
	if lineNo != progress.LineNo {
		return fmt.Errorf("checkpoint prefix line count %d does not match saved %d", lineNo, progress.LineNo)
	}
	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("rollback checkpoint reconstruction: %w", err)
	}
	return nil
}
