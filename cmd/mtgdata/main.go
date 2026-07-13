package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cschnabel/mtgdata/internal/api"
	"github.com/cschnabel/mtgdata/internal/appstate"
	"github.com/cschnabel/mtgdata/internal/db"
	"github.com/cschnabel/mtgdata/internal/ingest"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := os.Args[1]
	switch cmd {
	case "parse":
		if err := runParse(ctx, os.Args[2:]); err != nil {
			log.Fatalf("parse failed: %v", err)
		}
	case "tail":
		if err := runTail(ctx, os.Args[2:]); err != nil {
			log.Fatalf("tail failed: %v", err)
		}
	case "serve":
		if err := runServe(ctx, os.Args[2:]); err != nil {
			log.Fatalf("serve failed: %v", err)
		}
	case "compact":
		if err := runCompact(ctx, os.Args[2:]); err != nil {
			log.Fatalf("compact failed: %v", err)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("mtgdata commands:")
	fmt.Println("  parse -db <path> [-log <path>] [-include-prev=true] [-resume=true]")
	fmt.Println("  tail  -db <path> [-log <path>] [-interval=2s] [-verbose=false]")
	fmt.Println("  serve -db <path> [-addr=:8080] [-web-dist=<path>]")
	fmt.Println("  compact -db <path>")
	fmt.Println("")
	fmt.Println("If -log is omitted, parse/tail default to:")
	fmt.Println("  ~/Library/Logs/Wizards Of The Coast/MTGA/Player.log")
	fmt.Println("parse also includes Player-prev.log by default.")
}

func runParse(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	dbPath := fs.String("db", "data/mtgdata.db", "sqlite database path")
	logPath := fs.String("log", "", "arena log path (optional; defaults to MTGA macOS path)")
	includePrev := fs.Bool("include-prev", true, "when -log is omitted, parse Player-prev.log before Player.log")
	resume := fs.Bool("resume", true, "resume from previous offset")
	if err := fs.Parse(args); err != nil {
		return err
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		return err
	}

	parser := ingest.NewParser(db.NewStore(database))

	logPaths, err := appstate.ResolveParseLogPaths(*logPath, *includePrev)
	if err != nil {
		return err
	}

	var totalLines int64
	var totalBytes int64
	var totalRawEvents int64
	var totalMatches int64
	var totalRankSnapshots int64
	var totalDecks int64
	var totalDraftPicks int64
	startedAt := time.Now().UTC()

	for _, path := range logPaths {
		stats, err := parser.ParseFile(ctx, path, *resume)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		duration := stats.CompletedAt.Sub(stats.StartedAt)
		log.Printf("parsed %s: lines=%d bytes=%d raw_events=%d matches=%d rank_snapshots=%d decks=%d draft_picks=%d duration=%s",
			path,
			stats.LinesRead,
			stats.BytesRead,
			stats.RawEventsStored,
			stats.MatchesUpserted,
			stats.RankSnapshots,
			stats.DecksUpserted,
			stats.DraftPicksAdded,
			duration,
		)

		totalLines += stats.LinesRead
		totalBytes += stats.BytesRead
		totalRawEvents += stats.RawEventsStored
		totalMatches += stats.MatchesUpserted
		totalRankSnapshots += stats.RankSnapshots
		totalDecks += stats.DecksUpserted
		totalDraftPicks += stats.DraftPicksAdded
	}

	log.Printf("parse complete (files=%d): lines=%d bytes=%d raw_events=%d matches=%d rank_snapshots=%d decks=%d draft_picks=%d duration=%s",
		len(logPaths),
		totalLines,
		totalBytes,
		totalRawEvents,
		totalMatches,
		totalRankSnapshots,
		totalDecks,
		totalDraftPicks,
		time.Since(startedAt),
	)

	compactReplays(ctx, db.NewStore(database))
	return nil
}

func compactReplays(ctx context.Context, store *db.Store) {
	started := time.Now()
	result, err := store.RunMaintenance(ctx)
	if err != nil {
		log.Printf("db maintenance failed (%+v): %v", result, err)
		return
	}
	if result.ReplaysArchived > 0 || result.ArchivesRecompressed > 0 || result.RawEventsPruned > 0 {
		log.Printf("db maintenance: archived %d replays, recompressed %d archives, pruned %d raw events in %s",
			result.ReplaysArchived, result.ArchivesRecompressed, result.RawEventsPruned, time.Since(started).Round(time.Millisecond))
	}
}

func runCompact(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compact", flag.ContinueOnError)
	dbPath := fs.String("db", "data/mtgdata.db", "sqlite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		return err
	}

	compactReplays(ctx, db.NewStore(database))
	return nil
}

func runTail(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tail", flag.ContinueOnError)
	dbPath := fs.String("db", "data/mtgdata.db", "sqlite database path")
	logPath := fs.String("log", "", "arena log path (optional; defaults to MTGA macOS Player.log)")
	interval := fs.Duration("interval", 2*time.Second, "poll interval")
	verbose := fs.Bool("verbose", false, "log each poll, including idle polls")
	if err := fs.Parse(args); err != nil {
		return err
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		return err
	}

	parser := ingest.NewParser(db.NewStore(database))
	activeLogPath := strings.TrimSpace(*logPath)
	if activeLogPath == "" {
		current, _, err := appstate.DefaultMTGALogPaths()
		if err != nil {
			return err
		}
		activeLogPath = current
	}
	if _, err := os.Stat(activeLogPath); err != nil {
		return fmt.Errorf("tail log path not found: %s (%w)", activeLogPath, err)
	}

	log.Printf("tailing %s every %s", activeLogPath, interval.String())

	go compactReplays(ctx, db.NewStore(database))

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		stats, err := parser.ParseFile(ctx, activeLogPath, true)
		if err != nil {
			log.Printf("tail parse error: %v", err)
		} else {
			hasActivity := stats.LinesRead > 0 ||
				stats.RawEventsStored > 0 ||
				stats.MatchesUpserted > 0 ||
				stats.DecksUpserted > 0 ||
				stats.DraftPicksAdded > 0

			if hasActivity {
				log.Printf(
					"tail activity: lines=%d bytes=%d raw_events=%d matches=%d decks=%d draft_picks=%d duration=%s",
					stats.LinesRead,
					stats.BytesRead,
					stats.RawEventsStored,
					stats.MatchesUpserted,
					stats.DecksUpserted,
					stats.DraftPicksAdded,
					stats.CompletedAt.Sub(stats.StartedAt),
				)
			} else if *verbose {
				log.Printf("tail idle: no new lines")
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dbPath := fs.String("db", "data/mtgdata.db", "sqlite database path")
	addr := fs.String("addr", ":8080", "http listen address")
	webDist := fs.String("web-dist", "", "path to built frontend dist")
	if err := fs.Parse(args); err != nil {
		return err
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := db.Init(ctx, database); err != nil {
		return err
	}

	staticDir := *webDist
	if staticDir == "" {
		cwd, err := os.Getwd()
		if err == nil {
			staticDir = api.DefaultStaticDir(cwd)
		}
	}
	if staticDir != "" {
		staticDir, _ = filepath.Abs(staticDir)
	}

	store := db.NewStore(database)
	currentLogPath, prevLogPath, _ := appstate.DefaultMTGALogPaths()
	runtimeService, err := appstate.NewService(appstate.Options{
		Store:              store,
		DBPath:             *dbPath,
		DefaultLogPath:     currentLogPath,
		DefaultPrevLogPath: prevLogPath,
	})
	if err != nil {
		return err
	}

	go compactReplays(ctx, store)

	if started, err := runtimeService.MaybeAutoStartLive(); err != nil {
		log.Printf("auto-start live tracking failed: %v", err)
	} else if started {
		log.Printf("live tracking auto-started")
	}

	server := api.NewServer(store, staticDir, runtimeService)
	server.StartUpdateChecker(ctx)
	return server.Run(ctx, *addr)
}
