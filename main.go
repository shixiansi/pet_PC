package main

import (
	"embed"
	"log"
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// assetHandler serves requests that the embedded bundle cannot satisfy.
//
// This is what makes a user-supplied pet picture work at all: the frontend only
// ever asks for "/pet-image", and this handler streams the file from the data
// directory. Pointing the webview at the user's absolute path would fail,
// because the bundled assets are compiled into the binary and the webview is
// not permitted to read arbitrary local files.
type assetHandler struct{ app *App }

func (h assetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/pet-image" {
		h.app.servePetImage(w, r)
		return
	}
	http.NotFound(w, r)
}

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("pet: failed to initialise: %v", err)
	}

	// The pet is deliberately small: a transparent, frameless, always-on-top
	// window just big enough for the picture (it grows when the todo panel
	// opens). Sizes are logical pixels; Windows scales them by the monitor DPI.
	petW, petH := app.store.Config().PetSize()

	err = wails.Run(&options.App{
		Title:         "PetPC",
		Width:         petW,
		Height:        petH,
		DisableResize: true,
		Frameless:     true,
		AlwaysOnTop:   true,
		// The window is created hidden and revealed by App.Ready, which the
		// frontend calls once it has painted the first layout.
		//
		// Wails creates the window with WS_OVERLAPPEDWINDOW and only hides the
		// frame later, so if it were shown immediately the first frames would
		// show a decorated "PetPC" window with the pet at its default position
		// and unstyled - the startup flash. Showing it after the page has
		// painted makes the first visible frame the finished pet.
		StartHidden: true,
		// Fully transparent: the webview draws the pet and nothing else.
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: assetHandler{app: app},
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			// Set the WebView2 default background to fully transparent. On its
			// own this is not enough (the Win32 window still paints its
			// background brush); desktop.MakeTransparent finishes the job by
			// compositing the whole client area.
			WebviewIsTransparent: true,
			// WindowIsTranslucent adds WS_EX_NOREDIRECTIONBITMAP, which is what
			// gives the window a real per-pixel alpha surface. Without it the
			// WebView2 composition is composited onto an opaque (white) backing,
			// so every transparent pixel of the page shows as white - a white
			// rectangle exactly the size of the pet.
			//
			// It cannot be set from our side: WS_EX_NOREDIRECTIONBITMAP is a
			// creation-time style, and attempting to add it later is silently
			// ignored (verified: the style reads back as unset). So enabling this
			// option is the only way to get the alpha surface.
			//
			// The cost is that on Windows 10 Wails also calls
			// SetTranslucentBackground() - ACCENT_ENABLE_BLURBEHIND - because the
			// Win11 backdrop APIs are unavailable there. That blur is removed
			// again by desktop.DisableWindowBlur (ACCENT_DISABLED).
			WindowIsTranslucent: true,
			// No icon, so only the pet is visible.
			DisableWindowIcon: true,
			// Keep Wails from re-extending the DWM frame by 1px on every
			// WM_ACTIVATE, which would undo the glass effect and re-introduce
			// the opaque background each time the pet is clicked.
			DisableFramelessWindowDecorations: true,
		},
	})
	if err != nil {
		log.Fatalf("pet: %v", err)
	}
}
