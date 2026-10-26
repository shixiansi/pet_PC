package plugins

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AssetPrefix is the URL space plugin files are served from.
//
// It is shared rather than declared in each program because the plugin's own code
// refers to it: the bridge is loaded as `<AssetPrefix>@host/bridge.js`, and a
// program that served the same files under a different prefix would need a bridge
// script that knew about the difference.
const AssetPrefix = "/plugin-asset/"

// BridgePath is the bridge script the host itself serves.
//
// The "@" is not a legal character in a plugin id, so no plugin can ever be called
// "@host" and shadow the bridge with a file of its own.
const BridgePath = AssetPrefix + "@host/bridge.js"

// ServePluginAsset serves one plugin front-end file, or the bridge script.
//
// It is the ONE implementation of "give the plugin its own files", used by every
// program that hosts a plugin interface. Two copies of this would be two places
// where the path-traversal defence could drift, and that defence is the only thing
// standing between a request-supplied string and the filesystem.
//
// It returns false when the request is not for a plugin asset at all, so a caller
// can fall through to its own routing rather than having to test the prefix
// itself.
func ServePluginAsset(gate Gate, w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, AssetPrefix) {
		return false
	}
	if r.URL.Path == BridgePath {
		serveBridgeJS(w)
		return true
	}
	rel := strings.TrimPrefix(r.URL.Path, AssetPrefix)
	id, sub, ok := splitAssetPath(rel)
	if !ok {
		http.Error(w, "bad plugin asset path", http.StatusBadRequest)
		return true
	}
	// The plugin must be installed AND enabled. A disabled plugin's files stop
	// being served at the same moment its interfaces close, so "off" is one state
	// rather than two that can disagree.
	row, found := gate.Get(id)
	if !found || !row.Enabled || row.Dir == "" {
		http.Error(w, "plugin not available", http.StatusForbidden)
		return true
	}

	full := filepath.Join(row.Dir, filepath.FromSlash(sub))
	// Belt and braces: the segments were already checked, and the join is verified
	// again, because this is the one place a request-supplied string reaches the
	// filesystem.
	absDir, err := filepath.Abs(row.Dir)
	if err != nil {
		http.Error(w, "bad plugin dir", http.StatusInternalServerError)
		return true
	}
	absFull, err := filepath.Abs(full)
	if err != nil || !strings.HasPrefix(absFull, absDir+string(filepath.Separator)) {
		http.Error(w, "bad plugin asset path", http.StatusForbidden)
		return true
	}

	f, err := os.Open(absFull)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return true
		}
		http.Error(w, "open failed", http.StatusInternalServerError)
		return true
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return true
	}

	if ct := mime.TypeByExtension(filepath.Ext(absFull)); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	// The opaque-origin sandbox needs this on subresources it fetches: without it
	// the request is a cross-origin one from the iframe's point of view.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Plugin files change when the plugin is reinstalled, and the plugin directory
	// name does not carry a version, so nothing may be cached.
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filepath.Base(absFull), fi.ModTime(), f)
	return true
}

// splitAssetPath splits "<id>/<relpath>" and validates both halves.
func splitAssetPath(rel string) (id, sub string, ok bool) {
	slash := strings.Index(rel, "/")
	if slash <= 0 || slash == len(rel)-1 {
		return "", "", false
	}
	id, sub = rel[:slash], rel[slash+1:]
	if !IsValidID(id) {
		return "", "", false
	}
	if !ValidRelPath(sub) {
		return "", "", false
	}
	return id, sub, true
}

// serveBridgeJS serves the host's own bridge script.
//
// The script is a COMPILED-IN CONSTANT rather than a file, so it can never be
// missing, edited or replaced: it is the host's half of the plugin protocol, and a
// plugin that could change it could change what the host says.
func serveBridgeJS(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	// No caching: this is the protocol itself, and a stale copy would speak a
	// version the host no longer implements.
	w.Header().Set("Cache-Control", "no-store")
	// The iframe is a sandboxed opaque origin, so even a same-URL subresource
	// fetch is cross-origin from its point of view.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Last-Modified", time.Time{}.UTC().Format(http.TimeFormat))
	_, _ = w.Write([]byte(BridgeJS))
}

// BridgeJS is the bridge a plugin's script talks to.
//
// It runs INSIDE the sandboxed iframe, so it has no access to the host page: it
// can only post messages to its parent, and the host decides what to do with them.
// Its whole job is to turn that exchange into something a plugin author can call
// without knowing the message shapes.
//
// The protocol (see design §9.2): the script announces itself with `ready` and
// waits for the host's `init`, which carries the permissions the user granted - a
// plugin must not guess its own capabilities. Requests carry an id so the matching
// response can be found, and events are delivered by name.
//
// It lives in this package rather than in either program because both programs
// serve it, and a plugin that received a different bridge depending on which one
// mounted it would be a plugin whose protocol depends on its host.
const BridgeJS = `// PetPC plugin bridge. Served by the host; the same protocol in both directions.
(function () {
  'use strict';
  var VERSION = 1;
  var pending = new Map();
  var nextID = 1;
  var listeners = [];
  var info = { pluginId: '', permissions: [], granted: [], surface: null };
  var readySent = false;

  function post(msg) {
    msg.v = VERSION;
    parent.postMessage(msg, '*');
  }

  window.addEventListener('message', function (ev) {
    // Accept only the host: a plugin's iframe has no other legitimate source,
    // and anything else in the window hierarchy is not the host.
    if (ev.source !== parent) return;
    var data = ev.data;
    if (!data || data.v !== VERSION) return;

    if (data.type === 'init') {
      info = {
        pluginId: data.pluginId,
        permissions: data.permissions || [],
        granted: data.granted || [],
        surface: data.surface || null,
      };
      listeners.slice().forEach(function (fn) { fn(info); });
      window.dispatchEvent(new CustomEvent('pet:init', { detail: info }));
      return;
    }

    if (data.type === 'response') {
      var entry = pending.get(data.id);
      if (!entry) return;
      pending.delete(data.id);
      if (data.ok) entry.resolve(data.result);
      else entry.reject(new Error(data.error || 'plugin request failed'));
      return;
    }

    if (data.type === 'event') {
      window.dispatchEvent(new CustomEvent('pet:event', {
        detail: { name: data.name, payload: data.payload },
      }));
    }
  });

  function ready() {
    if (readySent) return;
    readySent = true;
    // The handshake's first half. The host replies with exactly one init, and
    // until it arrives the plugin must not send a request: it does not know
    // what it has been granted.
    post({ type: 'ready' });
  }

  function request(method, payload) {
    return new Promise(function (resolve, reject) {
      var id = String(nextID++);
      pending.set(id, { resolve: resolve, reject: reject });
      post({ type: 'request', id: id, method: method, payload: payload || {} });
    });
  }

  window.pet = {
    version: VERSION,
    ready: ready,
    request: request,
    // info() is a snapshot; onInit/onEvent are the subscription forms.
    info: function () { return info; },
    onInit: function (fn) { listeners.push(fn); if (info.pluginId) fn(info); },
    granted: function (perm) { return info.granted.indexOf(perm) !== -1; },
    onEvent: function (fn) {
      window.addEventListener('pet:event', function (ev) { fn(ev.detail); });
    },
  };

  // The script announces itself as soon as it is loaded. The plugin's own code
  // waits for pet.onInit before touching its UI.
  if (document.readyState === 'complete' || document.readyState === 'interactive') {
    ready();
  } else {
    document.addEventListener('DOMContentLoaded', ready);
  }
})();
`
