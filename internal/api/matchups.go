package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

const (
	opponentDeckSizeConstructed = 60.0
	opponentDeckSizeLimited     = 40.0
)

// matchupArchetypes is the closed label set. Only the first four are ever
// derived; combo and ramp exist for manual correction, since neither can be
// recognized from observed types and curves alone.
var matchupArchetypes = []string{"aggro", "midrange", "control", "combo", "ramp", "unknown"}

func isAllowedArchetype(label string) bool {
	for _, allowed := range matchupArchetypes {
		if label == allowed {
			return true
		}
	}
	return false
}

func eventLooksLimited(format, eventName string) bool {
	combined := strings.ToLower(format + " " + eventName)
	return strings.Contains(combined, "draft") || strings.Contains(combined, "sealed") ||
		strings.Contains(combined, "limited")
}

var (
	// Arena event names embed dates as bare YYYYMMDD tokens.
	eventDateToken = regexp.MustCompile(`^\d{8}$`)
	// Set codes are short all-caps alphanumerics (TMT, FIN, Y25); mixed-case
	// type words like "QuickDraft" never match. Mirrors web/src/lib/events.ts.
	eventSetCodeToken = regexp.MustCompile(`^[A-Z0-9]{2,5}$`)
)

// limitedSetCode extracts the set code from an Arena event name like
// "QuickDraft_TMT_20260313" or "FIN_Quick_Draft"; empty when there is none.
func limitedSetCode(eventName string) string {
	for _, token := range strings.Split(eventName, "_") {
		if eventDateToken.MatchString(token) {
			continue
		}
		if eventSetCodeToken.MatchString(token) {
			return token
		}
	}
	return ""
}

// opponentCardFacts is everything classification knows about one card.
type opponentCardFacts struct {
	Colors    []string
	ManaValue *float64
	TypeLine  string
}

// classifyOpponent derives a broad, explainable archetype from observed cards.
// Thresholds are deliberately coarse: the label is a grouping key for matchup
// records, not a judgment, and every consumer also sees the confidence.
func classifyOpponent(quantities map[int64]int64, facts map[int64]opponentCardFacts, limited bool) model.OpponentClassification {
	out := model.OpponentClassification{
		Archetype:  "unknown",
		Source:     "derived",
		Confidence: "low",
		Colors:     []string{},
	}

	colorIdentityByCardID := make(map[int64][]string, len(facts))
	for cardID, fact := range facts {
		colorIdentityByCardID[cardID] = fact.Colors
	}
	colors, known := matchColorsForCardQuantities(quantities, colorIdentityByCardID)
	if known {
		out.Colors = colors
		out.ColorsKnown = true
	}

	deckSize := opponentDeckSizeConstructed
	if limited {
		deckSize = opponentDeckSizeLimited
	}

	var totalCopies, distinctTypedNonland int64
	var typedNonlandCopies, creatureCopies, spellCopies int64
	var manaValueWeighted float64
	var manaValueCopies int64
	for cardID, quantity := range quantities {
		if quantity <= 0 {
			continue
		}
		totalCopies += quantity
		out.DistinctCards++
		fact, ok := facts[cardID]
		if !ok {
			continue
		}
		typeLine := strings.ToLower(fact.TypeLine)
		if typeLine == "" || strings.Contains(typeLine, "land") {
			continue
		}
		distinctTypedNonland++
		typedNonlandCopies += quantity
		if strings.Contains(typeLine, "creature") {
			creatureCopies += quantity
		}
		if strings.Contains(typeLine, "instant") || strings.Contains(typeLine, "sorcery") {
			spellCopies += quantity
		}
		if fact.ManaValue != nil {
			manaValueWeighted += *fact.ManaValue * float64(quantity)
			manaValueCopies += quantity
		}
	}
	out.ObservedCards = totalCopies
	out.PctObserved = float64(totalCopies) / deckSize
	if out.PctObserved > 1 {
		out.PctObserved = 1
	}

	if manaValueCopies > 0 {
		avg := manaValueWeighted / float64(manaValueCopies)
		out.AvgManaValue = &avg
	}
	if typedNonlandCopies > 0 {
		share := float64(creatureCopies) / float64(typedNonlandCopies)
		out.CreatureShare = &share
	}

	switch {
	case out.PctObserved >= 0.4 && distinctTypedNonland >= 10:
		out.Confidence = "high"
	case out.PctObserved >= 0.22 || distinctTypedNonland >= 7:
		out.Confidence = "medium"
	}

	if distinctTypedNonland < 4 {
		return out
	}
	creatureShare := float64(creatureCopies) / float64(typedNonlandCopies)
	spellShare := float64(spellCopies) / float64(typedNonlandCopies)
	avgManaValue := 0.0
	if manaValueCopies > 0 {
		avgManaValue = manaValueWeighted / float64(manaValueCopies)
	}
	switch {
	case avgManaValue > 0 && avgManaValue <= 2.8 && creatureShare >= 0.5:
		out.Archetype = "aggro"
	case creatureShare < 0.35 && spellShare >= 0.35:
		out.Archetype = "control"
	default:
		out.Archetype = "midrange"
	}
	return out
}

// resolveCardMetadata returns color identity and mana value for the given
// cards, reading the local cache first, then the MTGA raw card database, then
// Scryfall, caching anything newly resolved.
func (s *Server) resolveCardMetadata(ctx context.Context, cardIDs []int64) map[int64]db.CardMetadata {
	cardIDs = uniqueCardIDs(cardIDs)
	if len(cardIDs) == 0 {
		return map[int64]db.CardMetadata{}
	}

	resolved, err := s.store.LookupCardMetadata(ctx, cardIDs)
	if err != nil {
		log.Printf("card metadata lookup failed: %v", err)
		resolved = map[int64]db.CardMetadata{}
	}

	unresolved := make([]int64, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		if _, ok := resolved[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}
	if len(unresolved) == 0 {
		return resolved
	}

	newlyResolved := make(map[int64]db.CardMetadata)
	localMetadata, localErr := s.fetchCardMetadataFromMTGARaw(ctx, unresolved)
	if localErr != nil {
		log.Printf("local MTGA card metadata lookup failed: %v", localErr)
	}
	for cardID, meta := range localMetadata {
		resolved[cardID] = meta
		newlyResolved[cardID] = meta
	}

	unresolved = unresolved[:0]
	for _, cardID := range cardIDs {
		if _, ok := resolved[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}
	if len(unresolved) > 0 {
		fetched, fetchErr := s.fetchCardMetadataFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			log.Printf("scryfall card metadata lookup failed: %v", fetchErr)
		}
		for cardID, meta := range fetched {
			resolved[cardID] = meta
			newlyResolved[cardID] = meta
		}
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardMetadata(ctx, newlyResolved); err != nil {
			log.Printf("card metadata cache upsert failed: %v", err)
		}
	}
	return resolved
}

func (s *Server) fetchCardMetadataFromMTGARaw(ctx context.Context, cardIDs []int64) (map[int64]db.CardMetadata, error) {
	out := make(map[int64]db.CardMetadata, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	rawDBPath := discoverMTGARawCardDBPath()
	if strings.TrimSpace(rawDBPath) == "" {
		return out, nil
	}

	rawDB, err := sql.Open("sqlite", rawDBPath)
	if err != nil {
		return nil, fmt.Errorf("open MTGA raw card db %q: %w", rawDBPath, err)
	}
	defer rawDB.Close()
	rawDB.SetMaxOpenConns(1)
	rawDB.SetMaxIdleConns(1)

	if err := rawDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping MTGA raw card db %q: %w", rawDBPath, err)
	}

	for start := 0; start < len(cardIDs); start += rawCardLookupBatchMax {
		end := min(start+rawCardLookupBatchMax, len(cardIDs))
		batch := cardIDs[start:end]

		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, cardID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, cardID)
		}

		// Order_CMCWithXLast holds the card's mana value in current raw
		// databases; it is a sort key, so guard against unexpected values.
		rows, err := rawDB.QueryContext(ctx, fmt.Sprintf(`
			SELECT GrpId, COALESCE(ColorIdentity, ''), Order_CMCWithXLast
			FROM Cards
			WHERE GrpId IN (%s)
		`, strings.Join(placeholders, ",")), args...)
		if err != nil {
			return nil, fmt.Errorf("query MTGA raw card metadata: %w", err)
		}
		for rows.Next() {
			var cardID int64
			var rawColorIdentity string
			var rawManaValue sql.NullFloat64
			if err := rows.Scan(&cardID, &rawColorIdentity, &rawManaValue); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan MTGA raw card metadata: %w", err)
			}
			meta := db.CardMetadata{
				ColorIdentity: strings.Join(parseMTGARawColorIdentity(rawColorIdentity), ""),
			}
			if rawManaValue.Valid && rawManaValue.Float64 >= 0 && rawManaValue.Float64 <= 20 {
				value := rawManaValue.Float64
				meta.ManaValue = &value
			}
			out[cardID] = meta
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate MTGA raw card metadata: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

func (s *Server) fetchCardMetadataFromScryfall(ctx context.Context, cardIDs []int64) (map[int64]db.CardMetadata, error) {
	out := make(map[int64]db.CardMetadata, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	var firstErr error
	for start := 0; start < len(cardIDs); start += scryfallSearchBatchMax {
		end := min(start+scryfallSearchBatchMax, len(cardIDs))
		batch, err := s.fetchCardMetadataBatch(ctx, cardIDs[start:end])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for cardID, meta := range batch {
			out[cardID] = meta
		}
	}
	return out, firstErr
}

func (s *Server) fetchCardMetadataBatch(ctx context.Context, cardIDs []int64) (map[int64]db.CardMetadata, error) {
	type responseCard struct {
		ArenaID       int64    `json:"arena_id"`
		ColorIdentity []string `json:"color_identity"`
		ManaValue     *float64 `json:"cmc"`
	}
	type responsePayload struct {
		Data     []responseCard `json:"data"`
		HasMore  bool           `json:"has_more"`
		NextPage string         `json:"next_page"`
	}

	out := make(map[int64]db.CardMetadata, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	terms := make([]string, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		terms = append(terms, fmt.Sprintf("arenaid:%d", cardID))
	}
	searchURL := fmt.Sprintf("%s?q=%s&unique=cards", scryfallSearchURL, url.QueryEscape(strings.Join(terms, " or ")))

	addCards := func(cards []responseCard) {
		for _, card := range cards {
			if card.ArenaID <= 0 {
				continue
			}
			out[card.ArenaID] = db.CardMetadata{
				ColorIdentity: strings.Join(normalizeDeckColors(card.ColorIdentity), ""),
				ManaValue:     card.ManaValue,
			}
		}
	}

	nextURL := searchURL
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return out, fmt.Errorf("build scryfall metadata request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "ponder/0.1 (local tracker)")

		res, err := s.httpClient.Do(req)
		if err != nil {
			return out, fmt.Errorf("request scryfall metadata: %w", err)
		}
		if res.StatusCode == http.StatusNotFound {
			res.Body.Close()
			return out, nil
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
			res.Body.Close()
			return out, fmt.Errorf("scryfall metadata status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
		}
		var decoded responsePayload
		err = json.NewDecoder(res.Body).Decode(&decoded)
		res.Body.Close()
		if err != nil {
			return out, fmt.Errorf("decode scryfall metadata response: %w", err)
		}
		addCards(decoded.Data)
		if !decoded.HasMore {
			break
		}
		nextURL = strings.TrimSpace(decoded.NextPage)
	}
	return out, nil
}

func parseCachedColorIdentity(identity string) []string {
	colors := make([]string, 0, len(identity))
	for _, r := range identity {
		colors = append(colors, string(r))
	}
	return normalizeDeckColors(colors)
}

// matchupInputs is everything the matchup builders need, fetched in one pass.
type matchupInputs struct {
	matchRows       []db.MatchupMatchRow
	observedByMatch map[int64]map[int64]int64
	facts           map[int64]opponentCardFacts
	names           map[int64]string
	gameSummaries   map[int64]model.MatchGameSummary
	overrides       map[int64]string
}

// loadMatchupInputs fetches deck-linked match rows (all decks when deckID <= 0)
// plus the observed cards, card facts, game summaries, and manual overrides
// that classification and aggregation need.
func (s *Server) loadMatchupInputs(ctx context.Context, deckID int64) (*matchupInputs, error) {
	matchRows, err := s.store.ListMatchupMatchRows(ctx, deckID)
	if err != nil {
		return nil, err
	}
	matchIDs := make([]int64, 0, len(matchRows))
	for _, row := range matchRows {
		matchIDs = append(matchIDs, row.MatchID)
	}
	observedByMatch, err := s.store.ListMatchOpponentCardQuantities(ctx, matchIDs)
	if err != nil {
		return nil, err
	}
	gameSummaries, err := s.store.ListMatchGameSummaries(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := s.store.ListMatchOpponentArchetypeOverrides(ctx)
	if err != nil {
		return nil, err
	}

	allCardIDs := make([]int64, 0)
	for _, quantities := range observedByMatch {
		for cardID := range quantities {
			allCardIDs = append(allCardIDs, cardID)
		}
	}
	metadata := s.resolveCardMetadata(ctx, allCardIDs)
	typeLines := s.resolveCardTypeLines(ctx, allCardIDs)
	names := s.resolveCardNames(ctx, allCardIDs)

	facts := make(map[int64]opponentCardFacts, len(metadata))
	for _, cardID := range uniqueCardIDs(allCardIDs) {
		fact := opponentCardFacts{TypeLine: typeLines[cardID]}
		if meta, ok := metadata[cardID]; ok {
			fact.Colors = parseCachedColorIdentity(meta.ColorIdentity)
			fact.ManaValue = meta.ManaValue
		}
		if fact.TypeLine == "" && isBasicLandName(names[cardID]) {
			fact.TypeLine = "Basic Land"
		}
		facts[cardID] = fact
	}

	return &matchupInputs{
		matchRows:       matchRows,
		observedByMatch: observedByMatch,
		facts:           facts,
		names:           names,
		gameSummaries:   gameSummaries,
		overrides:       overrides,
	}, nil
}

// handleDeckMatchups serves one deck's opponent-archetype matchups for the
// deck detail page.
func (s *Server) handleDeckMatchups(w http.ResponseWriter, r *http.Request, deckID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	inputs, err := s.loadMatchupInputs(r.Context(), deckID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	full := buildMatchupsResponse(inputs.matchRows, inputs.observedByMatch, inputs.facts,
		inputs.names, inputs.gameSummaries, inputs.overrides)
	out := model.DeckMatchupsResponse{Archetypes: matchupArchetypes}
	for index := range full.Decks {
		if full.Decks[index].DeckID == deckID {
			out.Deck = &full.Decks[index]
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ownDeckColors is the player's own deck color identity in one match.
type ownDeckColors struct {
	Colors []string
	Known  bool
}

// resolveOwnDeckColors derives the player's deck colors per match from the
// decklist as played, using the shared cached color-identity resolver.
func (s *Server) resolveOwnDeckColors(ctx context.Context, matchRows []db.MatchupMatchRow) map[int64]ownDeckColors {
	matchIDs := make([]int64, 0, len(matchRows))
	for _, row := range matchRows {
		matchIDs = append(matchIDs, row.MatchID)
	}
	deckCardQuantitiesByMatch, err := s.store.ListMatchDeckCardQuantities(ctx, matchIDs)
	if err != nil {
		log.Printf("limited matchup deck color lookup failed: %v", err)
		return map[int64]ownDeckColors{}
	}

	allCardIDs := make([]int64, 0)
	for _, quantities := range deckCardQuantitiesByMatch {
		for cardID := range quantities {
			allCardIDs = append(allCardIDs, cardID)
		}
	}
	colorIdentityByCardID := s.resolveCardColorIdentities(ctx, allCardIDs)

	out := make(map[int64]ownDeckColors, len(deckCardQuantitiesByMatch))
	for matchID, quantities := range deckCardQuantitiesByMatch {
		colors, known := matchColorsForCardQuantities(quantities, colorIdentityByCardID)
		out[matchID] = ownDeckColors{Colors: colors, Known: known}
	}
	return out
}

// handleLimitedMatchups serves opponent color-pair records pooled per set
// across every limited (draft/sealed) match.
func (s *Server) handleLimitedMatchups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	inputs, err := s.loadMatchupInputs(r.Context(), 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ownColorsByMatch := s.resolveOwnDeckColors(r.Context(), inputs.matchRows)
	writeJSON(w, http.StatusOK, buildLimitedMatchupsResponse(inputs.matchRows,
		inputs.observedByMatch, inputs.facts, inputs.names, inputs.gameSummaries,
		inputs.overrides, ownColorsByMatch))
}

type matchupCellKey struct {
	colorsKey string
	archetype string
}

func addMatchResult(record *model.RecordAgg, result string) {
	switch result {
	case "win":
		record.Wins++
		record.Games++
	case "loss":
		record.Losses++
		record.Games++
	case "draw":
		record.Draws++
		record.Games++
	}
}

func addRecord(target *model.RecordAgg, add model.RecordAgg) {
	target.Games += add.Games
	target.Wins += add.Wins
	target.Losses += add.Losses
	target.Draws += add.Draws
}

// matchupCellState accumulates one (colors × archetype) or (colors-only)
// group of matches into a MatchupRow.
type matchupCellState struct {
	row         model.MatchupRow
	pctSum      float64
	cardMatches map[int64]*model.MatchupObservedCard
}

func newMatchupCellState(colorsKey string, colors []string, archetype string) *matchupCellState {
	return &matchupCellState{
		row: model.MatchupRow{
			ColorsKey:        colorsKey,
			Colors:           colors,
			Archetype:        archetype,
			TopObservedCards: []model.MatchupObservedCard{},
			LossSkewedCards:  []model.MatchupObservedCard{},
			MatchRefs:        []model.MatchupMatchRef{},
		},
		cardMatches: make(map[int64]*model.MatchupObservedCard),
	}
}

func (cell *matchupCellState) addMatch(
	matchRow db.MatchupMatchRow,
	classification model.OpponentClassification,
	gameSummaries map[int64]model.MatchGameSummary,
	observed map[int64]int64,
	facts map[int64]opponentCardFacts,
	names map[int64]string,
) {
	addMatchResult(&cell.row.Matches, matchRow.Result)
	if summary, ok := gameSummaries[matchRow.MatchID]; ok {
		addRecord(&cell.row.Games, summary.Games)
		cell.row.UnknownResultGames += summary.UnknownGames
		addRecord(&cell.row.GameOne, summary.GameOne)
		addRecord(&cell.row.PostBoard, summary.PostBoard)
		addRecord(&cell.row.OnPlay, summary.OnPlay)
		addRecord(&cell.row.OnDraw, summary.OnDraw)
	}
	cell.pctSum += classification.PctObserved
	cell.row.MatchRefs = append(cell.row.MatchRefs, model.MatchupMatchRef{
		MatchID:         matchRow.MatchID,
		Opponent:        matchRow.Opponent,
		EventName:       matchRow.EventName,
		Result:          matchRow.Result,
		StartedAt:       matchRow.StartedAt,
		Colors:          classification.Colors,
		Archetype:       classification.Archetype,
		ArchetypeSource: classification.Source,
		Confidence:      classification.Confidence,
		PctObserved:     classification.PctObserved,
	})

	for cardID, quantity := range observed {
		if quantity <= 0 {
			continue
		}
		if fact, ok := facts[cardID]; ok && isLandTypeLine(fact.TypeLine) {
			continue
		}
		agg, ok := cell.cardMatches[cardID]
		if !ok {
			agg = &model.MatchupObservedCard{CardID: cardID, CardName: names[cardID]}
			cell.cardMatches[cardID] = agg
		}
		agg.Matches++
		agg.Copies += quantity
		switch matchRow.Result {
		case "win":
			agg.WinMatches++
		case "loss":
			agg.LossMatches++
		}
	}
}

// finalize computes the derived fields (confidence, top observed cards,
// loss-skewed cards) and returns the completed row.
func (cell *matchupCellState) finalize() model.MatchupRow {
	matchTotal := len(cell.row.MatchRefs)
	if matchTotal > 0 {
		cell.row.AvgPctObserved = cell.pctSum / float64(matchTotal)
	}
	switch {
	case cell.row.AvgPctObserved >= 0.4 && matchTotal >= 2:
		cell.row.Confidence = "high"
	case cell.row.AvgPctObserved >= 0.22:
		cell.row.Confidence = "medium"
	default:
		cell.row.Confidence = "low"
	}

	cards := make([]model.MatchupObservedCard, 0, len(cell.cardMatches))
	for _, card := range cell.cardMatches {
		cards = append(cards, *card)
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Matches != cards[j].Matches {
			return cards[i].Matches > cards[j].Matches
		}
		if cards[i].Copies != cards[j].Copies {
			return cards[i].Copies > cards[j].Copies
		}
		return cards[i].CardID < cards[j].CardID
	})
	if len(cards) > 8 {
		cell.row.TopObservedCards = cards[:8]
	} else {
		cell.row.TopObservedCards = cards
	}

	decided := cell.row.Matches.Wins + cell.row.Matches.Losses
	lossRate := 0.0
	if decided > 0 {
		lossRate = float64(cell.row.Matches.Losses) / float64(decided)
	}
	skewed := make([]model.MatchupObservedCard, 0)
	for _, card := range cards {
		cardDecided := card.WinMatches + card.LossMatches
		if card.LossMatches < 2 || cardDecided == 0 {
			continue
		}
		if float64(card.LossMatches)/float64(cardDecided) >= lossRate+0.2 {
			skewed = append(skewed, card)
		}
	}
	sort.Slice(skewed, func(i, j int) bool {
		if skewed[i].LossMatches != skewed[j].LossMatches {
			return skewed[i].LossMatches > skewed[j].LossMatches
		}
		return skewed[i].CardID < skewed[j].CardID
	})
	if len(skewed) > 6 {
		skewed = skewed[:6]
	}
	cell.row.LossSkewedCards = skewed

	return cell.row
}

// classifyMatchupRow classifies one match's opponent and applies any manual
// override.
func classifyMatchupRow(
	matchRow db.MatchupMatchRow,
	observedByMatch map[int64]map[int64]int64,
	facts map[int64]opponentCardFacts,
	overrides map[int64]string,
) model.OpponentClassification {
	classification := classifyOpponent(
		observedByMatch[matchRow.MatchID],
		facts,
		eventLooksLimited(matchRow.Format, matchRow.EventName),
	)
	if override, ok := overrides[matchRow.MatchID]; ok && isAllowedArchetype(override) {
		classification.Archetype = override
		classification.Source = "manual"
	}
	return classification
}

func sortMatchupRows(rows []model.MatchupRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Games.Games != rows[j].Games.Games {
			return rows[i].Games.Games > rows[j].Games.Games
		}
		return len(rows[i].MatchRefs) > len(rows[j].MatchRefs)
	})
}

func buildMatchupsResponse(
	matchRows []db.MatchupMatchRow,
	observedByMatch map[int64]map[int64]int64,
	facts map[int64]opponentCardFacts,
	names map[int64]string,
	gameSummaries map[int64]model.MatchGameSummary,
	overrides map[int64]string,
) model.MatchupsResponse {
	type deckState struct {
		deck  model.MatchupDeck
		cells map[matchupCellKey]*matchupCellState
		order []matchupCellKey
	}

	decksByID := make(map[int64]*deckState)
	deckOrder := make([]int64, 0)

	for _, matchRow := range matchRows {
		classification := classifyMatchupRow(matchRow, observedByMatch, facts, overrides)

		state, ok := decksByID[matchRow.DeckID]
		if !ok {
			state = &deckState{
				deck: model.MatchupDeck{
					DeckID:   matchRow.DeckID,
					DeckName: matchRow.DeckName,
					Format:   matchRow.Format,
				},
				cells: make(map[matchupCellKey]*matchupCellState),
			}
			decksByID[matchRow.DeckID] = state
			deckOrder = append(deckOrder, matchRow.DeckID)
		}
		addMatchResult(&state.deck.Matches, matchRow.Result)

		key := matchupCellKey{
			colorsKey: strings.Join(classification.Colors, ""),
			archetype: classification.Archetype,
		}
		cell, ok := state.cells[key]
		if !ok {
			cell = newMatchupCellState(key.colorsKey, classification.Colors, key.archetype)
			state.cells[key] = cell
			state.order = append(state.order, key)
		}
		cell.addMatch(matchRow, classification, gameSummaries, observedByMatch[matchRow.MatchID], facts, names)
	}

	out := model.MatchupsResponse{
		Decks:      make([]model.MatchupDeck, 0, len(deckOrder)),
		Archetypes: matchupArchetypes,
	}
	for _, deckID := range deckOrder {
		state := decksByID[deckID]
		for _, key := range state.order {
			state.deck.Rows = append(state.deck.Rows, state.cells[key].finalize())
		}
		sortMatchupRows(state.deck.Rows)
		out.Decks = append(out.Decks, state.deck)
	}
	sort.SliceStable(out.Decks, func(i, j int) bool {
		return out.Decks[i].Matches.Games > out.Decks[j].Matches.Games
	})
	return out
}

// buildLimitedMatchupsResponse pools every limited (draft/sealed) match by
// set and groups records by opponent color pair. Speed labels stay visible on
// the individual match refs but are not a grouping axis: in limited, the
// color pair effectively is the archetype, and pooling across a set's drafts
// is what makes the sample large enough to mean anything.
func buildLimitedMatchupsResponse(
	matchRows []db.MatchupMatchRow,
	observedByMatch map[int64]map[int64]int64,
	facts map[int64]opponentCardFacts,
	names map[int64]string,
	gameSummaries map[int64]model.MatchGameSummary,
	overrides map[int64]string,
	ownColorsByMatch map[int64]ownDeckColors,
) model.LimitedMatchupsResponse {
	type colorGroupState struct {
		group model.LimitedMatchupColorGroup
		decks map[int64]bool
		cells map[string]*matchupCellState
		order []string
	}
	type setState struct {
		set    model.LimitedMatchupSet
		decks  map[int64]bool
		cells  map[string]*matchupCellState
		order  []string
		groups map[string]*colorGroupState
	}

	setsByCode := make(map[string]*setState)
	setOrder := make([]string, 0)

	for _, matchRow := range matchRows {
		if !eventLooksLimited(matchRow.Format, matchRow.EventName) {
			continue
		}
		setCode := limitedSetCode(matchRow.EventName)
		classification := classifyMatchupRow(matchRow, observedByMatch, facts, overrides)

		state, ok := setsByCode[setCode]
		if !ok {
			state = &setState{
				set:    model.LimitedMatchupSet{SetCode: setCode},
				decks:  make(map[int64]bool),
				cells:  make(map[string]*matchupCellState),
				groups: make(map[string]*colorGroupState),
			}
			setsByCode[setCode] = state
			setOrder = append(setOrder, setCode)
		}
		addMatchResult(&state.set.Matches, matchRow.Result)
		if !state.decks[matchRow.DeckID] {
			state.decks[matchRow.DeckID] = true
			state.set.DeckCount++
		}

		colorsKey := strings.Join(classification.Colors, "")
		cell, ok := state.cells[colorsKey]
		if !ok {
			// The archetype axis is intentionally blank: rows group by color
			// pair only.
			cell = newMatchupCellState(colorsKey, classification.Colors, "")
			state.cells[colorsKey] = cell
			state.order = append(state.order, colorsKey)
		}
		cell.addMatch(matchRow, classification, gameSummaries, observedByMatch[matchRow.MatchID], facts, names)

		// The same match also lands in a group keyed by the player's own deck
		// colors, so records can be filtered to "when I was BG".
		ownColors := ownColorsByMatch[matchRow.MatchID]
		ownKey := ""
		if ownColors.Known {
			ownKey = strings.Join(ownColors.Colors, "")
		}
		group, ok := state.groups[ownKey]
		if !ok {
			group = &colorGroupState{
				group: model.LimitedMatchupColorGroup{
					ColorsKey:   ownKey,
					Colors:      append([]string{}, ownColors.Colors...),
					ColorsKnown: ownColors.Known,
				},
				decks: make(map[int64]bool),
				cells: make(map[string]*matchupCellState),
			}
			state.groups[ownKey] = group
		}
		addMatchResult(&group.group.Matches, matchRow.Result)
		if !group.decks[matchRow.DeckID] {
			group.decks[matchRow.DeckID] = true
			group.group.DeckCount++
		}
		groupCell, ok := group.cells[colorsKey]
		if !ok {
			groupCell = newMatchupCellState(colorsKey, classification.Colors, "")
			group.cells[colorsKey] = groupCell
			group.order = append(group.order, colorsKey)
		}
		groupCell.addMatch(matchRow, classification, gameSummaries, observedByMatch[matchRow.MatchID], facts, names)
	}

	// Match rows arrive newest first, so first-seen set order is most
	// recently played first.
	out := model.LimitedMatchupsResponse{Sets: make([]model.LimitedMatchupSet, 0, len(setOrder))}
	for _, setCode := range setOrder {
		state := setsByCode[setCode]
		for _, colorsKey := range state.order {
			state.set.Rows = append(state.set.Rows, state.cells[colorsKey].finalize())
		}
		sortMatchupRows(state.set.Rows)

		state.set.ColorGroups = make([]model.LimitedMatchupColorGroup, 0, len(state.groups))
		for _, group := range state.groups {
			for _, colorsKey := range group.order {
				group.group.Rows = append(group.group.Rows, group.cells[colorsKey].finalize())
			}
			sortMatchupRows(group.group.Rows)
			state.set.ColorGroups = append(state.set.ColorGroups, group.group)
		}
		sort.SliceStable(state.set.ColorGroups, func(i, j int) bool {
			left, right := state.set.ColorGroups[i], state.set.ColorGroups[j]
			// Unknown-color groups sort last regardless of size.
			if left.ColorsKnown != right.ColorsKnown {
				return left.ColorsKnown
			}
			if left.Matches.Games != right.Matches.Games {
				return left.Matches.Games > right.Matches.Games
			}
			return left.ColorsKey < right.ColorsKey
		})

		out.Sets = append(out.Sets, state.set)
	}
	return out
}

func (s *Server) handleMatchOpponentArchetype(w http.ResponseWriter, r *http.Request, matchID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload := struct {
		Archetype string `json:"archetype"`
	}{}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	archetype := strings.ToLower(strings.TrimSpace(payload.Archetype))
	if archetype != "" && !isAllowedArchetype(archetype) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown archetype %q (use one of %s, or empty to clear)",
				archetype, strings.Join(matchupArchetypes, ", ")))
		return
	}
	if err := s.store.SetMatchOpponentArchetypeOverride(r.Context(), matchID, archetype); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "archetype": archetype})
}
