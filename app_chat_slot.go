package main

import (
	"fmt"
	"strings"

	"PetPC/internal/plugins"
)

// This file owns the pet's CHAT SLOT: who occupies it, and what the conversation
// button on the pet opens.
//
// The slot is a DECLARATION, not a reserved id. A plugin says `panelSlot: "chat"`
// in its manifest and the host asks it for the conversation surface; nothing in
// this file knows the name of any particular plugin - including the one shipped
// in the market. That is deliberate: a built-in id would be a back door in an
// architecture whose whole promise is that the pet is generic and the plugin is
// the thing that knows about LLMs.
//
// (The name `panelSlot` is older than this behaviour: it named the panel tab the
// claimant used to fill, and the panel retired in stage 3. The field still says
// "which slot do I answer for", which is what the menu and this resolver need.)
//
// The floating surface is NOT re-derived here. Opening the conversation surface
// goes through the existing OpenPluginSurface, so there is exactly one place
// that computes plugin surface geometry and exactly one guard that refuses to
// cover an unanswered question. A second geometry path would drift from the
// first, and the drift would show up as the window being cropped to a rectangle
// nothing is painted in.

// chatSlotPlugin resolves the plugin that answers for the 对话 slot.
//
// The rule is "the first installed plugin in plugins.json order that is enabled,
// has been granted pet.surface, and declares panelSlot == chat". Each clause is
// load-bearing:
//
//   - enabled: a plugin the user switched off must not keep the slot, or
//     "disable" would not disable anything the user can see;
//   - granted pet.surface: the slot renders a sandboxed iframe that draws
//     outside the pet's own box, which is exactly what that permission covers,
//     so the slot would otherwise be a way around the permission prompt;
//   - panelSlot == chat: the slot is claimed by declaration.
//
// "First in plugins.json order" rather than "newest" or "the one the user last
// touched" is what makes the answer deterministic: two plugins may claim the
// same slot, and the pet has to pick the same one on every launch and in every
// process. The order the manager reports IS that file's order.
//
// It reads through PluginsInstalled - the same binding the frontend uses to
// render the slot - rather than the manager directly, so the page and the host
// can never disagree about who owns the tab. The full row (with the manifest the
// surface geometry needs) is fetched only for the plugin that won.
func (a *App) chatSlotPlugin() (plugins.Installed, bool) {
	rows, err := a.PluginsInstalled()
	if err != nil {
		return plugins.Installed{}, false
	}
	for _, row := range rows {
		if !row.Enabled || row.PanelSlot != plugins.PanelSlotChat {
			continue
		}
		if !hasPermission(row.Granted, plugins.PermPetSurface) {
			continue
		}
		if installed, ok := a.pluginManager().Get(row.ID); ok {
			return installed, true
		}
		// The row was in the list but could not be read back: that is a race
		// with the market program, not a slot owner. Keep looking rather than
		// handing back a half-built row.
	}
	return plugins.Installed{}, false
}

// chatSlotMissingMessage is what the slot shows when no plugin claims it.
//
// It is a sentence rather than a blank bubble on purpose: "nothing happened" is
// indistinguishable from "this is broken", and the user's next move (open the control
// center, install the chat plugin, tick its permissions) is not guessable from an
// empty card. It names BOTH ways in - the pet's own menu and the tray - because a
// gear in a panel header used to be the obvious one, and that panel retired in stage 3.
const chatSlotMissingMessage = "对话插件还没装好：打开控制中心（右键桌宠 → 插件，或托盘菜单）装上并授权它。"

// chatSlotDiagnosis explains why an INSTALLED plugin was refused the slot, and
// returns "" when nothing is installed at all.
//
// The generic sentence above is correct when the list is empty and actively
// misleading when it is not: a user who installed the conversation plugin sees it
// listed in the market, reads "还没装好", and has no way to discover that the copy
// on disk is an older PACKAGE. That is a real and reachable state rather than a
// hypothetical - `panelSlot` was introduced with version 1.1.0, so an install of
// 1.0.0 resolves to no owner, and the obvious next move (install it again) would
// reinstall the same old package whenever the catalogue still offers it.
//
// Every clause of the resolution rule gets its own sentence because each has a
// different fix. The messages are built from the same rows and in the same order
// the resolver walks, so the sentence describes the plugin that would otherwise
// have won.
func (a *App) chatSlotDiagnosis() string {
	rows, err := a.PluginsInstalled()
	if err != nil {
		return ""
	}
	enabled := make([]PluginView, 0, len(rows))
	for _, row := range rows {
		if row.Enabled {
			enabled = append(enabled, row)
		}
	}
	if len(enabled) == 0 {
		return ""
	}

	claimants := make([]PluginView, 0, len(enabled))
	for _, row := range enabled {
		if row.PanelSlot == plugins.PanelSlotChat {
			claimants = append(claimants, row)
		}
	}
	if len(claimants) == 0 {
		names := make([]string, 0, len(enabled))
		for _, row := range enabled {
			names = append(names, fmt.Sprintf("%s（v%s）", viewDisplayName(row), row.Version))
		}
		return "对话插件是旧版本：" + strings.Join(names, "、") +
			" 没有声明「对话」槽位，在插件市场里更新它（或卸载后重新安装新版本）。"
	}

	ungranted := make([]string, 0, len(claimants))
	for _, row := range claimants {
		if !hasPermission(row.Granted, plugins.PermPetSurface) {
			ungranted = append(ungranted, viewDisplayName(row))
		}
	}
	if len(ungranted) > 0 {
		return "对话插件缺少 pet.surface 授权：在插件市场里勾上 " +
			strings.Join(ungranted, "、") + " 的这项权限。"
	}

	for _, row := range claimants {
		if row.Error != "" {
			return "对话插件无法读取：" + viewDisplayName(row) + " — " + row.Error
		}
	}
	return ""
}

// viewDisplayName is the plugin's own name, falling back to its id when the
// manifest could not be read.
//
// The fallback matters for exactly the case these messages exist for: a row whose
// manifest failed to parse has no Name, and a sentence that said "（v）" instead of
// naming the id would be useless for finding the plugin in the market.
func viewDisplayName(row PluginView) string {
	if strings.TrimSpace(row.Name) != "" {
		return row.Name
	}
	return row.ID
}

// OpenChatPluginSurface opens the conversation surface that the button over the
// pet leads to.
//
// With a usable chat plugin it delegates to OpenPluginSurface, which brings its
// own refusals (not enabled, no grant, no declared rectangles, a question
// waiting for an answer) and its own geometry. Without one there is nothing to
// open, so the reason travels as a bubble - the pet has to say something, since
// a gesture that silently does nothing reads as a frozen app.
//
// The bubble is shown through showBubble, so it takes the slot from the panel
// and the live surfaces exactly as any other bubble does.
//
// A crop failure is deliberately NOT reported as an error here. showBubble has
// already recorded the bubble in the state and in the layout, so the failure is
// "the window could not be re-cropped", not "the bubble did not appear" -
// returning the error would tell the frontend the opposite of what its next
// GetLayout says. OpenPluginSurface takes the same position for the same reason,
// and the next geometry pass crops to the bubble that is already there.
func (a *App) OpenChatPluginSurface() (Layout, error) {
	row, ok := a.chatSlotPlugin()
	if !ok {
		// The bubble carries the SPECIFIC reason when a plugin is installed but
		// was refused - an old package, a missing grant, a broken manifest - and
		// the generic sentence only when the list is genuinely empty. A
		// gesture that says "还没装好" while the market shows the plugin as
		// installed sends the user to reinstall the wrong thing.
		message := chatSlotMissingMessage
		if diagnosis := a.chatSlotDiagnosis(); diagnosis != "" {
			message = diagnosis
		}
		if _, err := a.showBubble(&BubbleView{
			Kind:  BubbleReminder,
			Title: message,
		}); err != nil {
			a.log.Warnf("plugin", "对话浮层面：说明气泡之后重新裁剪窗口失败：%v", err)
			return a.currentLayout(), nil
		}
		return a.currentLayout(), nil
	}
	layout, err := a.OpenPluginSurface(row.ID)
	if err != nil {
		return layout, fmt.Errorf("%s：%w", row.ID, err)
	}
	return layout, nil
}
