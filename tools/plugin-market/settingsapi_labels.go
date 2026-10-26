package main

import (
	"PetPC/internal/plugins"
)

// LabelPair is one permission and the host's wording for it.
//
// WHY A SLICE OF PAIRS AND NOT A map[string]string: the frontend has to be able to
// NAME what it received. A map crosses the bridge as an anonymous object whose keys
// are data - nothing in the generated wailsjs/models.ts can describe it, so a page that
// gets the wrong thing sees `{}` and cannot say whether the call failed, returned
// nothing, or returned something else entirely. A struct is a shape: the generator
// writes it down, the page can import it, and a mismatch becomes a type error instead
// of an empty screen.
type LabelPair struct {
	ID    string
	Label string
}

// PermissionLabels returns the host's own wording for every permission.
//
// It is a READ of internal/plugins.PermissionLabels rather than a copy in the frontend,
// and that is the point: the install dialog is the user's consent, and consent in two
// different wordings is consent to two different things. A duplicated map would also
// drift silently the moment a permission is added - the Go side would gain a row and the
// screen would keep showing a bare `timer.control`.
//
// The ORDER is AllowedPermissions' order, for the same reason that slice exists: it is
// the order the install dialog asks in, and two views of one consent should not disagree
// about it. The two tables are kept equal by TestPermissionLabelsCoverTheVocabulary, so
// walking one and reading the other cannot lose a permission.
func (m *Market) PermissionLabels() []LabelPair {
	out := make([]LabelPair, 0, len(plugins.AllowedPermissions))
	for _, p := range plugins.AllowedPermissions {
		out = append(out, LabelPair{ID: p, Label: plugins.PermissionLabels[p]})
	}
	return out
}
