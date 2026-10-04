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
	"runtime"
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

	// Cursor polling drives previews only; the native window never captures input.
	overlayPointerPollInterval = time.Second / 30

	// mtgaBundleID identifies the MTG Arena client the overlay belongs to. The
	// overlay is bound to that application: it is only ever on screen while
	// Arena is frontmost, and never floats above other apps.
	mtgaBundleID = "com.wizards.mtga"

	// A live match with no observed game activity for this long is treated as
	// abandoned (Arena quit mid-match leaves the row in progress forever), so
	// the overlay stops following it. Well above any in-game stall: every GRE
	// message, including mulligans and sideboard submissions, counts.
	overlayLiveActivityWindow = 10 * time.Minute
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

	overlayHidden     bool
	overlayWake       chan struct{}
	overlayMenuItem   *application.MenuItem
	overlayShortcut   string
	overlayShortcutMu sync.Mutex
}

func NewApp(staticAssets fs.FS) *App {
	return &App{staticAssets: staticAssets, overlayWake: make(chan struct{}, 1)}
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
			status := "failed"
			if message == "" {
				message = "backend is starting"
				status = "starting"
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "status": status})
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

// OpenURL satisfies api.Desktop by opening a link in the user's default browser.
func (a *App) OpenURL(rawURL string) error {
	if a.wailsApp == nil {
		return fmt.Errorf("desktop runtime not ready")
	}
	return a.wailsApp.Browser.OpenURL(rawURL)
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
		Store:                  store,
		DBPath:                 dbPath,
		SupportDir:             supportDir,
		OverlayShortcutChanged: a.updateOverlayShortcut,
		DefaultLogPath:         currentLogPath,
		DefaultPrevLogPath:     prevLogPath,
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

	if err := a.updateOverlayShortcut(runtimeService.OverlaySettings().Shortcut); err != nil {
		log.Printf("overlay shortcut unavailable: %v; use the Overlay menu", err)
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

// overlayShownScript forces the overlay's live query to refetch. A window that
// was ordered out can have its webview timers throttled or suspended, so the
// HUD would otherwise come back carrying whatever it last managed to poll.
const overlayShownScript = "window.dispatchEvent(new Event('ponder:overlay-shown'))"

func overlayPointerScript(x, y float64, supported bool) string {
	detail := "null"
	if supported && x >= 0 && x < 1 && y >= 0 && y < 1 {
		detail = fmt.Sprintf("{x:%f,y:%f}", x, y)
	}
	return "window.dispatchEvent(new CustomEvent('ponder:overlay-pointer',{detail:" + detail + "}))"
}

// overlayMenuLabel names the action the item performs, because the item is the
// only place the mechanism is visible: during a match the app runs as an
// accessory (no Dock icon, no menu bar), so the global shortcut is the only
// in-game control. The keystroke is part of the label rather than a menu
// accelerator: the global shortcut already claims it everywhere, and a key
// equivalent would fire the same toggle a second time whenever Ponder is
// focused.
func overlayMenuLabel(hidden bool) string {
	return overlayMenuLabelWithShortcut(hidden, "CmdOrCtrl+Shift+O")
}

func overlayMenuLabelWithShortcut(hidden bool, shortcut string) string {
	if shortcut == "" {
		if hidden {
			return "Show Game Overlay (shortcut unavailable)"
		}
		return "Hide Game Overlay (shortcut unavailable)"
	}
	if runtime.GOOS == "darwin" {
		shortcut = strings.ReplaceAll(shortcut, "CmdOrCtrl", "⌘")
		shortcut = strings.ReplaceAll(shortcut, "Cmd", "⌘")
		shortcut = strings.ReplaceAll(shortcut, "Super", "⌘")
		shortcut = strings.ReplaceAll(shortcut, "Ctrl", "⌃")
		shortcut = strings.ReplaceAll(shortcut, "Alt", "⌥")
		shortcut = strings.ReplaceAll(shortcut, "Shift", "⇧")
		shortcut = strings.ReplaceAll(shortcut, "+", "")
	} else {
		shortcut = strings.ReplaceAll(shortcut, "CmdOrCtrl", "Ctrl")
	}
	if hidden {
		return fmt.Sprintf("Show Game Overlay (%s)", shortcut)
	}
	return fmt.Sprintf("Hide Game Overlay (%s)", shortcut)
}

func (a *App) setOverlayMenuItem(item *application.MenuItem) {
	a.mu.Lock()
	a.overlayMenuItem = item
	hidden := a.overlayHidden
	a.mu.Unlock()
	a.syncOverlayMenuItem(hidden)
}

// syncOverlayMenuItem keeps the label pointed at the next action. InvokeSync
// short-circuits when already on the main thread, so a menu click can call it.
func (a *App) syncOverlayMenuItem(hidden bool) {
	a.mu.RLock()
	item := a.overlayMenuItem
	shortcut := a.overlayShortcut
	a.mu.RUnlock()
	if item == nil {
		return
	}
	label := overlayMenuLabelWithShortcut(hidden, shortcut)
	application.InvokeSync(func() { item.SetLabel(label) })
}

func (a *App) toggleOverlay() {
	a.mu.Lock()
	a.overlayHidden = !a.overlayHidden
	hidden := a.overlayHidden
	a.mu.Unlock()
	state := "shown"
	if hidden {
		state = "hidden"
	}
	log.Printf("overlay %s by user", state)
	a.syncOverlayMenuItem(hidden)
	a.wakeOverlayMonitor()
}

func (a *App) hideOverlay() {
	a.mu.Lock()
	a.overlayHidden = true
	a.mu.Unlock()
	a.syncOverlayMenuItem(true)
	a.wakeOverlayMonitor()
}

func (a *App) wakeOverlayMonitor() {
	select {
	case a.overlayWake <- struct{}{}:
	default:
	}
}

func (a *App) startOverlayMonitor(ctx context.Context, store *db.Store) {
	if a.overlayWindow == nil || a.wailsApp == nil {
		return
	}

	go func() {
		stateTicker := time.NewTicker(time.Second)
		pointerTicker := time.NewTicker(overlayPointerPollInterval)
		defer stateTicker.Stop()
		defer pointerTicker.Stop()

		attached := false
		visible := false
		hadReadError := false

		setVisible := func(next bool) {
			if next == visible {
				return
			}
			visible = next
			if next {
				a.overlayWindow.ExecJS(overlayShownScript)
			}
		}
		detach := func() {
			if !attached {
				return
			}
			detachOverlayWindow(a.overlayWindow)
			attached = false
			setVisible(false)
			log.Printf("overlay window detached")
		}
		updateState := func() {
			_, isLive, err := store.GetLiveMatchIDActiveSince(ctx, time.Now().Add(-overlayLiveActivityWindow))
			if err != nil {
				if !hadReadError {
					log.Printf("overlay live-state check failed: %v", err)
					hadReadError = true
				}
				return
			}
			hadReadError = false
			a.mu.RLock()
			hidden := a.overlayHidden
			a.mu.RUnlock()

			// Detaching whenever Arena is gone keeps the accessory activation
			// policy (and the missing Dock icon) scoped to an actual game.
			if !isLive || hidden || !overlayTargetRunning(mtgaBundleID) {
				detach()
				return
			}
			if attached {
				setVisible(syncOverlayWindow(a.overlayWindow))
				return
			}

			// Hidden webviews may suspend timers. Navigate explicitly: Reload
			// can cancel the first pending navigation before any URL commits.
			a.overlayWindow.SetIgnoreMouseEvents(true)
			a.overlayWindow.SetURL("/overlay")
			configured, level, behavior := attachOverlayWindow(a.overlayWindow, mtgaBundleID)
			if !configured {
				log.Printf("overlay window did not accept the required overlay configuration (level=%d behavior=%#x)", level, behavior)
				setVisible(false)
				return
			}
			attached = true
			log.Printf("overlay window attached to %s (level=%d behavior=%#x)", mtgaBundleID, level, behavior)
			setVisible(syncOverlayWindow(a.overlayWindow))
		}
		updatePointer := func() {
			if !visible {
				return
			}
			x, y, supported := overlayPointerPosition(a.overlayWindow)
			a.overlayWindow.ExecJS(overlayPointerScript(x, y, supported))
		}

		updateState()
		for {
			select {
			case <-ctx.Done():
				return
			case <-a.overlayWake:
				updateState()
			case <-stateTicker.C:
				updateState()
			case <-pointerTicker.C:
				updatePointer()
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

// updateOverlayShortcut registers the replacement before releasing the previous
// binding, so a rejected shortcut leaves the current control available.
func (a *App) updateOverlayShortcut(shortcut string) error {
	a.overlayShortcutMu.Lock()
	defer a.overlayShortcutMu.Unlock()
	a.mu.RLock()
	previous := a.overlayShortcut
	a.mu.RUnlock()
	if shortcut == previous {
		return nil
	}
	if a.wailsApp != nil {
		if previous != "" && a.wailsApp.GlobalShortcut.IsRegistered(shortcut) {
			return fmt.Errorf("shortcut is already registered; use the existing shortcut spelling %s", previous)
		}
		if err := a.wailsApp.GlobalShortcut.Register(shortcut, a.toggleOverlay); err != nil {
			return fmt.Errorf("overlay shortcut unavailable: %w", err)
		}
		if previous != "" {
			if err := a.wailsApp.GlobalShortcut.Unregister(previous); err != nil {
				_ = a.wailsApp.GlobalShortcut.Unregister(shortcut)
				return fmt.Errorf("replace overlay shortcut: %w", err)
			}
		}
	}
	a.mu.Lock()
	a.overlayShortcut = shortcut
	hidden := a.overlayHidden
	a.mu.Unlock()
	a.syncOverlayMenuItem(hidden)
	return nil
}
