package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

// ErrDeckProjectNotFound reports a missing deck project id so the API layer
// can answer 404 instead of 500.
var ErrDeckProjectNotFound = errors.New("deck project not found")

// DeckProjectCardInput is the caller-provided state of one project card row.
type DeckProjectCardInput struct {
	Section  string
	ArenaID  int64
	Quantity int64
}

func (s *Store) ListDeckProjects(ctx context.Context) ([]model.DeckProjectSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id,
			p.name,
			p.format,
			p.created_at,
			p.updated_at,
			COALESCE(SUM(CASE WHEN pc.section = 'main' THEN pc.quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN pc.section = 'sideboard' THEN pc.quantity ELSE 0 END), 0)
		FROM deck_projects p
		LEFT JOIN deck_project_cards pc ON pc.project_id = p.id
		GROUP BY p.id
		ORDER BY p.updated_at DESC, p.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list deck projects: %w", err)
	}
	defer rows.Close()

	out := make([]model.DeckProjectSummary, 0)
	for rows.Next() {
		var row model.DeckProjectSummary
		if err := rows.Scan(&row.ID, &row.Name, &row.Format, &row.CreatedAt, &row.UpdatedAt, &row.MainCount, &row.SideboardCount); err != nil {
			return nil, fmt.Errorf("scan deck project summary: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deck project summaries: %w", err)
	}
	return out, nil
}

func (s *Store) CreateDeckProject(ctx context.Context, name, format string, cards []DeckProjectCardInput) (model.DeckProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Untitled deck"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeckProject{}, fmt.Errorf("begin create deck project: %w", err)
	}
	defer tx.Rollback()

	now := nowUTC()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO deck_projects (name, format, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, name, strings.TrimSpace(format), now, now)
	if err != nil {
		return model.DeckProject{}, fmt.Errorf("insert deck project: %w", err)
	}
	projectID, err := res.LastInsertId()
	if err != nil {
		return model.DeckProject{}, fmt.Errorf("deck project id: %w", err)
	}

	if err := insertDeckProjectCards(ctx, tx, projectID, cards); err != nil {
		return model.DeckProject{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.DeckProject{}, fmt.Errorf("commit create deck project: %w", err)
	}

	return s.GetDeckProject(ctx, projectID)
}

// SaveDeckProject replaces the project's metadata and full card list in one
// transaction and returns the canonical saved state.
func (s *Store) SaveDeckProject(ctx context.Context, projectID int64, name, format string, cards []DeckProjectCardInput) (model.DeckProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Untitled deck"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.DeckProject{}, fmt.Errorf("begin save deck project: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE deck_projects
		SET name = ?, format = ?, updated_at = ?
		WHERE id = ?
	`, name, strings.TrimSpace(format), nowUTC(), projectID)
	if err != nil {
		return model.DeckProject{}, fmt.Errorf("update deck project: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.DeckProject{}, fmt.Errorf("deck project rows affected: %w", err)
	}
	if affected == 0 {
		return model.DeckProject{}, ErrDeckProjectNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM deck_project_cards WHERE project_id = ?`, projectID); err != nil {
		return model.DeckProject{}, fmt.Errorf("clear deck project cards: %w", err)
	}
	if err := insertDeckProjectCards(ctx, tx, projectID, cards); err != nil {
		return model.DeckProject{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.DeckProject{}, fmt.Errorf("commit save deck project: %w", err)
	}

	return s.GetDeckProject(ctx, projectID)
}

func insertDeckProjectCards(ctx context.Context, tx *sql.Tx, projectID int64, cards []DeckProjectCardInput) error {
	if len(cards) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO deck_project_cards (project_id, section, arena_id, quantity)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id, section, arena_id) DO UPDATE SET
			quantity = deck_project_cards.quantity + excluded.quantity
	`)
	if err != nil {
		return fmt.Errorf("prepare deck project card insert: %w", err)
	}
	defer stmt.Close()

	for _, card := range cards {
		section := strings.ToLower(strings.TrimSpace(card.Section))
		if section != "main" && section != "sideboard" {
			return fmt.Errorf("invalid deck project section %q", card.Section)
		}
		if card.ArenaID <= 0 {
			return fmt.Errorf("invalid arena id %d", card.ArenaID)
		}
		if card.Quantity <= 0 {
			return fmt.Errorf("invalid quantity %d for card %d", card.Quantity, card.ArenaID)
		}
		if _, err := stmt.ExecContext(ctx, projectID, section, card.ArenaID, card.Quantity); err != nil {
			return fmt.Errorf("insert deck project card %d: %w", card.ArenaID, err)
		}
	}
	return nil
}

func (s *Store) GetDeckProject(ctx context.Context, projectID int64) (model.DeckProject, error) {
	var project model.DeckProject
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, format, created_at, updated_at
		FROM deck_projects
		WHERE id = ?
	`, projectID).Scan(&project.ID, &project.Name, &project.Format, &project.CreatedAt, &project.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return project, ErrDeckProjectNotFound
	}
	if err != nil {
		return project, fmt.Errorf("get deck project: %w", err)
	}

	// card_catalog is the lazy name cache; it keeps rows for printings that a
	// newer Arena catalog no longer contains, so a stale project row still
	// renders with a name alongside its "missing" repair warning.
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			pc.section,
			pc.arena_id,
			pc.quantity,
			cd.name,
			cd.set_code,
			cd.collector_number,
			cd.rarity,
			cd.mana_cost,
			cd.mana_value,
			cd.colors,
			cd.type_line,
			cc.name
		FROM deck_project_cards pc
		LEFT JOIN card_definitions cd ON cd.arena_id = pc.arena_id
		LEFT JOIN card_catalog cc ON cc.arena_id = pc.arena_id
		WHERE pc.project_id = ?
		ORDER BY pc.section ASC, cd.mana_value ASC, cd.name_normalized ASC, pc.arena_id ASC
	`, projectID)
	if err != nil {
		return project, fmt.Errorf("list deck project cards: %w", err)
	}
	defer rows.Close()

	project.Cards = make([]model.DeckProjectCard, 0)
	for rows.Next() {
		var card model.DeckProjectCard
		var defName, setCode, collectorNumber, rarity, manaCost, colors, typeLine sql.NullString
		var manaValue sql.NullFloat64
		var cachedName sql.NullString
		if err := rows.Scan(
			&card.Section, &card.ArenaID, &card.Quantity,
			&defName, &setCode, &collectorNumber, &rarity, &manaCost, &manaValue, &colors, &typeLine,
			&cachedName,
		); err != nil {
			return project, fmt.Errorf("scan deck project card: %w", err)
		}
		if defName.Valid {
			card.Name = defName.String
			card.SetCode = setCode.String
			card.CollectorNumber = collectorNumber.String
			card.Rarity = rarity.String
			card.ManaCost = manaCost.String
			card.TypeLine = typeLine.String
			card.Colors = splitColorLetters(colors.String)
			if manaValue.Valid {
				value := manaValue.Float64
				card.ManaValue = &value
			}
		} else {
			card.Missing = true
			card.Name = strings.TrimSpace(cachedName.String)
			card.Colors = make([]string, 0)
		}
		project.Cards = append(project.Cards, card)
	}
	if err := rows.Err(); err != nil {
		return project, fmt.Errorf("iterate deck project cards: %w", err)
	}

	return project, nil
}

func (s *Store) DeleteDeckProject(ctx context.Context, projectID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM deck_projects WHERE id = ?`, projectID)
	if err != nil {
		return fmt.Errorf("delete deck project: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete deck project rows affected: %w", err)
	}
	if affected == 0 {
		return ErrDeckProjectNotFound
	}
	return nil
}
