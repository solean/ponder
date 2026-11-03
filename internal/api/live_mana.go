package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/solean/ponder/internal/model"
)

type liveManaObject struct {
	CardTypes []string `json:"cardTypes"`
	Abilities []struct {
		ID int64 `json:"grpId"`
	} `json:"uniqueAbilities"`
}

type liveManaAbility struct {
	Text string
	Mana bool
}

// Resolve the abilities actually present on each permanent, including granted
// abilities and the active face, rather than inferring production from colors
// or color identity. Arena's local catalog works offline and includes tokens.
func liveManaAbilities(ctx context.Context, objects []model.MatchReplayFrameObjectRow) map[int64]liveManaAbility {
	ids := map[int64]bool{}
	for _, object := range objects {
		var details liveManaObject
		if json.Unmarshal([]byte(object.DetailsJSON), &details) != nil {
			continue
		}
		for _, ability := range details.Abilities {
			if ability.ID > 0 {
				ids[ability.ID] = true
			}
		}
	}
	result := map[int64]liveManaAbility{}
	path := discoverMTGARawCardDBPath()
	if path == "" || len(ids) == 0 {
		return result
	}
	catalog, err := sql.Open("sqlite", path)
	if err != nil {
		return result
	}
	defer catalog.Close()
	placeholders, args := make([]string, 0, len(ids)), make([]any, 0, len(ids))
	for id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := catalog.QueryContext(ctx, `SELECT a.Id, a.Category = 1 AND a.SubCategory = 1,
 COALESCE((SELECT l.Loc FROM Localizations_enUS l WHERE l.LocId = a.TextId ORDER BY l.Formatted LIMIT 1), '')
 FROM Abilities a WHERE a.Id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ability liveManaAbility
		if rows.Scan(&id, &ability.Mana, &ability.Text) == nil {
			result[id] = ability
		}
	}
	return result
}

var arenaManaSymbols = regexp.MustCompile(`\{((?:o[WUBRGCT])+)\}`)
var manaAlternatives = regexp.MustCompile(`,\s*(?:or\s+)?|\s+or\s+`)
var manaSymbol = regexp.MustCompile(`\{([WUBRGC])\}`)

// Only unconditional, fixed-output tap abilities are counted. Filters, variable
// amounts, activation conditions and spending restrictions remain unresolved.
// This is an estimate, not a rules engine for replacement effects or combos.
func fixedManaProduction(text string) (int64, map[string]int64, bool) {
	text = arenaManaSymbols.ReplaceAllStringFunc(strings.TrimSpace(text), func(s string) string {
		return "{" + strings.ReplaceAll(strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{o"), "o", "}{") + "}"
	})
	cost, effect, found := strings.Cut(text, ": ")
	if !found || (cost != "{T}" && cost != "{T}, Sacrifice CARDNAME") {
		return 0, nil, false
	}
	if !strings.HasPrefix(effect, "Add ") || !strings.HasSuffix(effect, ".") {
		return 0, nil, false
	}
	output := strings.TrimSuffix(strings.TrimPrefix(effect, "Add "), ".")
	colors := map[string]int64{}
	for word, count := range map[string]int64{"one": 1, "two": 2, "three": 3} {
		if output == word+" mana of any color" || output == word+" mana of any one color" {
			for _, color := range []string{"W", "U", "B", "R", "G"} {
				colors[color] = count
			}
			return count, colors, true
		}
	}
	var total int64
	for _, option := range manaAlternatives.Split(output, -1) {
		option = strings.TrimSpace(option)
		if manaSymbol.ReplaceAllString(option, "") != "" {
			return 0, nil, false
		}
		symbols := manaSymbol.FindAllStringSubmatch(option, -1)
		if len(symbols) == 0 {
			return 0, nil, false
		}
		produced := map[string]int64{}
		for _, symbol := range symbols {
			produced[symbol[1]]++
		}
		total = max(total, int64(len(symbols)))
		for color, count := range produced {
			colors[color] = max(colors[color], count)
		}
	}
	return total, colors, true
}

func projectLiveMana(objects []model.MatchReplayFrameObjectRow, pool []model.ManaPoolEntry, poolKnown bool, abilities map[int64]liveManaAbility) *model.LiveMana {
	result := &model.LiveMana{ByColor: map[string]int64{}, PoolKnown: poolKnown, LandsKnown: true}
	for _, entry := range pool {
		color := map[string]string{"ManaColor_White": "W", "ManaColor_Blue": "U", "ManaColor_Black": "B", "ManaColor_Red": "R", "ManaColor_Green": "G", "ManaColor_Colorless": "C"}[entry.Color]
		if color != "" && entry.Count > 0 {
			result.ByColor[color] += entry.Count
			result.Floating += entry.Count
		}
	}
	result.Available = result.Floating
	for _, object := range objects {
		var details liveManaObject
		if json.Unmarshal([]byte(object.DetailsJSON), &details) != nil || details.CardTypes == nil {
			result.LandsKnown = false
			result.Unresolved++
			continue
		}
		land, creature := false, false
		for _, kind := range details.CardTypes {
			land = land || kind == "CardType_Land"
			creature = creature || kind == "CardType_Creature"
		}
		if land {
			result.Lands++
		}
		if object.IsTapped || (creature && object.HasSummoningSickness) {
			continue
		}
		var best int64
		colors := map[string]int64{}
		unresolved := false
		for _, ref := range details.Abilities {
			ability, known := abilities[ref.ID]
			if !known {
				unresolved = true
				continue
			}
			if !ability.Mana {
				continue
			}
			total, produced, supported := fixedManaProduction(ability.Text)
			if !supported {
				unresolved = true
				continue
			}
			best = max(best, total)
			for color, count := range produced {
				colors[color] = max(colors[color], count)
			}
		}
		if unresolved {
			result.Unresolved++
		}
		result.Available += best
		for color, count := range colors {
			result.ByColor[color] += count
		}
	}
	return result
}
