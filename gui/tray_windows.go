//go:build windows

package gui

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/golang-gui/goui/platform/windows/sdk/shell"
	"github.com/golang-gui/goui/platform/windows/sdk/winapi"
	"github.com/golang-gui/goui/platform/windows/win32"
)

// TrayIcon represents a system tray icon.
type TrayIcon struct {
	mu      sync.Mutex
	hwnd    winapi.HWND
	icon    winapi.HICON
	uid     uint32
	tip     string
	visible bool
	onClick func()
}

// trayIcons routes TrayMessage callbacks to the icon registered for their
// window handle. One icon per window: with the default callback version Shell
// identifies the delivery only by HWND, so a second icon on the same window
// could never tell its own clicks from the first one's.
var trayIcons sync.Map // winapi.HWND -> *TrayIcon

// nextTrayUID numbers icons as they are created. Windows identifies an icon by
// (HWND, uID), and reusing an id would make a second NIM_ADD rewrite the first
// icon in place instead of adding one.
var nextTrayUID atomic.Uint32

var routeOnce sync.Once

func installTrayRouting() {
	win32.SetMessageHandler(func(hwnd winapi.HWND, message winapi.UINT, _ winapi.WPARAM, lParam winapi.LPARAM) bool {
		if message == winapi.WM_DESTROY {
			pruneTrayIcon(hwnd)
			return false
		}
		v, ok := trayIcons.Load(hwnd)
		if !ok {
			return false
		}
		// The event id is in the low word of lParam for the classic callback.
		switch uint32(lParam & 0xFFFF) {
		case winapi.WM_LBUTTONUP, winapi.WM_LBUTTONDBLCLK:
			ti := v.(*TrayIcon)
			ti.mu.Lock()
			fn := ti.onClick
			ti.mu.Unlock()
			if fn != nil {
				safeOnClick(fn)
			}
		}
		return true
	})
}

// pruneTrayIcon removes the icon bound to a window that is going away. Shell
// would keep delivering clicks to a handle another window can be given, and
// the tray would show an icon no live object can remove.
func pruneTrayIcon(hwnd winapi.HWND) {
	v, ok := trayIcons.LoadAndDelete(hwnd)
	if !ok {
		return
	}
	ti := v.(*TrayIcon)
	ti.mu.Lock()
	defer ti.mu.Unlock()
	if err := ti.delete(); err != nil {
		log.Printf("gui: remove tray icon for destroyed window %v: %v", uintptr(hwnd), err)
	}
	ti.visible = false
}

// safeOnClick isolates the process from whatever the application callback does.
// This runs inside a syscall.NewCallback, where a panic is fatal at the process
// level and nothing further up the stack can recover it. A misbehaving UI
// callback should cost one click, not every zone's connected players.
func safeOnClick(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("gui: tray onClick panic: %v", r)
		}
	}()
	fn()
}

// OnClick sets fn to run on the GUI thread when the icon is clicked
// (left button up or double click). nil clears it.
func (ti *TrayIcon) OnClick(fn func()) {
	ti.mu.Lock()
	ti.onClick = fn
	ti.mu.Unlock()
}

// NewTrayIcon creates a new tray icon associated with the given window handle.
// The icon will send WM_TRAYMESSAGE to hwnd when clicked. A window hosts one
// tray icon; a second icon for the same handle replaces the first.
func NewTrayIcon(hwnd uintptr, icon uintptr, tip string) (*TrayIcon, error) {
	if hwnd == 0 || icon == 0 {
		return nil, fmt.Errorf("hwnd and icon must be non-zero")
	}

	routeOnce.Do(installTrayRouting)

	ti := &TrayIcon{
		hwnd: winapi.HWND(hwnd),
		icon: winapi.HICON(icon),
		uid:  nextTrayUID.Add(1),
		tip:  tip,
	}
	// Windows identifies an icon by (HWND, uID), so the replaced icon keeps its
	// own id in the shell and has to be removed there explicitly. Leaving it
	// would show two icons of the same window.
	if old, loaded := trayIcons.Swap(ti.hwnd, ti); loaded {
		prev := old.(*TrayIcon)
		prev.mu.Lock()
		err := prev.delete()
		prev.visible = false
		prev.mu.Unlock()
		if err != nil {
			log.Printf("gui: remove replaced tray icon for window %v: %v", uintptr(hwnd), err)
		}
	}

	ti.mu.Lock()
	defer ti.mu.Unlock()
	if err := ti.add(); err != nil {
		trayIcons.CompareAndDelete(ti.hwnd, ti)
		return nil, err
	}
	ti.visible = true
	return ti, nil
}

// add registers the tray icon with Windows. The caller holds ti.mu.
func (ti *TrayIcon) add() error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_MESSAGE | shell.NIF_ICON | shell.NIF_TIP
	data.UCallbackMessage = shell.WM_TRAYMESSAGE
	data.HIcon = ti.icon

	if err := copyUTF16Field(data.SzTip[:], ti.tip); err != nil {
		return err
	}
	return shell.ShellNotifyIcon(shell.NIM_ADD, &data)
}

// modify refreshes the icon image and tooltip. The caller holds ti.mu.
func (ti *TrayIcon) modify() error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_MESSAGE | shell.NIF_ICON | shell.NIF_TIP
	data.UCallbackMessage = shell.WM_TRAYMESSAGE
	data.HIcon = ti.icon

	if err := copyUTF16Field(data.SzTip[:], ti.tip); err != nil {
		return err
	}
	return shell.ShellNotifyIcon(shell.NIM_MODIFY, &data)
}

// delete forgets the icon. The caller holds ti.mu.
func (ti *TrayIcon) delete() error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	return shell.ShellNotifyIcon(shell.NIM_DELETE, &data)
}

// setVisible shows or hides the icon without removing it.
func (ti *TrayIcon) setVisible(visible bool) error {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	if ti.visible == visible {
		return nil
	}

	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_STATE
	data.DwStateMask = shell.NIS_HIDDEN
	if !visible {
		data.DwState = shell.NIS_HIDDEN
	}

	if err := shell.ShellNotifyIcon(shell.NIM_MODIFY, &data); err != nil {
		return err
	}
	ti.visible = visible
	return nil
}

// Show makes the tray icon visible.
func (ti *TrayIcon) Show() error {
	return ti.setVisible(true)
}

// Hide hides the tray icon without removing it.
func (ti *TrayIcon) Hide() error {
	return ti.setVisible(false)
}

// SetTip changes the tooltip text of the tray icon. Text longer than the native
// field is clipped by copyUTF16Field.
func (ti *TrayIcon) SetTip(tip string) error {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	previous := ti.tip
	ti.tip = tip
	if err := ti.modify(); err != nil {
		ti.tip = previous
		return err
	}
	return nil
}

// ShowBalloon displays a balloon notification from the tray icon.
func (ti *TrayIcon) ShowBalloon(title, message string, flags uint32) error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_INFO
	data.DwInfoFlags = flags

	if err := copyUTF16Field(data.SzInfoTitle[:], title); err != nil {
		return err
	}
	if err := copyUTF16Field(data.SzInfo[:], message); err != nil {
		return err
	}
	return shell.ShellNotifyIcon(shell.NIM_MODIFY, &data)
}

// Remove removes the tray icon from the system tray.
func (ti *TrayIcon) Remove() error {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	err := ti.delete()
	trayIcons.CompareAndDelete(ti.hwnd, ti)
	if err == nil {
		ti.visible = false
	}
	return err
}

// copyUTF16Field writes a NUL-terminated UTF-16 string into a fixed native
// array. UTF16FromString supplies the terminator, but a string longer than the
// field leaves no room for it and Shell would keep reading past the array
// looking for the end, so an over-long value is clipped and closed by hand.
func copyUTF16Field(dst []uint16, s string) error {
	utf16, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	if n := copy(dst, utf16); n == len(dst) {
		dst[len(dst)-1] = 0
	}
	return nil
}
