package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/solean/ponder/internal/ai"
	"github.com/solean/ponder/internal/model"
)

var errReviewGameNotFound = errors.New("game not found")

func (s *Server) handleGameReview(w http.ResponseWriter, r *http.Request, matchID int64) {
	gameNumber, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("game")), 10, 64)
	if err != nil || gameNumber <= 0 {
		writeError(w, http.StatusBadRequest, "invalid game number")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGameReviewGet(w, r, matchID, gameNumber)
	case http.MethodPost:
		s.handleGameReviewGenerate(w, r, matchID, gameNumber)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGameReviewGet(w http.ResponseWriter, r *http.Request, matchID, gameNumber int64) {
	review, err := s.store.GetGameReview(r.Context(), matchID, gameNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if review == nil {
		writeError(w, http.StatusNotFound, "no review generated for this game")
		return
	}

	input, err := s.loadGameReviewInput(r.Context(), matchID, gameNumber)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errReviewGameNotFound) {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	review.Stale = ai.GameReviewSourceHash(input) != review.SourceHash
	writeJSON(w, http.StatusOK, review)
}

func (s *Server) handleGameReviewGenerate(w http.ResponseWriter, r *http.Request, matchID, gameNumber int64) {
	if !s.aiGenBusy.TryLock() {
		writeError(w, http.StatusConflict, "another AI generation is already running")
		return
	}
	defer s.aiGenBusy.Unlock()

	provider, modelName := s.aiSelection()
	if status := s.aiProvider.ProviderStatus(r.Context(), provider); !status.Available {
		writeError(w, http.StatusServiceUnavailable, status.Detail)
		return
	}

	input, err := s.loadGameReviewInput(r.Context(), matchID, gameNumber)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errReviewGameNotFound) {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(input.Frames) == 0 {
		writeError(w, http.StatusBadRequest, "game has no replay frames to review")
		return
	}

	flush := func() {}
	if flusher, ok := w.(http.Flusher); ok {
		flush = flusher.Flush
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx, cancel := context.WithTimeout(r.Context(), aiGenerateTimeout)
	defer cancel()
	prompt := ai.BuildGameReviewPrompt(input)
	generation, err := streamAIGeneration(
		ctx,
		w,
		flush,
		aiHeartbeatInterval,
		func(onDelta func(string)) (ai.GenerationResult, error) {
			return s.aiProvider.Generate(ctx, provider, modelName, prompt, onDelta)
		},
	)

	if generation.Usage.HasTokens() {
		usageSubjectID := int64(0)
		if input.Match.DeckID != nil {
			usageSubjectID = *input.Match.DeckID
		}
		usageCtx, usageCancel := context.WithTimeout(context.Background(), 10*time.Second)
		usageErr := s.store.RecordAIUsage(
			usageCtx,
			usageSubjectID,
			provider,
			ai.NormalizeModel(provider, modelName),
			generation.Usage,
			err == nil,
		)
		usageCancel()
		if usageErr != nil {
			log.Printf("AI usage record failed (match_id=%d game=%d provider=%s model=%s): %v", matchID, gameNumber, provider, modelName, usageErr)
		}
	}

	if err != nil {
		log.Printf("AI game review generation failed (match_id=%d game=%d provider=%s model=%s): %v", matchID, gameNumber, provider, modelName, err)
		if ctx.Err() == nil {
			if sendErr := writeSSEEvent(w, flush, "error", map[string]string{"error": err.Error()}); sendErr != nil {
				log.Printf("AI game review error event failed (match_id=%d game=%d): %v", matchID, gameNumber, sendErr)
			}
		}
		return
	}

	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()
	review, err := s.store.UpsertGameReview(
		saveCtx,
		matchID,
		gameNumber,
		ai.GameReviewSourceHash(input),
		ai.GenerationModel(provider, modelName),
		generation.Content,
	)
	if err != nil {
		log.Printf("AI game review save failed (match_id=%d game=%d provider=%s model=%s): %v", matchID, gameNumber, provider, modelName, err)
		if sendErr := writeSSEEvent(w, flush, "error", map[string]string{"error": err.Error()}); sendErr != nil {
			log.Printf("AI game review save error event failed (match_id=%d game=%d): %v", matchID, gameNumber, sendErr)
		}
		return
	}
	if err := writeSSEEvent(w, flush, "done", review); err != nil {
		log.Printf("AI game review done event failed (match_id=%d game=%d): %v", matchID, gameNumber, err)
	}
}

func (s *Server) loadGameReviewInput(ctx context.Context, matchID, gameNumber int64) (ai.GameReviewInput, error) {
	if err := s.store.EnsureMatchAnalytics(ctx, matchID); err != nil {
		return ai.GameReviewInput{}, fmt.Errorf("prepare match analytics: %w", err)
	}
	detail, err := s.store.GetMatchDetail(ctx, matchID)
	if err != nil {
		return ai.GameReviewInput{}, err
	}
	s.enrichGameCardNames(ctx, detail.Games)

	var game *model.GameRow
	for index := range detail.Games {
		if detail.Games[index].GameNumber == gameNumber {
			game = &detail.Games[index]
			break
		}
	}
	if game == nil {
		return ai.GameReviewInput{}, errReviewGameNotFound
	}

	allFrames, err := s.store.ListMatchReplayFrames(ctx, matchID)
	if err != nil {
		return ai.GameReviewInput{}, fmt.Errorf("load match replay: %w", err)
	}
	frames := make([]model.MatchReplayFrameRow, 0, len(allFrames))
	for _, frame := range allFrames {
		frameGameNumber := int64(1)
		if frame.GameNumber != nil && *frame.GameNumber > 0 {
			frameGameNumber = *frame.GameNumber
		}
		if frameGameNumber == gameNumber {
			frames = append(frames, frame)
		}
	}
	s.enrichMatchReplayNames(ctx, frames)

	var deckCards []model.DeckCardRow
	if detail.Match.DeckID != nil {
		deck, deckErr := s.store.GetDeckDetail(ctx, *detail.Match.DeckID, 1)
		if deckErr != nil {
			log.Printf("AI game review deck load failed (match_id=%d deck_id=%d): %v", matchID, *detail.Match.DeckID, deckErr)
		} else {
			deckCards = deck.Cards
			if detail.Match.DeckVersionID != nil {
				for _, version := range deck.Versions {
					if version.ID == *detail.Match.DeckVersionID {
						deckCards = version.Cards
						break
					}
				}
			}
			deckCards = append([]model.DeckCardRow(nil), deckCards...)
			s.enrichDeckCardNames(ctx, deckCards)
		}
	}

	return ai.GameReviewInput{
		Match:     detail.Match,
		Game:      *game,
		Frames:    frames,
		DeckCards: deckCards,
	}, nil
}
