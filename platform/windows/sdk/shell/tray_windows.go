package shell

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/golang-gui/goui/platform/windows/sdk/winapi"
)

var (
	procShellNotifyIconW = shell32.NewSymbol("Shell_NotifyIconW")
)

// Tray icon notification constants.
const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	NIF_STATE   = 0x00000008
	NIF_INFO    = 0x00000010

	NIS_HIDDEN     = 0x00000001
	NIS_SHAREDICON = 0x00000002

	// Notification balloon types.
	NIIF_NONE       = 0x00000000
	NIIF_INFO       = 0x00000001
	NIIF_WARNING    = 0x00000002
	NIIF_ERROR      = 0x00000003
	NIIF_USER       = 0x00000004
	NIIF_NOSOUND    = 0x00000010
	NIIF_LARGE_ICON = 0x00000020
)

// WM_TRAYMESSAGE is a custom window message for tray notifications.
// Applications should define their own message ID using RegisterWindowMessage
// or use WM_USER + offset.
const WM_TRAYMESSAGE = winapi.WM_USER + 1

// NOTIFYICONDATA represents the Windows NOTIFYICONDATA structure.
type NOTIFYICONDATA struct {
	CbSize           winapi.DWORD
	HWnd             winapi.HWND
	UID              winapi.UINT
	UFlags           winapi.UINT
	UCallbackMessage winapi.UINT
	HIcon            winapi.HICON
	SzTip            [128]uint16
	DwState          winapi.DWORD
	DwStateMask      winapi.DWORD
	SzInfo           [256]uint16
	UVersion         winapi.UINT
	SzInfoTitle      [64]uint16
	DwInfoFlags      winapi.DWORD
	GuidItem         GUID
	BalloonTimeout   winapi.DWORD
}

// GUID for future extensibility (currently unused).
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// ShellNotifyIcon sends a message to the system tray area.
func ShellNotifyIcon(message winapi.UINT, data *NOTIFYICONDATA) error {
	var pin runtime.Pinner
	pin.Pin(data)
	defer pin.Unpin()
	ret, _, err := procShellNotifyIconW.CallRaw(uintptr(message), uintptr(unsafe.Pointer(data)))
	runtime.KeepAlive(data)
	if ret == 0 {
		if err != nil {
			return err
		}
		return errors.New("Shell_NotifyIconW failed")
	}
	return nil
}
