// Command voice-studio is the standalone voice studio.
//
// It is a SEPARATE PROGRAM rather than a tab in the pet, for a reason the user
// gave directly: the pet's panel is a fixed 380x520 rectangle cropped by the OS,
// and a workspace that lists five lines with several takes each does not belong in
// it. Splitting them also means the pet does not carry a rendering UI it rarely
// needs.
//
// What it shares with the pet is everything that must not be duplicated:
//
//   - internal/voice, the rendered library. Both processes read and write the SAME
//     directory (%AppData%\PetPC\voice\user), and the library notices when the
//     other one has written, so a take chosen here takes effect in the pet without
//     restarting either side.
//   - internal/tts, the engine client and launcher.
//   - internal/store, only for DefaultDir and the engine defaults.
//
// Nothing here is on the pet's playback path: the pet plays WAV files and never
// talks to this program or to the engine.
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
	app, err := NewStudio()
	if err != nil {
		// A studio that cannot open its library has nothing to show, and the
		// message is the whole diagnosis, so it is worth failing loudly here.
		log.Fatalf("voice-studio: %v", err)
	}

	err = wails.Run(&options.App{
		Title:     "配音工坊 · PetPC",
		Width:     940,
		Height:    700,
		MinWidth:  760,
		MinHeight: 520,
		// A normal, resizable, framed window: this is a workbench the user keeps
		// open beside the pet, not an always-on-top overlay, so it must behave like
		// every other desktop window (minimise, resize, move behind things).
		//
		// Wails v2 spells this as the inverse of the pet's setting: the pet passes
		// DisableResize, this leaves resizing ENABLED.
		DisableResize: false,
		Frameless:     false,
		// Deliberately NOT always-on-top: it would fight with whatever the user is
		// actually working in.
		AlwaysOnTop:      false,
		BackgroundColour: &options.RGBA{R: 24, G: 26, B: 32, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("voice-studio: %v", err)
	}
}
