package api

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
)

// newCardNameTestServer returns a server whose MTGA raw card database knows no
// cards and whose Scryfall responses come from respond.
func newCardNameTestServer(t *testing.T, respond func() (int, string)) (*Server, *db.Store, *int) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	rawPath := filepath.Join(dir, "Raw_CardDatabase_test.mtga")
	raw, err := sql.Open("sqlite", rawPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE Cards (GrpId INTEGER PRIMARY KEY, TitleId INTEGER, AltTitleId INTEGER, InterchangeableTitleId INTEGER)`,
		`CREATE TABLE Localizations_enUS (LocId INT NOT NULL, Formatted INT NOT NULL, Loc TEXT, PRIMARY KEY (LocId, Formatted))`,
	} {
		if _, err := raw.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mtgaRawCardDBEnvVar, rawPath)

	database, err := db.Open(filepath.Join(dir, "ponder.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	server := NewServer(store, "", nil)

	requests := 0
	server.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			status, body := respond()
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
	return server, store, &requests
}

func TestUnresolvableCardNameIsNotRefetchedUntilRetryInterval(t *testing.T) {
	ctx := context.Background()
	status, body := http.StatusNotFound, `{"object":"error","code":"not_found"}`
	server, store, requests := newCardNameTestServer(t, func() (int, string) { return status, body })
	ids := []int64{999001}

	for range 3 {
		server.scryfallNextRequest = time.Time{}
		if got := server.resolveCardNames(ctx, ids); len(got) != 0 {
			t.Fatalf("resolveCardNames = %v, want no names", got)
		}
	}
	if *requests != 1 {
		t.Fatalf("Scryfall requests = %d, want 1", *requests)
	}

	// Once the miss is older than the retry interval the card is asked about
	// again, and a name found then replaces the miss.
	stale := time.Now().Add(-cardNameMissRetryInterval - time.Minute)
	if err := store.RecordCardNameMisses(ctx, ids, stale); err != nil {
		t.Fatal(err)
	}
	status, body = http.StatusOK, `{"data":[{"arena_id":999001,"name":"Late Arrival"}],"has_more":false}`
	server.scryfallNextRequest = time.Time{}
	if got := server.resolveCardNames(ctx, ids); got[999001] != "Late Arrival" {
		t.Fatalf("resolveCardNames after retry interval = %v, want Late Arrival", got)
	}
	if *requests != 2 {
		t.Fatalf("Scryfall requests = %d, want 2", *requests)
	}
	misses, err := store.RecentCardNameMisses(ctx, ids, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(misses) != 0 {
		t.Fatalf("card name misses after resolving = %v, want none", misses)
	}
}

func TestFailedScryfallLookupIsNotRememberedAsMiss(t *testing.T) {
	ctx := context.Background()
	server, store, requests := newCardNameTestServer(t, func() (int, string) {
		return http.StatusInternalServerError, "upstream unavailable"
	})
	ids := []int64{999002}

	for range 2 {
		server.scryfallNextRequest = time.Time{}
		server.resolveCardNames(ctx, ids)
	}
	if *requests != 2 {
		t.Fatalf("Scryfall requests = %d, want 2", *requests)
	}
	misses, err := store.RecentCardNameMisses(ctx, ids, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(misses) != 0 {
		t.Fatalf("card name misses after failed lookups = %v, want none", misses)
	}
}
