package api

import (
	"cmp"
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/solean/ponder/internal/model"
)

// draftDisplayPack mirrors the Arena draft layout observed in the client:
// rarity, color, then title. Keep the original pack untouched for
// consumers that need log order. Missing local metadata disables positioning.
func draftDisplayPack(ctx context.Context, pack []model.DraftPickCard) []model.DraftPickCard {
	if len(pack) == 0 || len(pack) > rawCardLookupBatchMax {
		return nil
	}
	path := discoverMTGARawCardDBPath()
	if path == "" {
		return nil
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	raw, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	placeholders := make([]string, len(pack))
	args := make([]any, len(pack))
	for i, card := range pack {
		placeholders[i] = "?"
		args[i] = card.CardID
	}
	rows, err := raw.QueryContext(ctx, `SELECT GrpId, Order_MythicToCommon, Order_ColorOrder, Order_Title FROM Cards WHERE GrpId IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type order struct {
		rarity, color int64
		title         string
	}
	orders := make(map[int64]order, len(pack))
	for rows.Next() {
		var id int64
		var rarity, color sql.NullInt64
		var title sql.NullString
		if err := rows.Scan(&id, &rarity, &color, &title); err != nil {
			return nil
		}
		if !rarity.Valid || !color.Valid || !title.Valid || strings.TrimSpace(title.String) == "" {
			return nil
		}
		orders[id] = order{rarity.Int64, color.Int64, title.String}
	}
	if rows.Err() != nil {
		return nil
	}
	for _, card := range pack {
		if _, ok := orders[card.CardID]; !ok {
			return nil
		}
	}
	display := slices.Clone(pack)
	slices.SortStableFunc(display, func(a, b model.DraftPickCard) int {
		x, y := orders[a.CardID], orders[b.CardID]
		if n := cmp.Compare(x.rarity, y.rarity); n != 0 {
			return n
		}
		if n := cmp.Compare(x.color, y.color); n != 0 {
			return n
		}
		return cmp.Compare(x.title, y.title)
	})
	return display
}
