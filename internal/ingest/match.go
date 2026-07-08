package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/cschnabel/mtgdata/internal/model"
)

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

// archiveCompletedMatchReplay compacts a finished match's replay frame rows
// into the compressed archive so the row tables only ever hold live matches.
func (p *Parser) archiveCompletedMatchReplay(ctx context.Context, tx *sql.Tx, arenaMatchID, result string) error {
	if result != "win" && result != "loss" && result != "draw" {
		return nil
	}
	_, err := p.store.ArchiveMatchReplay(ctx, tx, arenaMatchID)
	return err
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
			if playerName != "" && playerName != state.playerName {
				state.playerName = playerName
				if p.rememberPlayerName(playerName) {
					if err := p.store.SavePlayerName(ctx, tx, playerName); err != nil {
						return err
					}
				}
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
		linked := false
		if arenaDeckID := state.eventDeck(eventName); arenaDeckID != "" {
			linked, _ = p.store.LinkMatchToDeckByArenaDeckID(ctx, tx, config.MatchID, arenaDeckID, "event_deck")
		}
		if !linked {
			_ = p.store.LinkMatchToLatestDeckByEvent(ctx, tx, config.MatchID, eventName, "room_state")
		}
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
			} else if err := p.archiveCompletedMatchReplay(ctx, tx, config.MatchID, result); err != nil {
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
