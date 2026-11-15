//go:build windows

package shellapp

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type activationVtbl struct {
	unknownVtbl
	ActivateApplication, ActivateForFile, ActivateForProtocol uintptr
}
type activationManager struct{ vtbl *activationVtbl }

var (
	clsidActivationManager = windows.GUID{Data1: 0x45ba127d, Data2: 0x10a8, Data3: 0x46ea, Data4: [8]byte{0x8a, 0xb7, 0x56, 0xea, 0x90, 0x78, 0x94, 0x3c}}
	iidActivationManager   = windows.GUID{Data1: 0x2e941141, Data2: 0x7f97, Data3: 0x4756, Data4: [8]byte{0xba, 0x1d, 0x9d, 0xec, 0xde, 0x89, 0x4a, 0x3d}}
)

// Activate passes the argument string intact. Windows resolves the current
// package installation and owns the activation context and working directory.
func Activate(path, arguments string) (uint32, error) {
	identifier := ID(path)
	if identifier == "" {
		return 0, errors.New("invalid packaged application ID")
	}
	appID, err := windows.UTF16PtrFromString(identifier)
	if err != nil {
		return 0, err
	}
	args, err := windows.UTF16PtrFromString(arguments)
	if err != nil {
		return 0, err
	}
	leave, err := enterCOM()
	if err != nil {
		return 0, err
	}
	defer leave()
	var manager *activationManager
	result, _, _ := ole32.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsidActivationManager)), 0, 5, uintptr(unsafe.Pointer(&iidActivationManager)), uintptr(unsafe.Pointer(&manager)))
	if err := hresult(result, "create application activation manager"); err != nil {
		return 0, err
	}
	defer release(unsafe.Pointer(manager))
	var processID uint32
	result, _, _ = syscall.SyscallN(manager.vtbl.ActivateApplication, uintptr(unsafe.Pointer(manager)), uintptr(unsafe.Pointer(appID)), uintptr(unsafe.Pointer(args)), 2, uintptr(unsafe.Pointer(&processID)))
	if err := hresult(result, "activate packaged application"); err != nil {
		return 0, err
	}
	if processID == 0 {
		return 0, errors.New("application activation returned no process")
	}
	return processID, nil
}

func ProcessID(process windows.Handle) string {
	var length uint32
	query := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetApplicationUserModelId")
	result, _, _ := query.Call(uintptr(process), uintptr(unsafe.Pointer(&length)), 0)
	if result != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || length == 0 {
		return ""
	}
	buffer := make([]uint16, length)
	result, _, _ = query.Call(uintptr(process), uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buffer[0])))
	if result != 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}

func ProcessPath(processID uint32) string {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(process)
	if identifier := ProcessID(process); identifier != "" {
		return PathPrefix + identifier
	}
	return ""
}
