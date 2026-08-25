package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func (s *Server) handleCardSearch(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/cards" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.ensureCardDefinitions(r.Context())

	query := r.URL.Query()
	params := db.CardSearchParams{
		Query:    query.Get("q"),
		TypeText: query.Get("type"),
		Rarity:   query.Get("rarity"),
		SetCode:  query.Get("set"),
		Sort:     query.Get("sort"),
	}
	for _, letter := range strings.ToUpper(strings.TrimSpace(query.Get("colors"))) {
		params.Colors = append(params.Colors, string(letter))
	}
	if raw := strings.TrimSpace(query.Get("mvMin")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid mvMin")
			return
		}
		params.ManaValueMin = &value
	}
	if raw := strings.TrimSpace(query.Get("mvMax")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid mvMax")
			return
		}
		params.ManaValueMax = &value
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		params.Limit = value
	}
	if raw := strings.TrimSpace(query.Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		params.Offset = value
	}

	cards, total, err := s.store.SearchCardDefinitions(r.Context(), params)
	if err != nil {
		if strings.Contains(err.Error(), "invalid sort") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.CardSearchResult{Cards: cards, Total: total})
}

type deckProjectSaveRequest struct {
	Name   string                 `json:"name"`
	Format string                 `json:"format"`
	Cards  []deckProjectCardInput `json:"cards"`
}

type deckProjectCardInput struct {
	Section  string `json:"section"`
	ArenaID  int64  `json:"arenaId"`
	Quantity int64  `json:"quantity"`
}

func toStoreCardInputs(cards []deckProjectCardInput) []db.DeckProjectCardInput {
	out := make([]db.DeckProjectCardInput, 0, len(cards))
	for _, card := range cards {
		out = append(out, db.DeckProjectCardInput{
			Section:  card.Section,
			ArenaID:  card.ArenaID,
			Quantity: card.Quantity,
		})
	}
	return out
}

func (s *Server) handleDeckProjects(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/deck-projects" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := s.store.ListDeckProjects(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rows)
	case http.MethodPost:
		var input deckProjectSaveRequest
		if err := decodeJSONBody(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		project, err := s.store.CreateDeckProject(r.Context(), input.Name, input.Format, toStoreCardInputs(input.Cards))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, project)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDeckProjectDetail(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/deck-projects/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing deck project id")
		return
	}

	if parts[0] == "import" && len(parts) == 1 {
		s.handleDeckProjectImport(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid deck project id")
		return
	}

	if len(parts) == 2 && parts[1] == "export" {
		s.handleDeckProjectExport(w, r, id)
		return
	}
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		project, err := s.store.GetDeckProject(r.Context(), id)
		if err != nil {
			writeDeckProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, project)
	case http.MethodPut:
		var input deckProjectSaveRequest
		if err := decodeJSONBody(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		project, err := s.store.SaveDeckProject(r.Context(), id, input.Name, input.Format, toStoreCardInputs(input.Cards))
		if err != nil {
			if errors.Is(err, db.ErrDeckProjectNotFound) {
				writeDeckProjectError(w, err)
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, project)
	case http.MethodDelete:
		if err := s.store.DeleteDeckProject(r.Context(), id); err != nil {
			writeDeckProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeDeckProjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrDeckProjectNotFound) {
		writeError(w, http.StatusNotFound, "deck project not found")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

type deckProjectImportRequest struct {
	Text   string `json:"text"`
	Name   string `json:"name"`
	Format string `json:"format"`
}

func (s *Server) handleDeckProjectImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var input deckProjectImportRequest
	if err := decodeJSONBody(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Text) == "" {
		writeError(w, http.StatusBadRequest, "deck list text is required")
		return
	}

	s.ensureCardDefinitions(r.Context())

	parsedName, entries, malformed := parseArenaDeckList(input.Text)
	if len(entries) == 0 {
		writeError(w, http.StatusBadRequest, "no card lines found in deck list")
		return
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	printingsByName, err := s.store.ListCardDefinitionsByNames(r.Context(), names)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	unresolved := append([]string{}, malformed...)
	cards := make([]db.DeckProjectCardInput, 0, len(entries))
	for _, entry := range entries {
		printings := printingsByName[db.NormalizeCardName(entry.Name)]
		printing, ok := pickPrintingForImport(printings, entry.SetCode, entry.CollectorNumber)
		if !ok {
			unresolved = append(unresolved, strconv.FormatInt(entry.Quantity, 10)+" "+entry.Name)
			continue
		}
		cards = append(cards, db.DeckProjectCardInput{
			Section:  entry.Section,
			ArenaID:  printing.ArenaID,
			Quantity: entry.Quantity,
		})
	}
	if len(cards) == 0 {
		writeError(w, http.StatusBadRequest, "no cards in the deck list could be resolved against the Arena catalog")
		return
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = parsedName
	}
	if name == "" {
		name = "Imported deck"
	}

	project, err := s.store.CreateDeckProject(r.Context(), name, input.Format, cards)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if unresolved == nil {
		unresolved = make([]string, 0)
	}
	writeJSON(w, http.StatusCreated, model.DeckProjectImportResult{Project: project, Unresolved: unresolved})
}

func (s *Server) handleDeckProjectExport(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	project, err := s.store.GetDeckProject(r.Context(), id)
	if err != nil {
		writeDeckProjectError(w, err)
		return
	}

	text, unresolved := formatArenaDeckList(project.Cards)
	if unresolved == nil {
		unresolved = make([]int64, 0)
	}
	writeJSON(w, http.StatusOK, model.DeckProjectExport{Text: text, Unresolved: unresolved})
}
