package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

// cardDefinitionsSyncState tracks which Arena raw database (path + data
// version) the builder catalog was last checked against, so repeated searches
// don't reopen Arena's database.
type cardDefinitionsSyncState struct {
	mu             sync.Mutex
	checkedPath    string
	checkedVersion string
}

// ensureCardDefinitions rebuilds the builder card catalog when the installed
// Arena raw database reports a new data version. Failures are non-fatal: the
// last successful catalog stays in place and search keeps working offline.
func (s *Server) ensureCardDefinitions(ctx context.Context) {
	s.cardDefsSync.mu.Lock()
	defer s.cardDefsSync.mu.Unlock()

	rawDBPath := discoverMTGARawCardDBPath()
	if strings.TrimSpace(rawDBPath) == "" {
		return
	}
	if s.cardDefsSync.checkedPath == rawDBPath && s.cardDefsSync.checkedVersion != "" {
		return
	}

	rawVersion, err := readMTGARawDataVersion(ctx, rawDBPath)
	if err != nil {
		log.Printf("card catalog version check failed: %v", err)
		return
	}

	storedVersion, err := s.store.CardDefinitionsVersion(ctx)
	if err != nil {
		log.Printf("card catalog stored version lookup failed: %v", err)
		return
	}
	count, err := s.store.CountCardDefinitions(ctx)
	if err != nil {
		log.Printf("card catalog count failed: %v", err)
		return
	}

	if storedVersion == rawVersion && count > 0 {
		s.cardDefsSync.checkedPath = rawDBPath
		s.cardDefsSync.checkedVersion = rawVersion
		return
	}

	defs, err := extractCardDefinitionsFromMTGARaw(ctx, rawDBPath)
	if err != nil {
		log.Printf("card catalog extraction failed: %v", err)
		return
	}
	if len(defs) == 0 {
		log.Printf("card catalog extraction returned no cards; keeping previous catalog")
		return
	}
	if err := s.store.ReplaceCardDefinitions(ctx, rawVersion, defs); err != nil {
		log.Printf("card catalog replace failed: %v", err)
		return
	}

	s.cardDefsSync.checkedPath = rawDBPath
	s.cardDefsSync.checkedVersion = rawVersion
	log.Printf("card catalog rebuilt: %d cards from Arena data version %s", len(defs), rawVersion)
}

func readMTGARawDataVersion(ctx context.Context, rawDBPath string) (string, error) {
	rawDB, err := sql.Open("sqlite", rawDBPath)
	if err != nil {
		return "", fmt.Errorf("open MTGA raw card db %q: %w", rawDBPath, err)
	}
	defer rawDB.Close()
	rawDB.SetMaxOpenConns(1)
	rawDB.SetMaxIdleConns(1)

	var version string
	if err := rawDB.QueryRowContext(ctx, `
		SELECT Version FROM Versions WHERE Type = 'Data'
	`).Scan(&version); err != nil {
		return "", fmt.Errorf("read MTGA raw data version: %w", err)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("MTGA raw data version is empty")
	}
	return version, nil
}

var mtgaRawRarityNames = map[int64]string{
	1: "land",
	2: "common",
	3: "uncommon",
	4: "rare",
	5: "mythic",
}

func extractCardDefinitionsFromMTGARaw(ctx context.Context, rawDBPath string) ([]db.CardDefinitionUpsert, error) {
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

	// Localizations carry one row per (LocId, Formatted) variant; the lowest
	// Formatted value is the plain-text form, so collapse to one row per
	// LocId or every joined card would be duplicated.
	rows, err := rawDB.QueryContext(ctx, `
		WITH loc AS (
			SELECT LocId, MIN(Formatted) AS Formatted, Loc
			FROM Localizations_enUS
			GROUP BY LocId
		)
		SELECT
			c.GrpId,
			COALESCE(
				NULLIF(TRIM(l1.Loc), ''),
				NULLIF(TRIM(l2.Loc), ''),
				NULLIF(TRIM(l3.Loc), '')
			) AS name,
			COALESCE(c.ExpansionCode, ''),
			COALESCE(c.CollectorNumber, ''),
			c.Rarity,
			COALESCE(c.OldSchoolManaText, ''),
			COALESCE(c.Colors, ''),
			COALESCE(c.ColorIdentity, ''),
			COALESCE(NULLIF(TRIM(tt.Loc), ''), ''),
			COALESCE(NULLIF(TRIM(st.Loc), ''), ''),
			c.Order_CMCWithXLast,
			c.IsDigitalOnly,
			c.IsRebalanced
		FROM Cards c
		LEFT JOIN loc l1 ON l1.LocId = c.TitleId
		LEFT JOIN loc l2 ON l2.LocId = c.AltTitleId
		LEFT JOIN loc l3 ON l3.LocId = c.InterchangeableTitleId
		LEFT JOIN loc tt ON tt.LocId = c.TypeTextId
		LEFT JOIN loc st ON st.LocId = c.SubtypeTextId
		WHERE c.IsPrimaryCard = 1 AND c.IsToken = 0
	`)
	if err != nil {
		return nil, fmt.Errorf("query MTGA raw card catalog: %w", err)
	}
	defer rows.Close()

	out := make([]db.CardDefinitionUpsert, 0, 20000)
	for rows.Next() {
		var (
			grpID                      int64
			name                       sql.NullString
			setCode, collectorNumber   string
			rarity                     int64
			rawManaText                string
			rawColors, rawColorIdent   string
			typeText, subtypeText      string
			orderCMC                   sql.NullInt64
			isDigitalOnly, isRebalance int64
		)
		if err := rows.Scan(
			&grpID, &name, &setCode, &collectorNumber, &rarity,
			&rawManaText, &rawColors, &rawColorIdent,
			&typeText, &subtypeText, &orderCMC,
			&isDigitalOnly, &isRebalance,
		); err != nil {
			return nil, fmt.Errorf("scan MTGA raw catalog row: %w", err)
		}
		if grpID <= 0 || !name.Valid || strings.TrimSpace(name.String) == "" {
			continue
		}

		def := db.CardDefinitionUpsert{
			CardDefinition: model.CardDefinition{
				ArenaID:         grpID,
				Name:            strings.TrimSpace(name.String),
				SetCode:         strings.ToUpper(strings.TrimSpace(setCode)),
				CollectorNumber: strings.TrimSpace(collectorNumber),
				Rarity:          mtgaRawRarityNames[rarity],
				ManaCost:        convertMTGARawManaCost(rawManaText),
				Colors:          parseMTGARawColorIdentity(rawColors),
				ColorIdentity:   parseMTGARawColorIdentity(rawColorIdent),
				TypeLine:        composeTypeLine(typeText, subtypeText),
				IsDigitalOnly:   isDigitalOnly != 0,
				IsRebalanced:    isRebalance != 0,
			},
			IsPrimary: true,
			IsToken:   false,
		}
		if orderCMC.Valid {
			manaValue := float64(orderCMC.Int64)
			// Arena sorts X spells last by adding 1000 to their converted
			// cost; strip the offset to recover the real mana value.
			if manaValue >= 1000 {
				manaValue -= 1000
			}
			def.ManaValue = &manaValue
		}
		out = append(out, def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate MTGA raw catalog rows: %w", err)
	}

	return out, nil
}

func composeTypeLine(typeText, subtypeText string) string {
	typeText = strings.TrimSpace(typeText)
	subtypeText = strings.TrimSpace(subtypeText)
	if typeText == "" {
		return subtypeText
	}
	if subtypeText == "" {
		return typeText
	}
	return typeText + " \u2014 " + subtypeText
}

// convertMTGARawManaCost translates Arena's "o"-prefixed mana notation
// (e.g. "o3oGoWoUoB", "oXoGoU", "o3o(U/P)") into conventional curly-brace
// symbols like "{3}{G}{W}{U}{B}".
func convertMTGARawManaCost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var out strings.Builder
	for _, token := range strings.Split(raw, "o") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		token = strings.TrimPrefix(token, "(")
		token = strings.TrimSuffix(token, ")")
		out.WriteString("{")
		out.WriteString(token)
		out.WriteString("}")
	}
	return out.String()
}
