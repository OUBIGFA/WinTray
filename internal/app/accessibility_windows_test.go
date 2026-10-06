//go:build windows

package app

import (
	"fmt"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"unsafe"
)

// UI Automation reaches mygo's rendered controls through their public
// accessibility interface, without coordinates or test hooks in the app.
func sessionButton(parent win.HWND, name string, invoke bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := windows.NewLazySystemDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0)
	if int32(hr) >= 0 {
		defer ole.NewProc("CoUninitialize").Call()
	}
	clsid := windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iid := windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	var automation unsafe.Pointer
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsid)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&automation)))
	if int32(hr) < 0 {
		return fmt.Errorf("UIAutomation: %#x", hr)
	}
	defer automationCall(automation, 2)
	var root unsafe.Pointer
	if hr = automationCall(automation, 6, uintptr(parent), uintptr(unsafe.Pointer(&root))); int32(hr) < 0 || root == nil {
		return fmt.Errorf("UIAutomation window: %#x", hr)
	}
	defer automationCall(root, 2)
	text, _ := windows.UTF16PtrFromString(name)
	oleaut := windows.NewLazySystemDLL("oleaut32.dll")
	bstr, _, _ := oleaut.NewProc("SysAllocString").Call(uintptr(unsafe.Pointer(text)))
	defer oleaut.NewProc("SysFreeString").Call(bstr)
	variant := struct {
		Type     uint16
		Reserved [3]uint16
		Value    uintptr
		Extra    uintptr
	}{Type: 8, Value: bstr}
	var condition unsafe.Pointer
	if hr = automationCall(automation, 23, 30005, uintptr(unsafe.Pointer(&variant)), uintptr(unsafe.Pointer(&condition))); int32(hr) < 0 {
		return fmt.Errorf("UIAutomation condition: %#x", hr)
	}
	defer automationCall(condition, 2)
	var element unsafe.Pointer
	if hr = automationCall(root, 5, 4, uintptr(condition), uintptr(unsafe.Pointer(&element))); int32(hr) < 0 || element == nil {
		return fmt.Errorf("button %q unavailable (%#x)", name, hr)
	}
	defer automationCall(element, 2)
	if !invoke {
		return nil
	}
	var pattern unsafe.Pointer
	if hr = automationCall(element, 16, 10000, uintptr(unsafe.Pointer(&pattern))); int32(hr) < 0 || pattern == nil {
		return fmt.Errorf("button %q has no Invoke pattern (%#x)", name, hr)
	}
	defer automationCall(pattern, 2)
	if hr = automationCall(pattern, 3); int32(hr) < 0 {
		return fmt.Errorf("invoke %q: %#x", name, hr)
	}
	return nil
}

func automationCall(object unsafe.Pointer, index int, args ...uintptr) uintptr {
	table := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(table, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	result, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(object)}, args...)...)
	return result
}

func sessionHasButton(parent win.HWND, name string) bool {
	return sessionButton(parent, name, false) == nil
}
