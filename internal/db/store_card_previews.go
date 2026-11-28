package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CardPreview is what card hovers and art tiles show for one Arena card,
// copied from Scryfall so previews keep working across restarts and offline.
type CardPreview struct {
	ArenaID     int64
	Name        string
	ImageURL    string
	ArtCropURL  string
	ScryfallURL string
	ManaCost    string
	ManaValue   *float64
	TypeLine    string
	Rarity      string
	// Colors is a WUBRG-ordered subset string ("UB").
	Colors string
}

const appMetadataCardPreviewsSyncedKey = "card_previews_synced_at"

// NormalizeCardPreviewName is the key card previews are matched on by name:
// lowercase, front face only, so "Delver of Secrets" finds the double-faced
// "Delver of Secrets // Insectile Aberration".
func NormalizeCardPreviewName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if front, _, ok := strings.Cut(name, " // "); ok {
		name = strings.TrimSpace(front)
	}
	return name
}

const cardPreviewColumns = `arena_id, name, image_url, art_crop_url, scryfall_url, mana_cost, mana_value, type_line, rarity, colors`

func scanCardPreviews(rows *sql.Rows) ([]CardPreview, error) {
	defer rows.Close()
	var out []CardPreview
	for rows.Next() {
		var p CardPreview
		if err := rows.Scan(&p.ArenaID, &p.Name, &p.ImageURL, &p.ArtCropURL, &p.ScryfallURL,
			&p.ManaCost, &p.ManaValue, &p.TypeLine, &p.Rarity, &p.Colors); err != nil {
			return nil, fmt.Errorf("scan card preview: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate card previews: %w", err)
	}
	return out, nil
}

// LookupCardPreviews returns stored previews by Arena ID. Cards with no row
// are absent from the result.
func (s *Store) LookupCardPreviews(ctx context.Context, cardIDs []int64) (map[int64]CardPreview, error) {
	out := make(map[int64]CardPreview, len(cardIDs))
	for _, batch := range int64Batches(cardIDs, sqliteInClauseBatchSize) {
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT %s FROM card_previews WHERE arena_id IN (%s)
		`, cardPreviewColumns, placeholderList(len(batch))), args...)
		if err != nil {
			return nil, fmt.Errorf("lookup card previews: %w", err)
		}
		previews, err := scanCardPreviews(rows)
		if err != nil {
			return nil, err
		}
		for _, p := range previews {
			out[p.ArenaID] = p
		}
	}
	return out, nil
}

// LookupCardPreviewsByName returns one stored preview per name, keyed by
// NormalizeCardPreviewName. When several printings share a name, the one with
// the highest Arena ID (usually the newest) wins.
func (s *Store) LookupCardPreviewsByName(ctx context.Context, names []string) (map[string]CardPreview, error) {
	keys := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		key := NormalizeCardPreviewName(name)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	out := make(map[string]CardPreview, len(keys))
	for start := 0; start < len(keys); start += sqliteInClauseBatchSize {
		batch := keys[start:min(start+sqliteInClauseBatchSize, len(keys))]
		args := make([]any, len(batch))
		for i, key := range batch {
			args[i] = key
		}
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT name_normalized, %s FROM card_previews
			WHERE name_normalized IN (%s)
			ORDER BY arena_id ASC
		`, cardPreviewColumns, placeholderList(len(batch))), args...)
		if err != nil {
			return nil, fmt.Errorf("lookup card previews by name: %w", err)
		}
		for rows.Next() {
			var key string
			var p CardPreview
			if err := rows.Scan(&key, &p.ArenaID, &p.Name, &p.ImageURL, &p.ArtCropURL, &p.ScryfallURL,
				&p.ManaCost, &p.ManaValue, &p.TypeLine, &p.Rarity, &p.Colors); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan card preview by name: %w", err)
			}
			out[key] = p
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate card previews by name: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// UpsertCardPreviews stores previews in one transaction. Rows without an Arena
// ID or an image are skipped, since a preview can't show without one.
func (s *Store) UpsertCardPreviews(ctx context.Context, previews []CardPreview) error {
	if len(previews) == 0 {
		return nil
	}
	release, err := s.AcquireWriter(ctx)
	if err != nil {
		return err
	}
	defer release()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO card_previews (
			arena_id, name, name_normalized, image_url, art_crop_url, scryfall_url,
			mana_cost, mana_value, type_line, rarity, colors, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(arena_id) DO UPDATE SET
			name = excluded.name,
			name_normalized = excluded.name_normalized,
			image_url = excluded.image_url,
			art_crop_url = excluded.art_crop_url,
			scryfall_url = excluded.scryfall_url,
			mana_cost = excluded.mana_cost,
			mana_value = excluded.mana_value,
			type_line = excluded.type_line,
			rarity = excluded.rarity,
			colors = excluded.colors,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare card preview upsert: %w", err)
	}
	defer stmt.Close()

	now := nowUTC()
	for _, p := range previews {
		if p.ArenaID <= 0 || strings.TrimSpace(p.ImageURL) == "" || strings.TrimSpace(p.Name) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, p.ArenaID, p.Name, NormalizeCardPreviewName(p.Name),
			p.ImageURL, p.ArtCropURL, p.ScryfallURL, p.ManaCost, p.ManaValue, p.TypeLine,
			p.Rarity, p.Colors, now); err != nil {
			return fmt.Errorf("upsert card preview %d: %w", p.ArenaID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card previews: %w", err)
	}
	return nil
}

// CardPreviewsSyncedAt reports when every Arena card's preview was last
// downloaded. The zero time means never.
func (s *Store) CardPreviewsSyncedAt(ctx context.Context) (time.Time, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE key = ?`,
		appMetadataCardPreviewsSyncedKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read card previews sync time: %w", err)
	}
	syncedAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, nil
	}
	return syncedAt, nil
}

// SetCardPreviewsSyncedAt records a finished download of every Arena card.
func (s *Store) SetCardPreviewsSyncedAt(ctx context.Context, syncedAt time.Time) error {
	release, err := s.AcquireWriter(ctx)
	if err != nil {
		return err
	}
	defer release()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, appMetadataCardPreviewsSyncedKey, utcTimestamp(syncedAt), nowUTC()); err != nil {
		return fmt.Errorf("record card previews sync time: %w", err)
	}
	return nil
}

func placeholderList(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
