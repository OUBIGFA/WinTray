//go:build windows

package shellapp

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var windowUser32 = windows.NewLazySystemDLL("user32.dll")
var windowThreadProcess = windowUser32.NewProc("GetWindowThreadProcessId")
var windowClassName = windowUser32.NewProc("GetClassNameW")
var findChildWindow = windowUser32.NewProc("FindWindowExW")

func WindowProcessID(hwnd uintptr) uint32 {
	var processID uint32
	windowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&processID)))
	var className [256]uint16
	windowClassName.Call(hwnd, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	if windows.UTF16ToString(className[:]) != "ApplicationFrameWindow" {
		return processID
	}
	parents := []uintptr{hwnd}
	for len(parents) > 0 {
		parent := parents[len(parents)-1]
		parents = parents[:len(parents)-1]
		var after uintptr
		for {
			child, _, _ := findChildWindow.Call(parent, after, 0, 0)
			if child == 0 {
				break
			}
			after = child
			var childProcessID uint32
			windowThreadProcess.Call(child, uintptr(unsafe.Pointer(&childProcessID)))
			if childProcessID != 0 && childProcessID != processID && ProcessPath(childProcessID) != "" {
				return childProcessID
			}
			parents = append(parents, child)
		}
	}
	return processID
}
