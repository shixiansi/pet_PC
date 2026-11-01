package main

import (
	"net/http"

	"PetPC/internal/plugins"
)

// The plugin asset route is IMPLEMENTED IN internal/plugins and delegated to here.
//
// It moved because the plugin market hosts plugin interfaces too (the voice
// plugin's workshop), so it serves the same files under the same prefix and needs
// the same path-traversal defence and the same bridge protocol. Two copies of that
// would be two places for the defence to drift, and the defence is the only thing
// between a request-supplied string and the filesystem.
//
// The names stay declared here because the pet's own tests and its other code
// refer to them, and because "which URL space does THIS program serve" is a fact
// about this program. They are aliases, not a second definition: a change to the
// prefix is a change to plugins.AssetPrefix.

// pluginAssetPrefix is the URL space plugin files are served from.
const pluginAssetPrefix = plugins.AssetPrefix

// pluginHostBridgePath is the bridge script the host itself serves.
const pluginHostBridgePath = plugins.BridgePath

// pluginBridgeJS is the bridge a plugin's script talks to.
const pluginBridgeJS = plugins.BridgeJS

// servePluginAsset serves plugin front-end files and the host bridge script.
//
// The plugin must be installed AND enabled; a disabled plugin's files stop being
// served at the same moment its interfaces close, so "off" is one state rather
// than two that can disagree.
//
// The whole request is handed to the shared handler, including the "is this even
// a plugin asset path" test, so this function cannot disagree with the market's
// copy about what the prefix means.
func (a *App) servePluginAsset(w http.ResponseWriter, r *http.Request) {
	// The pack set rides along: an HTML document gets the chosen pack spliced in, because the frame
	// is an opaque origin and cannot be reached any other way (internal/plugins/asset.go).
	plugins.ServePluginAsset(a.pluginManager(), a.packs, w, r)
}
