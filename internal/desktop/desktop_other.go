//go:build !windows

package desktop

import (
	"errors"
	"fmt"
	"time"
)

var errUnsupported = errors.New("native window control is only implemented on Windows")

// FindWindow is not implemented off Windows.
func FindWindow() (Window, error) { return 0, errUnsupported }

// WaitForWindow is not implemented off Windows.
func WaitForWindow(time.Duration) (Window, error) { return 0, errUnsupported }

// IsWindow always reports false off Windows.
func IsWindow(Window) bool { return false }

// WindowBounds is not implemented off Windows.
func WindowBounds(Window) (Bounds, error) { return Bounds{}, errUnsupported }

// SetBounds is not implemented off Windows.
func SetBounds(Window, Bounds) error { return errUnsupported }

// MoveTo is not implemented off Windows.
func MoveTo(Window, int, int) error { return errUnsupported }

// Scale returns 1 off Windows.
func Scale(Window) float64 { return 1 }

// WorkArea is not implemented off Windows.
func WorkArea(Window) (Bounds, error) { return Bounds{}, errUnsupported }

// MakeTransparent is not implemented off Windows.
func MakeTransparent(Window) error { return errUnsupported }

// DisableWindowBlur is not implemented off Windows.
func DisableWindowBlur(Window) error { return errUnsupported }

// StripWindowFrame is not implemented off Windows.
func StripWindowFrame(Window) error { return errUnsupported }

// HideFromTaskbar is not implemented off Windows.
func HideFromTaskbar(Window) error { return errUnsupported }

// ShellOpen is not implemented off Windows.
func ShellOpen(string) error { return errUnsupported }

// TrayOptions configures a system-tray icon. Only Windows implements one.
type TrayOptions struct {
	Tooltip    string
	IconPath   string
	Items      []TrayMenuItem
	OnCommand  func(id uintptr)
	OnActivate func()
}

// TrayMenuItem is one entry in the tray menu.
type TrayMenuItem struct {
	ID        uintptr
	Label     string
	Separator bool
}

// TrayMenuAction builds a clickable menu entry.
func TrayMenuAction(id uintptr, label string) TrayMenuItem {
	return TrayMenuItem{ID: id, Label: label}
}

// TrayMenuSeparator builds a divider.
func TrayMenuSeparator() TrayMenuItem { return TrayMenuItem{Separator: true} }

// Tray command IDs, mirroring the Windows values.
const (
	TrayCmdTogglePanel    = 1001
	TrayCmdResetPos       = 1002
	TrayCmdOpenData       = 1003
	TrayCmdQuit           = 1004
	TrayCmdRemindNow      = 1005
	TrayCmdTogglePomodoro = 1006
	TrayCmdShowPet        = 1007
	TrayCmdHidePet        = 1008
)

// Tray is a no-op off Windows.
type Tray struct{}

// NewTray reports that the tray is unavailable off Windows.
func NewTray(TrayOptions) (*Tray, error) { return nil, errUnsupported }

// Close does nothing off Windows.
func (*Tray) Close() {}

// IsWindowVisible always reports false off Windows.
func IsWindowVisible(Window) bool { return false }

// Show is not implemented off Windows.
func Show(Window) error { return errUnsupported }

// ShowNoActivate is not implemented off Windows.
func ShowNoActivate(Window) error { return errUnsupported }

// Hide is not implemented off Windows.
func Hide(Window) error { return errUnsupported }

// NewHotkeys reports that global hot keys are unavailable off Windows.
func NewHotkeys([]HotkeySpec, func(HotkeyAction)) (HotkeyManager, HotkeyView, error) {
	return nil, unavailableHotkeyView(), errUnsupported
}

// SetWindowRegion is not implemented off Windows.
func SetWindowRegion(Window, ...RegionRect) error { return errUnsupported }

// SuppressBackgroundErasure is not implemented off Windows.
func SuppressBackgroundErasure(Window) error { return errUnsupported }

// SetBackgroundBrush is not implemented off Windows.
func SetBackgroundBrush(Window, uint8, uint8, uint8, bool) error { return errUnsupported }

// AutostartEnabled always reports false off Windows.
func AutostartEnabled() bool { return false }

// RepairAutostart has nothing to repair off Windows, where there is no Run key.
func RepairAutostart() bool { return false }

// SetAutostart only supports disabling off Windows, which is a no-op.
func SetAutostart(enable bool) error {
	if enable {
		return fmt.Errorf("auto-start: %w", errUnsupported)
	}
	return nil
}
