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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/solean/ponder/internal/api"
	"github.com/solean/ponder/internal/appstate"
	"github.com/solean/ponder/internal/db"
)

const (
	// devAPIEnvVar optionally exposes the API on a localhost port for browser
	// development (`bun run dev:desktop`). Set to an address ("127.0.0.1:39123")
	// or "1" for that default. The desktop webview itself never needs it: the API
	// is mounted on the Wails asset server, same-origin.
	devAPIEnvVar = "PONDER_DEV_API"

	// desktopDBEnvVar lets local Wails development reuse an existing database.
	// Installed apps use the application-support database when it is unset.
	desktopDBEnvVar = "PONDER_DB_PATH"

	// The transparent center must pass mouse input through to Arena. Only the
	// edge strips containing the two HUD panels become interactive.
	overlayInteractiveEdgeWidth = 430
	overlayPointerPollInterval  = time.Second / 30
)

type App struct {
	cancel        context.CancelFunc
	database      *sql.DB
	staticAssets  fs.FS
	wailsApp      *application.App
	mainWindow    application.Window
	overlayWindow application.Window

	mu         sync.RWMutex
	apiHandler http.Handler
	startupErr string
}

func NewApp(staticAssets fs.FS) *App {
	return &App{staticAssets: staticAssets}
}

func (a *App) setDesktopRuntime(
	wailsApp *application.App,
	mainWindow, overlayWindow application.Window,
) {
	a.wailsApp = wailsApp
	a.mainWindow = mainWindow
	a.overlayWindow = overlayWindow
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

func (a *App) onSecondInstanceLaunch(_ application.SecondInstanceData) {
	if a.mainWindow == nil {
		return
	}
	a.mainWindow.UnMinimise()
	a.mainWindow.Show()
	a.mainWindow.Focus()
}

// failStartup records a startup error for the API middleware. The
// ApplicationStarted handler shows it after Wails has finished initialising.
func (a *App) failStartup(stage string, err error) {
	message := fmt.Sprintf("%s: %v", stage, err)
	log.Printf("desktop startup failed: %s", message)

	a.mu.Lock()
	a.startupErr = message
	a.mu.Unlock()
}

func (a *App) showStartupError() {
	a.mu.RLock()
	message := a.startupErr
	a.mu.RUnlock()
	if message == "" || a.wailsApp == nil {
		return
	}

	dialog := a.wailsApp.Dialog.Error().
		SetTitle(appDisplayName + " failed to start").
		SetMessage(message + "\n\nThe app will stay open but cannot load data. Fix the issue and restart.")
	if a.mainWindow != nil {
		dialog.AttachToWindow(a.mainWindow)
	}
	dialog.Show()
}

// PickLogFile satisfies api.Desktop with a native open dialog. Returns "" if
// the user cancels.
func (a *App) PickLogFile() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("desktop runtime not ready")
	}
	defaultDir := ""
	if currentLogPath, _, err := appstate.DefaultMTGALogPaths(); err == nil {
		defaultDir = filepath.Dir(currentLogPath)
	}
	return a.wailsApp.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		CanChooseFiles:       true,
		CanChooseDirectories: false,
		Title:                "Choose MTGA log file",
		Directory:            defaultDir,
		Filters: []application.FileFilter{
			{DisplayName: "Log files (*.log)", Pattern: "*.log"},
			{DisplayName: "All files", Pattern: "*"},
		},
	}).PromptForSingleSelection()
}

// RevealPath satisfies api.Desktop: selects an existing file in the platform
// file manager, opens a directory, or falls back to the parent of a missing
// path.
func (a *App) RevealPath(path string) error {
	if a.wailsApp == nil {
		return fmt.Errorf("desktop runtime not ready")
	}
	info, err := os.Stat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		return a.wailsApp.Env.OpenFileManager(filepath.Dir(path), false)
	case err != nil:
		return err
	case info.IsDir():
		return a.wailsApp.Env.OpenFileManager(path, false)
	default:
		return a.wailsApp.Env.OpenFileManager(path, true)
	}
}

func (a *App) startup() {
	supportDir, err := appstate.DefaultSupportDir()
	if err != nil {
		a.failStartup("resolve support dir", err)
		return
	}
	if err := os.MkdirAll(supportDir, 0o755); err != nil {
		a.failStartup("create support dir", err)
		return
	}

	dbPath := desktopDatabasePath(supportDir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		a.failStartup("create database dir", err)
		return
	}
	log.Printf("using desktop database: %s", dbPath)
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
	a.startOverlayMonitor(bgCtx, store)

	devAddr := strings.TrimSpace(os.Getenv(devAPIEnvVar))
	if devAddr == "" && a.wailsApp != nil && a.wailsApp.Env.Info().Debug {
		// `wails3 dev` exposes the API locally so a regular browser at
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
		if result.ReplaysArchived > 0 || result.ArchivesRecompressed > 0 || result.RawEventsPruned > 0 || result.AnalyticsRefreshed > 0 {
			log.Printf("db maintenance: archived %d replays, recompressed %d archives, pruned %d raw events, refreshed %d analytics records",
				result.ReplaysArchived, result.ArchivesRecompressed, result.RawEventsPruned, result.AnalyticsRefreshed)
		}
	}()
}
func overlayPointInHUD(bounds application.Rect, x, y float64) bool {
	if bounds.Width <= 0 || bounds.Height <= 0 ||
		x < float64(bounds.X) || x >= float64(bounds.X+bounds.Width) ||
		y < float64(bounds.Y) || y >= float64(bounds.Y+bounds.Height) {
		return false
	}
	relativeX := x - float64(bounds.X)
	return relativeX <= overlayInteractiveEdgeWidth ||
		relativeX >= float64(bounds.Width-overlayInteractiveEdgeWidth)
}

func (a *App) startOverlayMonitor(ctx context.Context, store *db.Store) {
	if a.overlayWindow == nil || a.wailsApp == nil {
		return
	}

	go func() {
		visibilityTicker := time.NewTicker(time.Second)
		pointerTicker := time.NewTicker(overlayPointerPollInterval)
		defer visibilityTicker.Stop()
		defer pointerTicker.Stop()

		visible := false
		mouseInteractive := false
		hadReadError := false
		var overlayBounds application.Rect
		setMouseInteractive := func(interactive bool) {
			if interactive == mouseInteractive {
				return
			}
			mouseInteractive = interactive
			a.overlayWindow.SetIgnoreMouseEvents(!interactive)
		}
		updateVisibility := func() {
			_, isLive, err := store.GetLiveMatchID(ctx)
			if err != nil {
				if !hadReadError {
					log.Printf("overlay live-state check failed: %v", err)
					hadReadError = true
				}
				return
			}
			hadReadError = false
			if isLive == visible {
				return
			}
			visible = isLive
			if !isLive {
				setMouseInteractive(false)
				hideOverlayWindow(a.overlayWindow)
				return
			}

			if screen := a.wailsApp.Screen.GetPrimary(); screen != nil &&
				screen.Bounds.Width > 0 && screen.Bounds.Height > 0 {
				overlayBounds = screen.Bounds
				a.overlayWindow.SetBounds(overlayBounds)
			} else {
				overlayBounds = a.overlayWindow.Bounds()
			}
			// Hidden webviews may suspend timers. Reload on the hidden-to-live
			// transition so the first visible frame hydrates current match data.
			setMouseInteractive(false)
			a.overlayWindow.Reload()
			configured, level, behavior := showOverlayWindow(a.overlayWindow)
			if !configured {
				log.Printf("overlay window did not accept the required fullscreen configuration (level=%d behavior=%#x)", level, behavior)
			} else {
				log.Printf("overlay window configured for fullscreen (level=%d behavior=%#x)", level, behavior)
			}
		}
		updatePointerMode := func() {
			if !visible {
				return
			}
			x, y, supported := overlayPointerPosition()
			if !supported {
				// Wails has no portable cursor API. Keep hover behavior on other
				// platforms until they gain the native edge-hit-test bridge.
				setMouseInteractive(true)
				return
			}
			setMouseInteractive(overlayPointInHUD(overlayBounds, x, y))
		}

		updateVisibility()
		for {
			select {
			case <-ctx.Done():
				return
			case <-visibilityTicker.C:
				updateVisibility()
			case <-pointerTicker.C:
				updatePointerMode()
			}
		}
	}()
}

func desktopDatabasePath(supportDir string) string {
	if explicit := strings.TrimSpace(os.Getenv(desktopDBEnvVar)); explicit != "" {
		return filepath.Clean(explicit)
	}
	return filepath.Join(supportDir, "ponder.db")
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
