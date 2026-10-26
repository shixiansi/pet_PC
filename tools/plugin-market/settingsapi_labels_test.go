package main

import (
	"testing"

	"PetPC/internal/plugins"
)

// This file covers the SHAPE of PermissionLabels, not its wording.
//
// The wording lives in internal/plugins and TestPermissionLabelsCoverTheVocabulary keeps
// it complete. What is asserted here is the part that crosses the Wails bridge, because
// that is where this binding caused a defect: it used to return a map[string]string, which
// arrives on the JS side as an anonymous object no generated models.ts can describe - so a
// page that received something wrong had no way to say what it received, and the report
// was "空对象" plus a status code nobody could trace.
//
// A slice of {ID, Label} is describable: wailsjs/go/models.ts gains main.LabelPair, the
// page imports it, and a mismatch becomes a type error in `npm run check` instead of a
// blank row on a screenshot. These two assertions are what keeps it that way - they fail
// the day someone reaches for a map again, or reorders the table the UI is built from.
func TestPermissionLabelsIsASliceInTheConsentOrder(t *testing.T) {
	got := (&Market{}).PermissionLabels()

	if len(got) != len(plugins.AllowedPermissions) {
		t.Fatalf("PermissionLabels() returned %d pairs, AllowedPermissions has %d",
			len(got), len(plugins.AllowedPermissions))
	}
	for i, want := range plugins.AllowedPermissions {
		// The order is the install dialog's order (see AllowedPermissions). Two views of
		// one consent should not disagree about which permission comes first.
		if got[i].ID != want {
			t.Fatalf("pair %d is %q, want %q (AllowedPermissions order)", i, got[i].ID, want)
		}
		if got[i].Label != plugins.PermissionLabels[want] {
			t.Errorf("label for %q is %q, want %q", want, got[i].Label, plugins.PermissionLabels[want])
		}
		if got[i].Label == "" {
			t.Errorf("%q has an empty label: the install dialog would show a bare permission id", want)
		}
	}
}
