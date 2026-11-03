package main

import (
	"fmt"

	"PetPC/internal/desktop"
	"PetPC/internal/petmenu"
	"PetPC/internal/store"
)

// The control center's window onto the pet's preferences.
//
// The pet and this program share a data directory, and since the settings split
// (openspec/changes/ui-rebuild, phase 1) `settings.json` belongs to THIS program while
// `config.json` belongs to the pet. So these methods are the only correct way to change
// a preference: they go through store.SaveSettings, which re-reads the file before
// writing so a change is never lost to a stale copy.
//
// The pet picks the change up through Store.Config()'s mtime check - no IPC, no restart.

// MainSettings returns the pet's current configuration.
//
// Returns the whole Config rather than a hand-picked view: the pages need different
// fields, and a per-page projection would be a second definition of "the settings" that
// drifts from the struct the pet actually reads.
func (m *Market) MainSettings() (store.Config, error) {
	s, err := store.New(m.dataDir)
	if err != nil {
		return store.Config{}, fmt.Errorf("读取设置失败：%w", err)
	}
	cfg := s.Config()
	// `autostart` is answered by the REGISTRY, not by config.json. config.json's copy is
	// the pet's mirror of that entry, so it is stale whenever the pet is not running -
	// and stale here means the switch would show a state Windows does not have. The
	// predicate is desktop.AutostartEnabled, the same one the pet reads: it also rejects
	// an entry that names the control center, so a ticked box means "the pet will start".
	cfg.Autostart = desktop.AutostartEnabled()
	// 右键菜单的 denylist 必须**读**的时候也归一化一次，和桌宠的 GetState 同一条规则。
	//
	// 为什么不能只在写的时候过：settings.json 是用户能手改的纯文件，也可能是旧版本写下的
	// （那时「控制中心」还不是 fixed）。不归一化的话，这一页会把 "plugins" / "quit" 当成
	// "被关掉了"，于是那两行显示成**未勾选、又因为 fixed 点不动**——用户看到的就是两个
	// 灰掉的空框，而桌宠那一侧因为归一化过、照样画着它们。同一份文件两个结论。
	cfg.MenuHidden = petmenu.Normalise(cfg.MenuHidden)
	return cfg, nil
}

// SetPetScale changes how big the pet is drawn.
//
// One method per preference rather than a generic "patch the config" call: a generic
// writer would let a page change a field it has no UI for, which is the difference
// between a settings screen and a config editor.
func (m *Market) SetPetScale(scale float64) (store.Config, error) {
	return store.SaveSettings(m.dataDir, func(c *store.Config) { c.PetScale = scale })
}
