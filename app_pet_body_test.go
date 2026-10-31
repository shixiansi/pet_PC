package main

import (
	"testing"

	"PetPC/internal/plugins"
)

// TestPluginViewCarriesTheBodyDeclaration pins the one wire the pet's page needs to draw a
// plugin's pet: the installed row has to SAY that the plugin asked for the body.
//
// It is a two-line test on purpose, and it exists because this is the failure mode the whole
// feature would die of silently: Go validates the declaration, the manager stores it, the
// frontend gates on `p.body === true` - and if the view does not carry the field, that gate is
// false for every plugin in the world. Nothing would error; the pet would simply keep drawing
// the bundled picture while the user's new plugin says it is installed and granted. The page's
// own unit test (frontend/src/pet-body.test.js) uses a hand-written row, so it cannot catch a
// field that never left Go.
func TestPluginViewCarriesTheBodyDeclaration(t *testing.T) {
	body := plugins.Installed{
		ID:      "com.example.pet-body",
		Version: "1.0.0",
		Enabled: true,
		Granted: []string{plugins.PermPetSurface},
		Dir:     t.TempDir(),
		Manifest: &plugins.Manifest{
			ID:          "com.example.pet-body",
			Name:        "本体",
			Author:      "someone",
			Version:     "1.0.0",
			Description: "画桌宠本体",
			Permissions: []string{plugins.PermPetSurface},
			Body:        true,
			Entry:       "pet.html",
		},
	}
	if v := pluginViewOf(body); !v.Body {
		t.Errorf("the view dropped the body declaration: %+v", v)
	}

	// And the other direction: a plugin that draws a card beside the pet must not be reported
	// as owning the pet's face, or the page would mount it over the picture.
	plain := body
	plain.Manifest = &plugins.Manifest{
		ID:          "com.example.sidebar",
		Name:        "浮层面",
		Author:      "someone",
		Version:     "1.0.0",
		Description: "宠物旁边的卡片",
		Permissions: []string{plugins.PermPetSurface},
		Surface:     []plugins.Rect{{DX: 160, DY: -160, W: 320, H: 220}},
		Entry:       "panel.html",
	}
	if v := pluginViewOf(plain); v.Body {
		t.Errorf("a surface-only plugin was reported as drawing the pet: %+v", v)
	}
}
