//go:build windows

package gui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/golang-gui/goui/platform/windows/sdk/shell"
	"github.com/golang-gui/goui/platform/windows/sdk/winapi"
)

// TrayIcon represents a system tray icon.
type TrayIcon struct {
	hwnd    winapi.HWND
	icon    winapi.HICON
	uid     uint32
	tip     string
	visible bool
}

// NewTrayIcon creates a new tray icon associated with the given window handle.
// The icon will send WM_TRAYMESSAGE to hwnd when clicked.
func NewTrayIcon(hwnd uintptr, icon uintptr, tip string) (*TrayIcon, error) {
	if hwnd == 0 || icon == 0 {
		return nil, fmt.Errorf("hwnd and icon must be non-zero")
	}

	ti := &TrayIcon{
		hwnd: winapi.HWND(hwnd),
		icon: winapi.HICON(icon),
		uid:  1,
		tip:  tip,
	}

	if err := ti.add(); err != nil {
		return nil, err
	}

	ti.visible = true
	return ti, nil
}

// add registers the tray icon with Windows.
func (ti *TrayIcon) add() error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_MESSAGE | shell.NIF_ICON | shell.NIF_TIP
	data.UCallbackMessage = shell.WM_TRAYMESSAGE
	data.HIcon = ti.icon

	// Copy tip to SzTip
	tipUTF16, err := syscall.UTF16FromString(ti.tip)
	if err != nil {
		return err
	}
	copy(data.SzTip[:], tipUTF16)

	return shell.ShellNotifyIcon(shell.NIM_ADD, &data)
}

// modify updates an existing tray icon.
func (ti *TrayIcon) modify() error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_MESSAGE | shell.NIF_ICON | shell.NIF_TIP
	data.UCallbackMessage = shell.WM_TRAYMESSAGE
	data.HIcon = ti.icon

	tipUTF16, err := syscall.UTF16FromString(ti.tip)
	if err != nil {
		return err
	}
	copy(data.SzTip[:], tipUTF16)

	return shell.ShellNotifyIcon(shell.NIM_MODIFY, &data)
}

// Show makes the tray icon visible.
func (ti *TrayIcon) Show() error {
	if ti.visible {
		return nil
	}

	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_STATE
	data.DwState = 0
	data.DwStateMask = shell.NIS_HIDDEN

	err := shell.ShellNotifyIcon(shell.NIM_MODIFY, &data)
	if err == nil {
		ti.visible = true
	}
	return err
}

// Hide hides the tray icon without removing it.
func (ti *TrayIcon) Hide() error {
	if !ti.visible {
		return nil
	}

	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_STATE
	data.DwState = shell.NIS_HIDDEN
	data.DwStateMask = shell.NIS_HIDDEN

	err := shell.ShellNotifyIcon(shell.NIM_MODIFY, &data)
	if err == nil {
		ti.visible = false
	}
	return err
}

// SetTip changes the tooltip text of the tray icon.
func (ti *TrayIcon) SetTip(tip string) error {
	ti.tip = tip
	return ti.modify()
}

// ShowBalloon displays a balloon notification from the tray icon.
func (ti *TrayIcon) ShowBalloon(title, message string, flags uint32) error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid
	data.UFlags = shell.NIF_INFO
	data.DwInfoFlags = flags

	titleUTF16, err := syscall.UTF16FromString(title)
	if err != nil {
		return err
	}
	copy(data.SzInfoTitle[:], titleUTF16)

	msgUTF16, err := syscall.UTF16FromString(message)
	if err != nil {
		return err
	}
	copy(data.SzInfo[:], msgUTF16)

	return shell.ShellNotifyIcon(shell.NIM_MODIFY, &data)
}

// Remove removes the tray icon from the system tray.
func (ti *TrayIcon) Remove() error {
	var data shell.NOTIFYICONDATA
	data.CbSize = uint32(unsafe.Sizeof(data))
	data.HWnd = ti.hwnd
	data.UID = ti.uid

	err := shell.ShellNotifyIcon(shell.NIM_DELETE, &data)
	if err == nil {
		ti.visible = false
	}
	return err
}
