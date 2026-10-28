package main

// Read-only views of the pet's data.
//
// DataDir is the survivor, and it is not "a view of the pet's state": it is where this
// program's Settings page tells the user their files are.
//
// `Todos()` used to live here, reading `todos.json` so the control center's 待办 page could
// list it. Both are gone, and the reason is worth keeping: the list stopped being the
// PET's data. It moved into the todo plugin's own `storage` (one migration, one writer -
// the plugin), so a page reading `todos.json` was showing a file nothing writes any more.
// The list belongs to the plugin, so it is drawn by the plugin's own document - the same
// one the pet mounts beside itself - inside the plugin page's workspace.

// DataDir is where everything lives, so the advanced page can show it rather than
// describe it - "your data is in %APPDATA%\PetPC" is the one sentence users need and
// never find.
func (m *Market) DataDir() string { return m.dataDir }
