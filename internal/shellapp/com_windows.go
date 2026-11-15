//go:build windows

package shellapp

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ole32 = windows.NewLazySystemDLL("ole32.dll")
var shell32 = windows.NewLazySystemDLL("shell32.dll")

type unknownVtbl struct{ QueryInterface, AddRef, Release uintptr }
type unknown struct{ vtbl *unknownVtbl }

func release(raw unsafe.Pointer) {
	syscall.SyscallN((*unknown)(raw).vtbl.Release, uintptr(raw))
}

func hresult(result uintptr, operation string) error {
	if int32(result) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08x", operation, uint32(result))
	}
	return nil
}

func enterCOM() (func(), error) {
	runtime.LockOSThread()
	result, _, _ := ole32.NewProc("CoInitializeEx").Call(0, 2)
	if uint32(result) == 0x80010106 {
		return runtime.UnlockOSThread, nil
	}
	if err := hresult(result, "initialize application Shell"); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	return func() {
		ole32.NewProc("CoUninitialize").Call()
		runtime.UnlockOSThread()
	}, nil
}

func takeString(value *uint16) string {
	if value == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value)
}
