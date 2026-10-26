package main

import (
	"io/fs"
	"log"
	"os"
)

// uiAssets is the document tree this program serves as its own interface.
//
// Normally that is the embedded frontend. When PETPC_CC names a directory, the CONTROL
// CENTER's build is served from disk instead.
//
// WHY AN ENV VAR AND NOT A CODE EDIT: the control center is being built in
// `cc/` (openspec/changes/ui-rebuild) and its output is build output, gitignored. An
// `//go:embed all:cc/dist` would make `go build` fail on a fresh clone that has not run
// npm, and it would bake a half-finished interface into the binary. Reading the tree at
// runtime keeps ONE binary able to show either interface, so the switch over is a single
// reviewable step and falling back needs no rebuild.
//
// A path that is not a directory is a warning, not a failure: the reason someone set
// this variable is to look at an interface, and refusing to start would leave them with
// no window at all and no message either.
func uiAssets(embedded fs.FS) fs.FS {
	dir := os.Getenv("PETPC_CC")
	if dir == "" {
		return embedded
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		log.Printf("plugin-market: PETPC_CC=%q 不是一个目录，改为提供内置界面", dir)
		return embedded
	}
	log.Printf("plugin-market: 提供控制中心的界面 %s", dir)
	return os.DirFS(dir)
}
