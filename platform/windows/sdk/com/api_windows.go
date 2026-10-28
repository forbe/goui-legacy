package com

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/golang-gui/goui/internal/cgo"
)

var (
	ole32            = cgo.NewLazyLibrary("Ole32.dll")
	kernel32         = cgo.NewLazyLibrary("kernel32.dll")
	getCurrentThread = kernel32.NewSymbol("GetCurrentThreadId")
	coInitializeEx   = ole32.NewSymbol("CoInitializeEx")
	coUninitialize   = ole32.NewSymbol("CoUninitialize")
	coCreateInstance = ole32.NewSymbol("CoCreateInstance")
	coTaskMemFree    = ole32.NewSymbol("CoTaskMemFree")
	oleInitialize    = ole32.NewSymbol("OleInitialize")
	oleUninitialize  = ole32.NewSymbol("OleUninitialize")
	registerDragDrop = ole32.NewSymbol("RegisterDragDrop")
	revokeDragDrop   = ole32.NewSymbol("RevokeDragDrop")
	doDragDrop       = ole32.NewSymbol("DoDragDrop")
	releaseStgMedium = ole32.NewSymbol("ReleaseStgMedium")
)

// An apartment belongs to a thread, not to the process. A process-wide flag
// made a second OS thread believe it already had one, so it skipped
// CoInitializeEx and the next CoCreateInstance failed with CO_E_NOTINITIALIZED.
var (
	apartmentMu sync.Mutex
	apartments  = map[uint32]*apartment{}
)

// apartment records which initializers a thread owns. OleInitialize brings a
// CoInitializeEx of its own, so a thread that only initialized OLE has to be
// released with OleUninitialize. Releasing it with CoUninitialize drops the
// count below the number of initializations, which COM does not allow.
type apartment struct {
	ole bool // OleInitialize was called on this thread
	com bool // CoInitializeEx was called on this thread
}

func currentThread() uint32 {
	ret, _, _ := getCurrentThread.CallRaw()
	return uint32(ret)
}

// IsInitialized reports whether the calling thread has a COM apartment.
func IsInitialized() bool {
	apartmentMu.Lock()
	defer apartmentMu.Unlock()
	return apartments[currentThread()] != nil
}

// Initialize gives the calling thread a COM apartment. A thread that already
// has one keeps it: the concurrency model of an apartment cannot be changed.
func Initialize(coInit COINIT) HRESULT {
	tid := currentThread()
	apartmentMu.Lock()
	_, ok := apartments[tid]
	apartmentMu.Unlock()
	if ok {
		return 0 // S_OK: this thread already has an apartment
	}
	ret, _, _ := coInitializeEx.CallRaw(0, uintptr(coInit))
	hr := HRESULT(ret)
	if hr.Succeeded() {
		apartmentMu.Lock()
		apartments[tid] = &apartment{com: true}
		apartmentMu.Unlock()
	}
	return hr
}

// Uninitialize releases the COM apartment of the calling thread. It leaves the
// thread alone when the apartment came from OleInitialize, because
// OleUninitialize owns that count, and when the thread has no apartment of its
// own: releasing an apartment that was never set up unbalances the count.
func Uninitialize() {
	tid := currentThread()
	apartmentMu.Lock()
	a := apartments[tid]
	if a == nil || !a.com {
		apartmentMu.Unlock()
		return
	}
	a.com = false
	if !a.ole {
		delete(apartments, tid)
	}
	apartmentMu.Unlock()
	coUninitialize.CallRaw()
}

// InitializeOLE prepares the current STA for clipboard and drag-and-drop in
// addition to ordinary COM. Each successful call must pair with
// UninitializeOLE on the same OS thread.
func InitializeOLE() HRESULT {
	tid := currentThread()
	apartmentMu.Lock()
	a := apartments[tid]
	hasOLE := a != nil && a.ole
	apartmentMu.Unlock()
	if hasOLE {
		return 0 // S_OK: this thread already has an OLE apartment
	}
	ret, _, _ := oleInitialize.CallRaw(0)
	hr := HRESULT(ret)
	if hr.Failed() {
		return hr
	}
	apartmentMu.Lock()
	a = apartments[tid]
	if a == nil {
		a = &apartment{}
		apartments[tid] = a
	}
	a.ole = true
	apartmentMu.Unlock()
	return hr
}

// UninitializeOLE releases the OLE apartment of the calling thread. It leaves
// the thread alone when the thread never called OleInitialize: the OLE count
// of such a thread is zero, and dropping it below zero is what COM refuses to
// allow.
func UninitializeOLE() {
	tid := currentThread()
	apartmentMu.Lock()
	a := apartments[tid]
	if a == nil || !a.ole {
		apartmentMu.Unlock()
		return
	}
	a.ole = false
	if !a.com {
		delete(apartments, tid)
	}
	apartmentMu.Unlock()
	oleUninitialize.CallRaw()
}

func CreateInstance[T isUnknown](clsid CLSID, outer **T, clsCtx CLSCTX, iid IID) (inst *T, hr HRESULT) {
	out := new(*T)
	ret, _, _ := coCreateInstance.CallRaw(uintptr(cgo.Pointer(&clsid)), uintptr(cgo.Pointer(outer)), uintptr(clsCtx), uintptr(cgo.Pointer(&iid)), uintptr(cgo.Pointer(out)))
	inst = *out
	hr = HRESULT(ret)
	return
}

type isUnknown interface {
	IsUnknown()
}

type UnknownClass struct {
	QueryInterface cgo.Symbol //HRESULT(IUnknown *This,REFIID riid,void **ppvObject);
	AddRef         cgo.Symbol //ULONG(IUnknown *This);
	Release        cgo.Symbol //ULONG(IUnknown *This);
}

type Unknown struct {
	Class cgo.Pointer
}

func (Unknown) IsUnknown() {}

func (this *Unknown) QueryInterface(iid IID, ppvObject **Unknown) HRESULT {
	ret, _, _ := this.class().QueryInterface.CallRaw(uintptr(cgo.Pointer(this)), uintptr(cgo.Pointer(&iid)), uintptr(cgo.Pointer(ppvObject)))
	return HRESULT(ret)
}

func (this *Unknown) AddRef() ULONG {
	ret, _, _ := this.class().AddRef.CallRaw(uintptr(cgo.Pointer(this)))
	return ULONG(ret)
}

func (this *Unknown) Release() ULONG {
	ret, _, _ := this.class().Release.CallRaw(uintptr(cgo.Pointer(this)))
	return ULONG(ret)
}

func (this *Unknown) class() *UnknownClass {
	return (*UnknownClass)(this.Class)
}

// IDataObject

type DataObjectClass struct {
	UnknownClass
	GetData               cgo.Symbol
	GetDataHere           cgo.Symbol
	QueryGetData          cgo.Symbol
	GetCanonicalFormatEtc cgo.Symbol
	SetData               cgo.Symbol
	EnumFormatEtc         cgo.Symbol
	DAdvise               cgo.Symbol
	DUnadvise             cgo.Symbol
	EnumDAdvise           cgo.Symbol
}

type DataObject struct{ Unknown }

func (d *DataObject) QueryGetData(format *FormatEtc) HRESULT {
	var pin runtime.Pinner
	pin.Pin(format)
	defer pin.Unpin()
	ret, _, _ := (*DataObjectClass)(d.Class).QueryGetData.CallRaw(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(format)))
	return HRESULT(ret)
}

func (d *DataObject) GetData(format *FormatEtc, medium *StgMedium) HRESULT {
	var pin runtime.Pinner
	pin.Pin(format)
	pin.Pin(medium)
	defer pin.Unpin()
	ret, _, _ := (*DataObjectClass)(d.Class).GetData.CallRaw(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(format)), uintptr(unsafe.Pointer(medium)))
	return HRESULT(ret)
}

func RegisterDragDrop(hwnd uintptr, target unsafe.Pointer) HRESULT {
	ret, _, _ := registerDragDrop.CallRaw(hwnd, uintptr(target))
	return HRESULT(ret)
}

func RevokeDragDrop(hwnd uintptr) HRESULT {
	ret, _, _ := revokeDragDrop.CallRaw(hwnd)
	return HRESULT(ret)
}

func DoDragDrop(data, source unsafe.Pointer, effects uint32, effect *uint32) HRESULT {
	var pin runtime.Pinner
	pin.Pin(effect)
	defer pin.Unpin()
	ret, _, _ := doDragDrop.CallRaw(uintptr(data), uintptr(source), uintptr(effects), uintptr(unsafe.Pointer(effect)))
	return HRESULT(ret)
}

func ReleaseStgMedium(medium *StgMedium) {
	var pin runtime.Pinner
	pin.Pin(medium)
	defer pin.Unpin()
	releaseStgMedium.CallRaw(uintptr(unsafe.Pointer(medium)))
}

// CoTaskMemFree frees memory allocated by the COM subsystem (e.g. strings
// returned by IShellItem::GetDisplayName).
func CoTaskMemFree(ptr unsafe.Pointer) {
	coTaskMemFree.CallRaw(uintptr(ptr))
}

type HRESULT int32

func (hr HRESULT) Succeeded() bool {
	return hr >= 0
}

func (hr HRESULT) Failed() bool {
	return hr < 0
}

func (hr HRESULT) Code() int {
	return int(hr & 0xFFFF)
}

func (hr HRESULT) Facility() int {
	return int((hr >> 16) & 0x1FFF)
}

func (hr HRESULT) Severity() bool {
	return (hr>>31)&0x1 != 0
}

func (hr HRESULT) Error() string {
	return syscall.Errno(hr).Error()
}
