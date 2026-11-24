package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/solean/ponder/internal/appstate"
	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// menuBarStatus is everything the native status item renders. The dropdown
// header reads "Live Tracking" with one status line beneath it and a switch
// beside it.
type menuBarStatus struct {
	status    string
	activity  string // Follows status on the same line; may be empty.
	on        bool   // Switch position.
	canToggle bool
	indicator string // Text beside the menu bar icon.
	detail    string // Tooltip only.
	update    string // Label of the "Update Available" item; empty hides it.
}

// statusLine joins status and activity so the header is always two lines tall:
// toggling tracking changes the text but never the menu's height.
func (s menuBarStatus) statusLine() string {
	if s.activity == "" || s.activity == s.status {
		return s.status
	}
	return s.status + " · " + s.activity
}

func (s menuBarStatus) tooltip() string {
	tooltip := appDisplayName + " — " + s.status
	for _, line := range []string{s.activity, s.detail} {
		if line != "" && line != s.status {
			tooltip += "\n" + line
		}
	}
	return tooltip
}

// menuBarLiveMatch is the glanceable state of the match being tracked.
type menuBarLiveMatch struct {
	opponent string
	deckName string
	game     int64
	turn     int64 // Full turn, not Arena's per-player turn.
}

const detailedLogsSteps = "In Arena, open Options → Account, check Detailed Logs (Plugin Support), then restart Arena"

func trackingMenuBarStatus(status *appstate.Status, startupErr string, live *menuBarLiveMatch, now time.Time) menuBarStatus {
	if startupErr != "" {
		return menuBarStatus{status: "Unavailable", activity: startupErr, indicator: "!", detail: startupErr}
	}
	if status == nil {
		return menuBarStatus{status: "Starting…", detail: "Starting the tracking service"}
	}
	if status.LastError != "" {
		return menuBarStatus{status: "Needs attention", activity: status.LastError, on: status.LiveRunning, canToggle: true, indicator: "!", detail: status.LastError}
	}
	if status.LiveRunning && !status.ActiveLogPathExists {
		return menuBarStatus{status: "Waiting for MTGA log", on: true, canToggle: true, indicator: "!", detail: status.ActiveLogPath}
	}
	// Without detailed logs Arena writes no match, draft, or rank data, so
	// tracking runs but captures nothing until the user changes the setting.
	if status.DetailedLogs == appstate.DetailedLogsDisabled {
		return menuBarStatus{
			status:    "Detailed Logs off",
			activity:  "Turn on in Arena settings",
			on:        status.LiveRunning,
			canToggle: true,
			indicator: "!",
			detail:    detailedLogsSteps,
		}
	}
	if !status.LiveRunning {
		return menuBarStatus{status: "Off", canToggle: true, detail: "Turn on live tracking from this menu or in Ponder"}
	}
	detail := "Watching " + status.ActiveLogPath
	if live != nil {
		label := "In a match"
		if live.opponent != "" {
			label = "In match vs " + live.opponent
		}
		var progress []string
		if live.game > 0 {
			progress = append(progress, fmt.Sprintf("Game %d", live.game))
		}
		if live.turn > 0 {
			progress = append(progress, fmt.Sprintf("Turn %d", live.turn))
		}
		if live.deckName != "" {
			progress = append(progress, live.deckName)
		}
		return menuBarStatus{status: label, activity: strings.Join(progress, " · "), on: true, canToggle: true, detail: detail}
	}
	return menuBarStatus{status: "On", activity: lastActivityLabel(status.LastLiveActivity, now), on: true, canToggle: true, detail: detail}
}

// updateMenuLabel names the release the dropdown offers, or "" when there is
// nothing to open. The item links to the release page, so it needs a URL.
func updateMenuLabel(status *appstate.Status) string {
	check := updateToOffer(status)
	if check == nil {
		return ""
	}
	if check.LatestVersion == "" {
		return "Update Available"
	}
	return "Update Available: Version " + check.LatestVersion
}

func updateToOffer(status *appstate.Status) *appstate.UpdateCheck {
	if status == nil || status.UpdateCheck == nil {
		return nil
	}
	check := status.UpdateCheck
	if !check.UpdateAvailable || !strings.HasPrefix(check.ReleaseURL, "https://") {
		return nil
	}
	return check
}

// lastActivityLabel ages the newest log read that captured anything. The
// tracker only records activity since it started, so nil means a quiet log.
func lastActivityLabel(activity *appstate.OperationResult, now time.Time) string {
	if activity == nil {
		return "No MTGA activity yet"
	}
	at, err := time.Parse(time.RFC3339Nano, activity.CompletedAt)
	if err != nil {
		return ""
	}
	switch age := now.Sub(at); {
	case age < time.Minute:
		return "MTGA activity just now"
	case age < time.Hour:
		return fmt.Sprintf("MTGA activity %d min ago", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("MTGA activity %d hr ago", int(age/time.Hour))
	default:
		return "MTGA activity on " + at.Local().Format("Jan 2")
	}
}

// loadMenuBarLiveMatch uses the overlay's activity window so an abandoned
// match never shows as being tracked.
func loadMenuBarLiveMatch(ctx context.Context, store *db.Store) (*menuBarLiveMatch, error) {
	id, ok, err := store.GetLiveMatchIDActiveSince(ctx, time.Now().Add(-liveMatchActivityWindow))
	if err != nil || !ok {
		return nil, err
	}
	opponent, deckName, err := store.GetLiveMatchHeadline(ctx, id)
	if err != nil {
		return nil, err
	}
	game, arenaTurn, err := store.GetLiveProgress(ctx, id)
	if err != nil {
		return nil, err
	}
	return &menuBarLiveMatch{opponent: opponent, deckName: deckName, game: game, turn: model.ArenaTurnToFullTurn(arenaTurn)}, nil
}

// menuBarCallbacks routes native status item actions into the app. The native
// side invokes them on their own goroutine, never on the main thread.
type menuBarCallbacks struct {
	toggle     func(on bool)
	overlay    func()
	openUpdate func()
	open       func()
	quit       func()
}

// menuBarActions is assigned before the status item exists and never changes.
var menuBarActions menuBarCallbacks

// startMenuBar shows the status item and keeps it current. Call it off the main
// thread once the application has started. It runs alongside backend startup,
// so the item reads "Starting…" until the tracking service is ready.
func (a *App) startMenuBar() {
	if !menuBarSupported {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.shuttingDown {
		a.mu.Unlock()
		cancel()
		return
	}
	a.trayCancel = cancel
	a.mu.Unlock()

	// The switch moves as soon as it is clicked. Refreshing re-renders
	// unconditionally so an ignored or failed toggle snaps back at once.
	refresh := make(chan struct{}, 1)
	requestRefresh := func() {
		select {
		case refresh <- struct{}{}:
		default:
		}
	}
	var toggling atomic.Bool
	menuBarActions = menuBarCallbacks{
		toggle: func(on bool) {
			defer requestRefresh()
			a.mu.RLock()
			service := a.runtimeService
			a.mu.RUnlock()
			if service == nil || !toggling.CompareAndSwap(false, true) {
				return
			}
			defer toggling.Store(false)
			var err error
			if on {
				_, err = service.StartLive()
			} else {
				_, err = service.StopLive()
			}
			if err != nil {
				log.Printf("menu bar live tracking toggle failed: %v", err)
			}
		},
		overlay: a.toggleOverlay,
		// Reads the current result rather than the rendered one, so a click
		// never opens a release that a newer check has replaced.
		openUpdate: func() {
			a.mu.RLock()
			service := a.runtimeService
			a.mu.RUnlock()
			if service == nil {
				return
			}
			status := service.Status()
			check := updateToOffer(&status)
			if check == nil {
				return
			}
			if err := a.OpenURL(check.ReleaseURL); err != nil {
				log.Printf("menu bar update link failed: %v", err)
			}
		},
		open: func() { a.onSecondInstanceLaunch(application.SecondInstanceData{}) },
		quit: func() { a.wailsApp.Quit() },
	}
	application.InvokeSync(func() {
		menuBarCreate(menuBarIcon(true), menuBarIcon(false), "Live Tracking", "Open "+appDisplayName, "Quit "+appDisplayName)
	})
	a.mu.RLock()
	hidden := a.overlayHidden
	a.mu.RUnlock()
	a.syncOverlayMenuItem(hidden)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var previous menuBarStatus
		force := true
		hadLiveError := false
		for {
			a.mu.RLock()
			service, store, startupErr := a.runtimeService, a.store, a.startupErr
			a.mu.RUnlock()
			var status *appstate.Status
			var live *menuBarLiveMatch
			if service != nil {
				snapshot := service.Status()
				status = &snapshot
				if snapshot.LiveRunning && store != nil {
					var err error
					if live, err = loadMenuBarLiveMatch(ctx, store); err != nil && !hadLiveError {
						log.Printf("menu bar live-match check failed: %v", err)
					}
					hadLiveError = err != nil
				}
			}
			next := trackingMenuBarStatus(status, startupErr, live, time.Now())
			next.update = updateMenuLabel(status)
			if force || next != previous {
				application.InvokeSync(func() {
					if ctx.Err() == nil {
						menuBarRender(next)
					}
				})
				previous, force = next, false
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-refresh:
				force = true
			}
		}
	}()
}

// Three monochrome orbs echo Ponder's app icon: filled while live tracking
// runs, hollow rings while it is off. Supersampling keeps the small template
// image crisp on Retina displays; macOS supplies light/dark coloring.
func menuBarIcon(filled bool) []byte {
	innerRadius := 4.5
	if filled {
		innerRadius = 0
	}
	const size = 44
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	centers := [][2]float64{{22, 12}, {12, 30}, {32, 30}}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			covered := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px, py := float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4
					for _, center := range centers {
						distance := math.Hypot(px-center[0], py-center[1])
						if distance >= innerRadius && distance <= 8 {
							covered++
							break
						}
					}
				}
			}
			icon.SetNRGBA(x, y, color.NRGBA{A: uint8(covered * 255 / 16)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}
