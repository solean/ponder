package ingest

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

type inventoryInfoPayload struct {
	SequenceID            int64           `json:"SeqId"`
	Changes               json.RawMessage `json:"Changes"`
	Gems                  int64           `json:"Gems"`
	Gold                  int64           `json:"Gold"`
	VaultProgress         int64           `json:"TotalVaultProgress"`
	WildcardTrackPosition int64           `json:"wcTrackPosition"`
	WildcardCommons       int64           `json:"WildCardCommons"`
	WildcardUncommons     int64           `json:"WildCardUnCommons"`
	WildcardRares         int64           `json:"WildCardRares"`
	WildcardMythics       int64           `json:"WildCardMythics"`
	CustomTokens          json.RawMessage `json:"CustomTokens"`
	Boosters              json.RawMessage `json:"Boosters"`
	Vouchers              json.RawMessage `json:"Vouchers"`
}

type eventCoursePayload struct {
	CourseID          string `json:"CourseId"`
	InternalEventName string `json:"InternalEventName"`
	CurrentModule     string `json:"CurrentModule"`
	CurrentWins       int64  `json:"CurrentWins"`
	CurrentLosses     int64  `json:"CurrentLosses"`
	CourseDeckSummary struct {
		DeckID string `json:"DeckId"`
	} `json:"CourseDeckSummary"`
}

// Arena reports inventory either nested under "InventoryInfo" or, for module
// responses like draft card-pool grants, as a top-level "DTO_InventoryInfo".
type economyEnvelope struct {
	eventCoursePayload
	Course           *eventCoursePayload   `json:"Course"`
	Courses          []eventCoursePayload  `json:"Courses"`
	InventoryInfo    *inventoryInfoPayload `json:"InventoryInfo"`
	DTOInventoryInfo *inventoryInfoPayload `json:"DTO_InventoryInfo"`
}

func (p *Parser) handleCourseAndEconomyJSON(
	ctx context.Context,
	tx *sql.Tx,
	stats *model.ParseStats,
	state *parseState,
	logPath string,
	lineNo int64,
	line string,
) error {
	var envelope economyEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return nil
	}
	// CourseId is the pay/reward source identity. Persist it before deriving
	// inventory changes, including when the response carries no inventory.
	if err := p.upsertEventCourse(ctx, tx, envelope.eventCoursePayload, state.lastUnityLogTimestamp); err != nil {
		return err
	}
	if envelope.Course != nil {
		if err := p.upsertEventCourse(ctx, tx, *envelope.Course, state.lastUnityLogTimestamp); err != nil {
			return err
		}
	}
	for _, course := range envelope.Courses {
		if err := p.upsertEventCourse(ctx, tx, course, state.lastUnityLogTimestamp); err != nil {
			return err
		}
	}
	inventory := envelope.InventoryInfo
	if inventory == nil {
		inventory = envelope.DTOInventoryInfo
	}
	if inventory == nil {
		return nil
	}
	snapshotID, inserted, err := p.store.InsertEconomySnapshot(ctx, tx, logPath, lineNo, db.EconomySnapshotRecord{
		ObservedAt:            state.lastUnityLogTimestamp,
		SequenceID:            inventory.SequenceID,
		Gold:                  inventory.Gold,
		Gems:                  inventory.Gems,
		VaultProgress:         inventory.VaultProgress,
		WildcardTrackPosition: inventory.WildcardTrackPosition,
		WildcardCommons:       inventory.WildcardCommons,
		WildcardUncommons:     inventory.WildcardUncommons,
		WildcardRares:         inventory.WildcardRares,
		WildcardMythics:       inventory.WildcardMythics,
		CustomTokensJSON:      string(inventory.CustomTokens),
		BoostersJSON:          string(inventory.Boosters),
		VouchersJSON:          string(inventory.Vouchers),
		ChangesJSON:           string(inventory.Changes),
	})
	if err != nil {
		return err
	}
	if inserted {
		stats.EconomySnapshots++
		if _, err := p.store.DeriveEconomyTransactions(
			ctx, tx, snapshotID, state.lastUnityLogTimestamp, string(inventory.Changes),
		); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) upsertEventCourse(ctx context.Context, tx *sql.Tx, course eventCoursePayload, observedAt string) error {
	if course.CourseID == "" {
		return nil
	}
	return p.store.UpsertEventCourse(ctx, tx, db.EventCourseRecord{
		CourseID:      course.CourseID,
		EventName:     course.InternalEventName,
		DeckID:        course.CourseDeckSummary.DeckID,
		CurrentModule: course.CurrentModule,
		Wins:          course.CurrentWins,
		Losses:        course.CurrentLosses,
	}, observedAt)
}
