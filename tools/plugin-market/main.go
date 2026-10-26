// Command plugin-market is the standalone plugin market and manager.
//
// It is a SEPARATE PROGRAM rather than the pet's 「插件」tab, for the reason the
// voice studio is: the pet's panel is a fixed 380x520 rectangle cropped by the
// OS, and a plugin manager needs a list, a detail pane, a permission sheet and a
// settings form at the same time. The screenshots that prompted the move show
// exactly what happens otherwise - permission rows clipped by the right edge,
// the market address box half visible, and the dialogue and two lists fighting
// over one scrollbar.
//
// What it shares with the pet is everything that must not be duplicated:
//
//   - internal/plugins, the whole install / verify / grant / enable / uninstall
//     lifecycle and the market catalogue. There is no second implementation of
//     any of it.
//   - internal/store, only for DefaultDir, so both programs read and write the
//     SAME data directory (%AppData%\PetPC).
//   - <dataDir>/plugin-data/<id>/, the plugin's own settings, which this program
//     is the editor for.
//
// Its own preferences live in <dataDir>/plugin-market.json and it NEVER writes
// the pet's config - that file is a read-only migration source.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewMarket()
	if err != nil {
		// A market that cannot open the shared data directory has nothing to
		// show and the message is the whole diagnosis, so it is worth failing
		// loudly here (matching the voice studio).
		log.Fatalf("plugin-market: %v", err)
	}

	err = wails.Run(&options.App{
		Title:     "PetPC 插件市场",
		Width:     980,
		Height:    700,
		MinWidth:  720,
		MinHeight: 520,
		// A normal, resizable, framed window: this is a workbench the user keeps
		// open beside the pet, not an always-on-top overlay, so it must behave
		// like every other desktop window.
		DisableResize:    false,
		Frameless:        false,
		AlwaysOnTop:      false,
		BackgroundColour: &options.RGBA{R: 24, G: 26, B: 32, A: 255},
		AssetServer: &assetserver.Options{
			// Either the embedded frontend or, with PETPC_CC set, the control center's
			// build from disk. See uiAssets.
			Assets: uiAssets(assets),
			// The plugin's OWN files are served from disk, not from the bundle: the
			// workshop this program draws for the voice plugin is an HTML document
			// inside the plugin's installed directory. It is the SAME handler the
			// pet uses (internal/plugins.ServePluginAsset), so the traversal
			// defence and the bridge protocol have one implementation rather than
			// two that can drift.
			Handler: app,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("plugin-market: %v", err)
	}
}
