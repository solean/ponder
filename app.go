package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/cschnabel/mtgdata/internal/api"
	"github.com/cschnabel/mtgdata/internal/appstate"
	"github.com/cschnabel/mtgdata/internal/db"
)

// devAPIEnvVar optionally exposes the API on a localhost port for browser
// development (`bun run dev:desktop`). Set to an address ("127.0.0.1:39123")
// or "1" for that default. The desktop webview itself never needs it: the API
// is mounted on the Wails asset server, same-origin.
const devAPIEnvVar = "MTGDATA_DEV_API"

type App struct {
	ctx          context.Context
	cancel       context.CancelFunc
	database     *sql.DB
	staticAssets fs.FS

	mu         sync.RWMutex
	apiHandler http.Handler
	startupErr string
}

func NewApp(staticAssets fs.FS) *App {
	return &App{staticAssets: staticAssets}
}

// APIMiddleware mounts the backend API on the Wails asset server so the
// frontend reaches it same-origin: no listening port, no CORS exposure, no
// port collisions. Until startup finishes (or if it failed), API calls get a
// 503 carrying the startup error so the UI can render it.
func (a *App) APIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		a.mu.RLock()
		handler := a.apiHandler
		startupErr := a.startupErr
		a.mu.RUnlock()

		if handler == nil {
			message := startupErr
			if message == "" {
				message = "backend is starting"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func (a *App) onSecondInstanceLaunch(_ options.SecondInstanceData) {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.WindowShow(a.ctx)
}

// failStartup records a startup error for the API middleware and surfaces it
// in a native dialog instead of leaving the UI loading forever.
func (a *App) failStartup(stage string, err error) {
	message := fmt.Sprintf("%s: %v", stage, err)
	log.Printf("desktop startup failed: %s", message)

	a.mu.Lock()
	a.startupErr = message
	a.mu.Unlock()

	if a.ctx != nil {
		_, _ = wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
			Type:    wailsruntime.ErrorDialog,
			Title:   appDisplayName + " failed to start",
			Message: message + "\n\nThe app will stay open but cannot load data. Fix the issue and restart.",
		})
	}
}

// PickLogFile satisfies api.Desktop with a native open dialog. Returns "" if
// the user cancels.
func (a *App) PickLogFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("desktop context not ready")
	}
	defaultDir := ""
	if currentLogPath, _, err := appstate.DefaultMTGALogPaths(); err == nil {
		defaultDir = filepath.Dir(currentLogPath)
	}
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Choose MTGA log file",
		DefaultDirectory: defaultDir,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Log files (*.log)", Pattern: "*.log"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
}

// RevealPath satisfies api.Desktop: selects the file in Finder, or opens the
// directory. A missing file falls back to its parent directory so the button
// still lands somewhere useful.
func (a *App) RevealPath(path string) error {
	info, err := os.Stat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		return exec.Command("open", filepath.Dir(path)).Run()
	case err != nil:
		return err
	case info.IsDir():
		return exec.Command("open", path).Run()
	default:
		return exec.Command("open", "-R", path).Run()
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	supportBase, err := os.UserConfigDir()
	if err != nil {
		a.failStartup("resolve user config dir", err)
		return
	}

	supportDir := filepath.Join(supportBase, "mtgdata")
	if err := os.MkdirAll(supportDir, 0o755); err != nil {
		a.failStartup("create support dir", err)
		return
	}

	dbPath := filepath.Join(supportDir, "mtgdata.db")
	database, err := db.Open(dbPath)
	if err != nil {
		a.failStartup("open database", err)
		return
	}
	if err := db.Init(context.Background(), database); err != nil {
		_ = database.Close()
		a.failStartup("initialize database", err)
		return
	}

	store := db.NewStore(database)
	currentLogPath, prevLogPath, _ := appstate.DefaultMTGALogPaths()
	runtimeService, err := appstate.NewService(appstate.Options{
		Store:              store,
		DBPath:             dbPath,
		SupportDir:         supportDir,
		DefaultLogPath:     currentLogPath,
		DefaultPrevLogPath: prevLogPath,
		Capabilities: appstate.Capabilities{
			PickFile: true,
			Reveal:   true,
		},
	})
	if err != nil {
		_ = database.Close()
		a.failStartup("initialize runtime state", err)
		return
	}

	server := api.NewServer(store, "", runtimeService)
	server.SetDesktop(a)

	if started, err := runtimeService.MaybeAutoStartLive(); err != nil {
		log.Printf("auto-start live tracking failed: %v", err)
	} else if started {
		log.Printf("live tracking auto-started")
	}
	// The dev API listener below serves the whole app to a plain browser, so
	// give it the embedded frontend; deep links fall back to index.html there.
	server.SetStaticAssets(a.staticAssets)
	bgCtx, cancel := context.WithCancel(context.Background())
	server.StartUpdateChecker(bgCtx)

	a.database = database
	a.cancel = cancel
	a.mu.Lock()
	a.apiHandler = server.Handler()
	a.mu.Unlock()

	devAddr := strings.TrimSpace(os.Getenv(devAPIEnvVar))
	if devAddr == "" && wailsruntime.Environment(ctx).BuildType == "dev" {
		// `wails dev` always exposes the API locally so a regular browser at
		// the Vite dev server can reach it too. Production builds never listen.
		devAddr = "1"
	}
	if devAddr != "" {
		if devAddr == "1" || strings.EqualFold(devAddr, "true") {
			devAddr = "127.0.0.1:39123"
		}
		go func() {
			log.Printf("dev API listener enabled on %s", devAddr)
			if err := server.Run(bgCtx, devAddr); err != nil {
				log.Printf("dev API listener exited: %v", err)
			}
		}()
	}

	go func() {
		result, err := store.RunMaintenance(bgCtx)
		if err != nil {
			log.Printf("db maintenance failed (%+v): %v", result, err)
			return
		}
		if result.ReplaysArchived > 0 || result.ArchivesRecompressed > 0 || result.RawEventsPruned > 0 {
			log.Printf("db maintenance: archived %d replays, recompressed %d archives, pruned %d raw events",
				result.ReplaysArchived, result.ArchivesRecompressed, result.RawEventsPruned)
		}
	}()
}

func (a *App) shutdown() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.database != nil {
		_ = a.database.Close()
		a.database = nil
	}
}
