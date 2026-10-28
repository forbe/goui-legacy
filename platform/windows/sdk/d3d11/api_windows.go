package d3d11

import (
	"github.com/golang-gui/goui/internal/cgo"
	"github.com/golang-gui/goui/platform/windows/sdk/com"
)

var (
	d3d11             = cgo.NewLazyLibrary("d3d11.dll")
	d3d11CreateDevice = d3d11.NewSymbol("D3D11CreateDevice")
)

// Device and DeviceContext only need IUnknown for this backend. Direct2D uses
// the device through IDXGIDevice and the immediate context is released after
// creation.
type Device struct{ com.Unknown }
type DeviceContext struct{ com.Unknown }

func CreateDevice(driverType DriverType, flags CreateDeviceFlag, levels []FeatureLevel) (device *Device, selected FeatureLevel, immediate *DeviceContext, hr com.HRESULT) {
	if err := d3d11CreateDevice.Find(); err != nil {
		return nil, 0, nil, com.HRESULT(-2147467259)
	}
	// 同 d2d1.Factory1.CreateDevice：写回目标放堆上，避免栈地址在 syscall 期间失效。
	deviceOut := new(*Device)
	selectedOut := new(FeatureLevel)
	immediateOut := new(*DeviceContext)
	ret, _, _ := d3d11CreateDevice.CallRaw(
		0,
		uintptr(driverType),
		0,
		uintptr(flags),
		uintptr(cgo.CSlice(levels)),
		uintptr(len(levels)),
		D3D11_SDK_VERSION,
		uintptr(cgo.Pointer(deviceOut)),
		uintptr(cgo.Pointer(selectedOut)),
		uintptr(cgo.Pointer(immediateOut)),
	)
	device = *deviceOut
	selected = *selectedOut
	immediate = *immediateOut
	hr = com.HRESULT(ret)
	return
}
