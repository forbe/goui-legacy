package cgo

// Call calls a C function.
func Call[F ~uintptr](fn F, args ...any) {
	call(uintptr(fn), nil, args...)
}

// CallRet calls a C function and returns a value.
func CallRet[T any, F ~uintptr](fn F, args ...any) (ret T) {
	return callRet[T](uintptr(fn), args...)
}
