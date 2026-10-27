package cgo

import (
	"math"
	"reflect"
	"runtime"
	"syscall"
	"unsafe"
)

func callRet[T any](fn uintptr, args ...any) (ret T) {
	call(fn, &ret, args...)
	return
}

func call(fn uintptr, ret any, args ...any) {
	const PointerSize = unsafe.Sizeof(uintptr(0))

	rawArgs := make([]uintptr, 0, len(args)+1)
	var retValue reflect.Value
	var retType reflect.Type
	if ret != nil {
		retValue = reflect.ValueOf(ret)
		retType = retValue.Type().Elem()
		checkReturnType(retType)

		if retType.Kind() != reflect.String && retType.Size() > PointerSize {
			rawArgs = append(rawArgs, retValue.Pointer())
		}
	}

	for _, arg := range args {
		argType := reflect.TypeOf(arg)
		checkCgoType(argType)

		argVal := reflect.ValueOf(arg)
		switch argType.Kind() {
		case reflect.String:
			p := CStringTemp(argVal.String())
			defer runtime.KeepAlive(p)
			rawArgs = append(rawArgs, uintptr(p))
		case reflect.Bool:
			rawArgs = append(rawArgs, uintptr(CBool(argVal.Bool())))
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			rawArgs = append(rawArgs, uintptr(argVal.Int()))
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			rawArgs = append(rawArgs, uintptr(argVal.Uint()))
		case reflect.Float32:
			rawArgs = append(rawArgs, uintptr(math.Float32bits(float32(argVal.Float()))))
		case reflect.Float64:
			rawArgs = append(rawArgs, uintptr(math.Float64bits(argVal.Float())))
		case reflect.Pointer, reflect.UnsafePointer:
			rawArgs = append(rawArgs, argVal.Pointer())
		default:
			value := reflect.New(argType)
			value.Elem().Set(argVal)
			defer runtime.KeepAlive(value)
			if size := argType.Size(); size <= PointerSize {
				rawArgs = append(rawArgs, 0)
				Memcpy(unsafe.Pointer(&rawArgs[len(rawArgs)-1]), value.Elem().Addr().UnsafePointer(), Sizet(size))
			} else {
				// pass address
				rawArgs = append(rawArgs, value.Pointer())
			}
		}
	}

	r1, r2, _ := syscall.SyscallN(fn, rawArgs...)
	if ret != nil {
		retVal := retValue.Elem()
		switch retType.Kind() {
		case reflect.String:
			retVal.SetString(GoString(unsafe.Pointer(r1)))
		case reflect.Bool:
			retVal.SetBool(r1 != 0)
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			retVal.SetInt(int64(r1))
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			retVal.SetUint(uint64(r1))
		case reflect.Float32:
			retVal.SetFloat(float64(math.Float32frombits(uint32(r2))))
		case reflect.Float64:
			retVal.SetFloat(math.Float64frombits(uint64(r2)))
		default:
			if size := retType.Size(); size <= PointerSize {
				Memcpy(retVal.Addr().UnsafePointer(), unsafe.Pointer(&r1), Sizet(size))
			}
		}
	}
}

func checkReturnType(retType reflect.Type) {
	switch retType.Kind() {
	case reflect.Array:
		panic("cgo.Call: unsupported return type")
	default:
		checkCgoType(retType)
	}
}

func checkCgoType(typ reflect.Type) {
	switch typ.Kind() {
	case reflect.Slice, reflect.Interface, reflect.Map, reflect.Chan, reflect.Func:
		panic("cgo.Call: unsupported cgo type")
	case reflect.Array:
		checkCgoType(typ.Elem())
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			checkCgoType(typ.Field(i).Type)
		}
	}
}
