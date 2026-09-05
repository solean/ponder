package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:web/dist
var embeddedAssets embed.FS

// App display name — change here to rebrand. Keep web/src/lib/branding.ts in sync.
const appDisplayName = "Ponder"

func main() {
	assets, err := fs.Sub(embeddedAssets, "web/dist")
	if err != nil {
		log.Fatalf("prepare embedded web assets: %v", err)
	}

	desktop := NewApp(assets)
	wailsApp := application.New(application.Options{
		Name:        appDisplayName,
		Description: "Private, local-first MTG Arena match tracking and analytics.",
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: desktop.APIMiddleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "dev.ixianlabs.ponder",
			OnSecondInstanceLaunch: desktop.onSecondInstanceLaunch,
		},
		OnShutdown: desktop.shutdown,
	})

	mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            appDisplayName,
		Width:            1480,
		Height:           960,
		MinWidth:         1200,
		MinHeight:        760,
		BackgroundColour: application.NewRGBA(8, 12, 21, 255),
		URL:              "/",
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHidden,
			InvisibleTitleBarHeight: 32,
		},
	})

	overlayWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:              "overlay",
		Title:             appDisplayName + " Overlay",
		Width:             1440,
		Height:            900,
		AlwaysOnTop:       true,
		URL:               "/overlay",
		DisableResize:     true,
		Frameless:         true,
		BackgroundType:    application.BackgroundTypeTransparent,
		BackgroundColour:  application.NewRGBA(0, 0, 0, 0),
		InitialPosition:   application.WindowXY,
		Hidden:            true,
		IgnoreMouseEvents: true,
		Mac: application.MacWindow{
			Backdrop:      application.MacBackdropTransparent,
			DisableShadow: true,
			CornerType:    application.MacWindowCornerTypeSquare,
			WindowLevel:   application.MacWindowLevelScreenSaver,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle |
				application.MacWindowCollectionBehaviorStationary,
		},
		MinimiseButtonState:   application.ButtonHidden,
		MaximiseButtonState:   application.ButtonHidden,
		CloseButtonState:      application.ButtonHidden,
		FullscreenButtonState: application.ButtonHidden,
	})
	desktop.setDesktopRuntime(wailsApp, mainWindow, overlayWindow)

	// Closing the main window keeps the log tailer running. The Dock icon or a
	// second launch restores the existing window; Cmd+Q still quits the app.
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		mainWindow.Hide()
		event.Cancel()
	})
	overlayWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		hideOverlayWindow(overlayWindow)
		event.Cancel()
	})
	wailsApp.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		mainWindow.Show()
		mainWindow.Focus()
	})
	wailsApp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		desktop.startup()
		desktop.showStartupError()
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatalf("run wails app: %v", err)
	}
}
