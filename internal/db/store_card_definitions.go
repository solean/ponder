package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

const cardDefinitionsVersionKey = "card_definitions_source_version"

// CardDefinitionUpsert carries one extracted Arena printing into the builder
// catalog, including the raw flags that the search queries never expose.
type CardDefinitionUpsert struct {
	model.CardDefinition
	IsPrimary bool
	IsToken   bool
}

// CardSearchParams describes a builder card search. Zero values mean
// "no filter"; Limit is clamped by the store.
type CardSearchParams struct {
	Query        string
	Colors       []string
	TypeText     string
	Rarity       string
	SetCode      string
	ManaValueMin *float64
	ManaValueMax *float64
	Sort         string
	Limit        int
	Offset       int
}

const cardSearchMaxLimit = 200
const cardSearchDefaultLimit = 50

// NormalizeCardName produces the canonical lookup key for a card name:
// lowercase, trimmed, straight apostrophes, collapsed inner whitespace.
func NormalizeCardName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\u2019", "'")
	return strings.Join(strings.Fields(name), " ")
}

// CardDefinitionsVersion returns the Arena data version the catalog was last
// extracted from, or "" when no extraction has succeeded yet.
func (s *Store) CardDefinitionsVersion(ctx context.Context) (string, error) {
	var version string
	err := s.db.QueryRowContext(ctx, `
		SELECT value
		FROM app_metadata
		WHERE key = ?
	`, cardDefinitionsVersionKey).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get card definitions version: %w", err)
	}
	return strings.TrimSpace(version), nil
}

func (s *Store) CountCardDefinitions(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_definitions`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count card definitions: %w", err)
	}
	return count, nil
}

// ReplaceCardDefinitions swaps the whole builder catalog in one transaction
// and records the Arena data version it came from. Callers must pass a
// non-empty extraction, so a failed read of a future Arena schema can never
// wipe the last successful catalog.
func (s *Store) ReplaceCardDefinitions(ctx context.Context, version string, defs []CardDefinitionUpsert) error {
	if len(defs) == 0 {
		return fmt.Errorf("refusing to replace card definitions with an empty set")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin card definitions tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM card_definitions`); err != nil {
		return fmt.Errorf("clear card definitions: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO card_definitions (
			arena_id, name, name_normalized, set_code, collector_number, rarity,
			mana_cost, mana_value, colors, color_identity, type_line,
			is_primary, is_token, is_digital_only, is_rebalanced, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare card definitions insert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if def.ArenaID <= 0 || name == "" {
			continue
		}
		var manaValue any
		if def.ManaValue != nil {
			manaValue = *def.ManaValue
		}
		if _, err := stmt.ExecContext(ctx,
			def.ArenaID,
			name,
			NormalizeCardName(name),
			strings.ToUpper(strings.TrimSpace(def.SetCode)),
			strings.TrimSpace(def.CollectorNumber),
			strings.TrimSpace(def.Rarity),
			strings.TrimSpace(def.ManaCost),
			manaValue,
			strings.Join(def.Colors, ""),
			strings.Join(def.ColorIdentity, ""),
			strings.TrimSpace(def.TypeLine),
			boolToInt(def.IsPrimary),
			boolToInt(def.IsToken),
			boolToInt(def.IsDigitalOnly),
			boolToInt(def.IsRebalanced),
			now,
		); err != nil {
			return fmt.Errorf("insert card definition %d: %w", def.ArenaID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, cardDefinitionsVersionKey, strings.TrimSpace(version), now); err != nil {
		return fmt.Errorf("save card definitions version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card definitions tx: %w", err)
	}
	return nil
}

// escapeLike escapes LIKE wildcards in user input; queries using it must
// append ESCAPE '\'.
func escapeLike(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `%`, `\%`)
	return strings.ReplaceAll(v, `_`, `\_`)
}

var deckBuilderColorOrder = []string{"W", "U", "B", "R", "G"}

// SearchCardDefinitions returns one printing per logical card name matching
// the filters (the highest Arena ID, i.e. the newest printing) plus the total
// number of distinct matching names for pagination.
func (s *Store) SearchCardDefinitions(ctx context.Context, params CardSearchParams) ([]model.CardDefinition, int64, error) {
	where := []string{"1=1"}
	args := []any{}

	if q := NormalizeCardName(params.Query); q != "" {
		where = append(where, `name_normalized LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q)+"%")
	}
	if t := strings.TrimSpace(params.TypeText); t != "" {
		where = append(where, `type_line LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(t)+"%")
	}
	if r := strings.ToLower(strings.TrimSpace(params.Rarity)); r != "" {
		where = append(where, `rarity = ?`)
		args = append(args, r)
	}
	if set := strings.ToUpper(strings.TrimSpace(params.SetCode)); set != "" {
		where = append(where, `set_code = ?`)
		args = append(args, set)
	}
	if params.ManaValueMin != nil {
		where = append(where, `mana_value >= ?`)
		args = append(args, *params.ManaValueMin)
	}
	if params.ManaValueMax != nil {
		where = append(where, `mana_value <= ?`)
		args = append(args, *params.ManaValueMax)
	}
	if len(params.Colors) > 0 {
		// Subset semantics: exclude cards containing any unselected color, so
		// selecting W+U returns mono-white, mono-blue, Azorius, and colorless.
		selected := make(map[string]bool, len(params.Colors))
		for _, color := range params.Colors {
			selected[strings.ToUpper(strings.TrimSpace(color))] = true
		}
		for _, color := range deckBuilderColorOrder {
			if !selected[color] {
				where = append(where, fmt.Sprintf(`colors NOT LIKE '%%%s%%'`, color))
			}
		}
	}

	whereClause := strings.Join(where, " AND ")

	var total int64
	countQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT name_normalized)
		FROM card_definitions
		WHERE %s
	`, whereClause)
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count card search: %w", err)
	}

	orderClause := `cd.name_normalized ASC, cd.arena_id ASC`
	useRelevance := false
	switch strings.TrimSpace(params.Sort) {
	case "":
		// Default relevance ordering: prefix matches ahead of substring
		// matches, then by name.
		useRelevance = NormalizeCardName(params.Query) != ""
	case "name":
	case "manaValue":
		orderClause = `cd.mana_value IS NULL, cd.mana_value ASC, ` + orderClause
	case "set":
		orderClause = `cd.set_code ASC, ` + orderClause
	default:
		return nil, 0, fmt.Errorf("invalid sort %q", params.Sort)
	}
	if useRelevance {
		orderClause = `(cd.name_normalized LIKE ? ESCAPE '\') DESC, ` + orderClause
	}

	limit := params.Limit
	if limit <= 0 {
		limit = cardSearchDefaultLimit
	}
	if limit > cardSearchMaxLimit {
		limit = cardSearchMaxLimit
	}
	offset := max(params.Offset, 0)

	query := fmt.Sprintf(`
		SELECT
			cd.arena_id, cd.name, cd.set_code, cd.collector_number, cd.rarity,
			cd.mana_cost, cd.mana_value, cd.colors, cd.color_identity, cd.type_line,
			cd.is_digital_only, cd.is_rebalanced
		FROM card_definitions cd
		JOIN (
			SELECT MAX(arena_id) AS arena_id
			FROM card_definitions
			WHERE %s
			GROUP BY name_normalized
		) pick ON pick.arena_id = cd.arena_id
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, whereClause, orderClause)

	queryArgs := append([]any{}, args...)
	if useRelevance {
		queryArgs = append(queryArgs, escapeLike(NormalizeCardName(params.Query))+"%")
	}
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("search card definitions: %w", err)
	}
	defer rows.Close()

	out := make([]model.CardDefinition, 0, limit)
	for rows.Next() {
		def, err := scanCardDefinition(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, def)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate card search rows: %w", err)
	}

	return out, total, nil
}

func scanCardDefinition(rows *sql.Rows) (model.CardDefinition, error) {
	var def model.CardDefinition
	var manaValue sql.NullFloat64
	var colors, colorIdentity string
	var isDigitalOnly, isRebalanced int64
	if err := rows.Scan(
		&def.ArenaID, &def.Name, &def.SetCode, &def.CollectorNumber, &def.Rarity,
		&def.ManaCost, &manaValue, &colors, &colorIdentity, &def.TypeLine,
		&isDigitalOnly, &isRebalanced,
	); err != nil {
		return def, fmt.Errorf("scan card definition: %w", err)
	}
	if manaValue.Valid {
		value := manaValue.Float64
		def.ManaValue = &value
	}
	def.Colors = splitColorLetters(colors)
	def.ColorIdentity = splitColorLetters(colorIdentity)
	def.IsDigitalOnly = isDigitalOnly != 0
	def.IsRebalanced = isRebalanced != 0
	return def, nil
}

func splitColorLetters(colors string) []string {
	colors = strings.TrimSpace(colors)
	out := make([]string, 0, len(colors))
	for _, r := range colors {
		out = append(out, string(r))
	}
	return out
}

// ListCardDefinitionsByNames returns every printing whose normalized name is
// in the given set, keyed by normalized name, so import can pick a printing.
func (s *Store) ListCardDefinitionsByNames(ctx context.Context, normalizedNames []string) (map[string][]model.CardDefinition, error) {
	out := make(map[string][]model.CardDefinition, len(normalizedNames))
	if len(normalizedNames) == 0 {
		return out, nil
	}

	unique := make([]string, 0, len(normalizedNames))
	seen := make(map[string]struct{}, len(normalizedNames))
	for _, name := range normalizedNames {
		name = NormalizeCardName(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}

	for start := 0; start < len(unique); start += sqliteInClauseBatchSize {
		end := min(start+sqliteInClauseBatchSize, len(unique))
		batch := unique[start:end]

		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, name := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, name)
		}

		query := fmt.Sprintf(`
			SELECT
				cd.arena_id, cd.name, cd.set_code, cd.collector_number, cd.rarity,
				cd.mana_cost, cd.mana_value, cd.colors, cd.color_identity, cd.type_line,
				cd.is_digital_only, cd.is_rebalanced
			FROM card_definitions cd
			WHERE cd.name_normalized IN (%s)
		`, strings.Join(placeholders, ","))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("lookup card definitions by name: %w", err)
		}
		for rows.Next() {
			def, err := scanCardDefinition(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			key := NormalizeCardName(def.Name)
			out[key] = append(out[key], def)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate card definition name rows: %w", err)
		}
		rows.Close()
	}

	return out, nil
}
