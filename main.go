package main

import (
	"context"
	"embed"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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

	app := NewApp(assets)
	if err := wails.Run(&options.App{
		Title:            appDisplayName,
		Width:            1480,
		Height:           960,
		MinWidth:         1200,
		MinHeight:        760,
		BackgroundColour: &options.RGBA{R: 8, G: 12, B: 21, A: 1},
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: app.APIMiddleware,
		},
		// Closing the window keeps the app (and the live log tailer) running;
		// reopen it from the Dock. Quit fully with Cmd+Q. This is the Wails v2
		// stand-in for a tray icon, which needs Wails v3.
		HideWindowOnClose: true,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "dev.ixianlabs.mtgdata",
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
		OnStartup: app.startup,
		OnShutdown: func(_ context.Context) {
			app.shutdown()
		},
		Bind: []any{
			app,
		},
	}); err != nil {
		log.Fatalf("run wails app: %v", err)
	}
}
