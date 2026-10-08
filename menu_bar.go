package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
	"time"

	"github.com/solean/ponder/internal/appstate"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type menuBarStatus struct {
	label     string
	indicator string
	detail    string
}

func trackingMenuBarStatus(status *appstate.Status, startupErr string) menuBarStatus {
	if startupErr != "" {
		return menuBarStatus{"Tracking unavailable", "!", startupErr}
	}
	if status == nil {
		return menuBarStatus{"Starting Ponder…", "", "Starting the tracking service"}
	}
	if status.LastError != "" {
		return menuBarStatus{"Tracking needs attention", "!", status.LastError}
	}
	if !status.LiveRunning {
		return menuBarStatus{"Live tracking is off", "Ⅱ", "Open Ponder to enable live tracking"}
	}
	if !status.ActiveLogPathExists {
		return menuBarStatus{"Waiting for MTGA log", "!", status.ActiveLogPath}
	}
	return menuBarStatus{"Tracking MTGA data", "", "Watching " + status.ActiveLogPath}
}

// Configure before Run so Wails owns the status item's creation and teardown.
// The returned monitor starts after backend startup, including failed startup.
func (a *App) prepareMenuBar() func() {
	if runtime.GOOS != "darwin" {
		return func() {}
	}
	tray := a.wailsApp.SystemTray.New()
	tray.SetTemplateIcon(menuBarIcon())
	menu := application.NewMenu()
	statusItem := menu.Add("Starting Ponder…").SetEnabled(false)
	menu.AddSeparator()
	menu.Add("Open " + appDisplayName).OnClick(func(*application.Context) {
		a.onSecondInstanceLaunch(application.SecondInstanceData{})
	})
	menu.AddSeparator()
	menu.Add("Quit " + appDisplayName).OnClick(func(*application.Context) { a.wailsApp.Quit() })
	tray.SetMenu(menu)
	tray.SetTooltip(appDisplayName + " — starting")
	return func() {
		ctx, cancel := context.WithCancel(context.Background())
		a.trayCancel = cancel
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			var previous menuBarStatus
			for {
				a.mu.RLock()
				service, startupErr := a.runtimeService, a.startupErr
				a.mu.RUnlock()
				var status *appstate.Status
				if service != nil {
					snapshot := service.Status()
					status = &snapshot
				}
				next := trackingMenuBarStatus(status, startupErr)
				if next != previous {
					application.InvokeSync(func() {
						if ctx.Err() != nil {
							return
						}
						statusItem.SetLabel(next.label)
						tray.SetLabel(next.indicator)
						tray.SetTooltip(appDisplayName + " — " + next.label + "\n" + next.detail)
					})
					previous = next
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
}

// Three monochrome orbs echo Ponder's app icon. Supersampling keeps the small
// template image crisp on Retina displays; macOS supplies light/dark coloring.
func menuBarIcon() []byte {
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
						if distance >= 4.5 && distance <= 8 {
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
