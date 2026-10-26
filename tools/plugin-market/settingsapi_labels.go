package main

import (
	"PetPC/internal/plugins"
)

// PermissionLabels returns the host's own wording for every permission.
//
// It is a READ of internal/plugins.PermissionLabels rather than a copy in the frontend,
// and that is the point: the install dialog is the user's consent, and consent in two
// different wordings is consent to two different things. A duplicated map would also
// drift silently the moment a permission is added - the Go side would gain a row and the
// screen would keep showing a bare `timer.control`.
func (m *Market) PermissionLabels() map[string]string {
	out := make(map[string]string, len(plugins.PermissionLabels))
	for k, v := range plugins.PermissionLabels {
		out[k] = v
	}
	return out
}
