package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
)

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
type botDraftStatusResponse struct {
	EventName  string   `json:"EventName"`
	PackNumber int64    `json:"PackNumber"`
	PickNumber int64    `json:"PickNumber"`
	DraftPack  []string `json:"DraftPack"`
}

func decodeBotDraftStatusResponse(line string) (botDraftStatusResponse, bool) {
	payload := []byte(line)
	var envelope struct {
		Payload json.RawMessage `json:"Payload"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Payload) > 0 {
		decoded, err := decodeRawRequest(envelope.Payload)
		if err != nil {
			return botDraftStatusResponse{}, false
		}
		payload = decoded
	}

	var response botDraftStatusResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return botDraftStatusResponse{}, false
	}
	if strings.TrimSpace(response.EventName) == "" ||
		response.PackNumber < 0 ||
		response.PickNumber < 0 ||
		len(response.DraftPack) == 0 {
		return botDraftStatusResponse{}, false
	}
	return response, true
}

func (p *Parser) handleBotDraftStatusResponse(
	ctx context.Context,
	tx *sql.Tx,
	line string,
	observedAt string,
) error {
	response, ok := decodeBotDraftStatusResponse(line)
	if !ok {
		return nil
	}

	sessionID, err := p.store.EnsureDraftSession(ctx, tx, response.EventName, nil, true, observedAt)
	if err != nil {
		return err
	}
	return p.store.InsertDraftPick(
		ctx,
		tx,
		sessionID,
		response.PackNumber,
		response.PickNumber,
		nil,
		parseStringIDsToInt64(response.DraftPack),
		observedAt,
	)
}

const draftNotifyPrefix = "[UnityCrossThreadLogger]Draft.Notify "

// draftNotify is Arena's player-draft pack snapshot, logged before each pick.
// SelfPack/SelfPick are 1-based, matching EventPlayerDraftMakePick.
type draftNotify struct {
	DraftID   string `json:"draftId"`
	SelfPack  int64  `json:"SelfPack"`
	SelfPick  int64  `json:"SelfPick"`
	PackCards string `json:"PackCards"`
}

func (p *Parser) handleDraftNotify(ctx context.Context, tx *sql.Tx, payload, observedAt string) error {
	var notify draftNotify
	if err := json.Unmarshal([]byte(payload), &notify); err != nil {
		return nil
	}
	packIDs := parseStringIDsToInt64(strings.Split(notify.PackCards, ","))
	if notify.DraftID == "" || notify.SelfPack < 1 || notify.SelfPick < 1 || len(packIDs) == 0 {
		return nil
	}

	draftID := notify.DraftID
	sessionID, err := p.store.EnsureDraftSession(ctx, tx, "", &draftID, false, observedAt)
	if err != nil {
		return err
	}
	return p.store.InsertDraftPick(ctx, tx, sessionID, notify.SelfPack, notify.SelfPick, nil, packIDs, observedAt)
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
