package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/solean/ponder/internal/ai"
	"github.com/solean/ponder/internal/db"
)

const (
	// aiGenerateTimeout bounds a single primer generation. Subscription-backed
	// CLI runs with web search can legitimately take a few minutes.
	aiGenerateTimeout = 5 * time.Minute
	// Codex emits its final agent message as one JSONL item rather than token
	// deltas. Keep the HTTP body active while it works so WebKit does not treat
	// the otherwise-silent SSE request as a failed load.
	aiHeartbeatInterval = 10 * time.Second
)

func (s *Server) aiSelection() (string, string) {
	if s.appState == nil {
		return ai.DefaultProvider, ai.DefaultClaudeModel
	}
	config := s.appState.Config()
	return config.AIProvider, config.AIModel
}

type aiStatusResponse struct {
	ai.Status
	Usage      db.AIUsageSummary `json:"usage"`
	UsageError string            `json:"usageError,omitempty"`
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	provider, model := s.aiSelection()
	status := s.aiProvider.Status(r.Context(), provider, model)
	usage, err := s.store.GetAIUsageSummary(r.Context())
	response := aiStatusResponse{Status: status, Usage: usage}
	if err != nil {
		log.Printf("AI usage summary failed: %v", err)
		response.UsageError = err.Error()
	}
	writeJSON(w, http.StatusOK, response)
}

// handleDeckPrimer serves GET (cached primer) and POST (generate + stream)
// for /api/decks/{id}/primer.
func (s *Server) handleDeckPrimer(w http.ResponseWriter, r *http.Request, deckID int64) {
	switch r.Method {
	case http.MethodGet:
		s.handleDeckPrimerGet(w, r, deckID)
	case http.MethodPost:
		s.handleDeckPrimerGenerate(w, r, deckID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDeckPrimerGet(w http.ResponseWriter, r *http.Request, deckID int64) {
	primer, err := s.store.GetDeckPrimer(r.Context(), deckID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if primer == nil {
		writeError(w, http.StatusNotFound, "no primer generated for this deck")
		return
	}
	if detail, err := s.store.GetDeckDetail(r.Context(), deckID, 1); err == nil {
		primer.Stale = ai.CardsHash(detail.Cards) != primer.CardsHash
	}
	writeJSON(w, http.StatusOK, primer)
}

type aiGenerationResult struct {
	generation ai.GenerationResult
	err        error
}

func writeSSEEvent(w io.Writer, flush func(), event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", event, err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return fmt.Errorf("write %s event: %w", event, err)
	}
	flush()
	return nil
}

func writeSSEComment(w io.Writer, flush func(), comment string) error {
	if _, err := fmt.Fprintf(w, ": %s\n\n", comment); err != nil {
		return fmt.Errorf("write SSE heartbeat: %w", err)
	}
	flush()
	return nil
}

// streamAIGeneration owns every response write. The provider runs separately
// so heartbeats and deltas never write to http.ResponseWriter concurrently.
func streamAIGeneration(
	ctx context.Context,
	w io.Writer,
	flush func(),
	heartbeatInterval time.Duration,
	generate func(func(string)) (ai.GenerationResult, error),
) (ai.GenerationResult, error) {
	if err := writeSSEComment(w, flush, "connected"); err != nil {
		return ai.GenerationResult{}, err
	}

	deltas := make(chan string)
	result := make(chan aiGenerationResult, 1)
	go func() {
		generation, err := generate(func(text string) {
			select {
			case deltas <- text:
			case <-ctx.Done():
			}
		})
		result <- aiGenerationResult{generation: generation, err: err}
	}()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	ctxDone := ctx.Done()
	writesEnabled := true
	for {
		select {
		case text := <-deltas:
			if writesEnabled {
				if err := writeSSEEvent(w, flush, "delta", text); err != nil {
					return ai.GenerationResult{}, err
				}
			}
		case generated := <-result:
			return generated.generation, generated.err
		case <-heartbeat.C:
			if writesEnabled {
				if err := writeSSEComment(w, flush, "keep-alive"); err != nil {
					return ai.GenerationResult{}, err
				}
			}
		case <-ctxDone:
			// The provider shares this context and will stop. Disable writes but
			// wait for its process to exit before releasing the global AI lock.
			writesEnabled = false
			ctxDone = nil
		}
	}
}

// handleDeckPrimerGenerate streams generation progress as Server-Sent Events:
// `delta` events carry JSON-encoded text fragments, a final `done` event
// carries the saved primer, and `error` reports failures. The frontend sends
// Accept: text/event-stream, which also exempts the response from gzip.
func (s *Server) handleDeckPrimerGenerate(w http.ResponseWriter, r *http.Request, deckID int64) {
	if !s.aiGenBusy.TryLock() {
		writeError(w, http.StatusConflict, "another AI generation is already running")
		return
	}
	defer s.aiGenBusy.Unlock()

	provider, model := s.aiSelection()
	if status := s.aiProvider.ProviderStatus(r.Context(), provider); !status.Available {
		writeError(w, http.StatusServiceUnavailable, status.Detail)
		return
	}

	detail, err := s.store.GetDeckDetail(r.Context(), deckID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(detail.Cards) == 0 {
		writeError(w, http.StatusBadRequest, "deck has no cards to analyze")
		return
	}
	s.enrichDeckCardNames(r.Context(), detail.Cards)
	s.enrichMatchDeckColors(r.Context(), detail.Matches)

	// If the underlying writer can't flush (e.g. some asset-server setups),
	// events still arrive — just buffered until the handler returns.
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

	prompt := ai.BuildPrimerPrompt(detail)
	generation, err := streamAIGeneration(
		ctx,
		w,
		flush,
		aiHeartbeatInterval,
		func(onDelta func(string)) (ai.GenerationResult, error) {
			return s.aiProvider.Generate(ctx, provider, model, prompt, onDelta)
		},
	)
	if generation.Usage.HasTokens() {
		usageCtx, usageCancel := context.WithTimeout(context.Background(), 10*time.Second)
		usageErr := s.store.RecordAIUsage(
			usageCtx,
			deckID,
			provider,
			ai.NormalizeModel(provider, model),
			generation.Usage,
			err == nil,
		)
		usageCancel()
		if usageErr != nil {
			log.Printf("AI usage record failed (deck_id=%d provider=%s model=%s): %v", deckID, provider, model, usageErr)
		}
	}

	if err != nil {
		log.Printf("AI primer generation failed (deck_id=%d provider=%s model=%s): %v", deckID, provider, model, err)
		if ctx.Err() == nil {
			if sendErr := writeSSEEvent(w, flush, "error", map[string]string{"error": err.Error()}); sendErr != nil {
				log.Printf("AI primer error event failed (deck_id=%d): %v", deckID, sendErr)
			}
		}
		return
	}

	// Persist with a fresh context: the client may disconnect right as
	// generation finishes, and the work is too expensive to throw away.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()
	primer, err := s.store.UpsertDeckPrimer(
		saveCtx,
		deckID,
		ai.CardsHash(detail.Cards),
		ai.GenerationModel(provider, model),
		generation.Content,
	)
	if err != nil {
		log.Printf("AI primer save failed (deck_id=%d provider=%s model=%s): %v", deckID, provider, model, err)
		if sendErr := writeSSEEvent(w, flush, "error", map[string]string{"error": err.Error()}); sendErr != nil {
			log.Printf("AI primer save error event failed (deck_id=%d): %v", deckID, sendErr)
		}
		return
	}
	if err := writeSSEEvent(w, flush, "done", primer); err != nil {
		log.Printf("AI primer done event failed (deck_id=%d): %v", deckID, err)
	}
}
