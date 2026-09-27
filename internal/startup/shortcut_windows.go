//go:build windows

package startup

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const AppTaskHelperShortcut = "--launch-startup-shortcut"

type shortcutLaunch struct {
	path       string
	arguments  string
	directory  string
	show       int32
	runAsAdmin bool
}

// Only the methods used here are named. All vtable slots must remain in their
// COM ABI order; this is IShellLinkW, not IShellLinkA.
type shellLinkVtbl struct {
	win.IUnknownVtbl
	GetPath, GetIDList, SetIDList, GetDescription, SetDescription        uintptr
	GetWorkingDirectory, SetWorkingDirectory, GetArguments, SetArguments uintptr
	GetHotkey, SetHotkey, GetShowCmd, SetShowCmd                         uintptr
	GetIconLocation, SetIconLocation, SetRelativePath, Resolve, SetPath  uintptr
}

type shellLink struct{ vtbl *shellLinkVtbl }
type persistFileVtbl struct {
	win.IUnknownVtbl
	GetClassID, IsDirty, Load, Save, SaveCompleted, GetCurFile uintptr
}
type persistFile struct{ vtbl *persistFileVtbl }

var (
	clsidShellLink = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = windows.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = windows.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

func releaseCOM(raw unsafe.Pointer) {
	unknown := (*win.IUnknown)(raw)
	syscall.SyscallN(unknown.LpVtbl.Release, uintptr(raw))
}

func comResult(result uintptr, operation string) error {
	if int32(result) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08x", operation, uint32(result))
	}
	return nil
}

func readStartupShortcut(path string) (shortcutLaunch, error) {
	var result shortcutLaunch
	// Shell Link header carries RunAsUser. WScript.Shell does not expose it;
	// dropping it is exactly how an apparently identical shortcut loses UAC.
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	var header [76]byte
	_, err = io.ReadFull(file, header[:])
	file.Close()
	if err != nil {
		return result, err
	}
	if binary.LittleEndian.Uint32(header[:4]) != 76 || !bytes.Equal(header[4:20], []byte{1, 0x14, 2, 0, 0, 0, 0, 0, 0xc0, 0, 0, 0, 0, 0, 0, 0x46}) {
		return result, errors.New("invalid Shell Link header")
	}
	result.runAsAdmin = binary.LittleEndian.Uint32(header[20:24])&0x2000 != 0

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr := win.CoInitializeEx(nil, win.COINIT_APARTMENTTHREADED)
	if err := comResult(uintptr(hr), "initialize Shell Link COM"); err != nil {
		return result, err
	}
	defer win.CoUninitialize()
	var raw unsafe.Pointer
	hr = win.CoCreateInstance((*win.CLSID)(unsafe.Pointer(&clsidShellLink)), nil, win.CLSCTX_INPROC_SERVER,
		(*win.IID)(unsafe.Pointer(&iidShellLinkW)), &raw)
	if err := comResult(uintptr(hr), "create Shell Link"); err != nil {
		return result, err
	}
	link := (*shellLink)(raw)
	defer releaseCOM(raw)
	var persistent unsafe.Pointer
	r, _, _ := syscall.SyscallN(link.vtbl.QueryInterface, uintptr(raw), uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&persistent)))
	if err := comResult(r, "get IPersistFile"); err != nil {
		return result, err
	}
	defer releaseCOM(persistent)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return result, err
	}
	persist := (*persistFile)(persistent)
	r, _, _ = syscall.SyscallN(persist.vtbl.Load, uintptr(persistent), uintptr(unsafe.Pointer(name)), 0) // STGM_READ
	if err := comResult(r, "read startup shortcut"); err != nil {
		return result, err
	}
	buf := make([]uint16, 32768)
	r, _, _ = syscall.SyscallN(link.vtbl.GetPath, uintptr(raw), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 4) // SLGP_RAWPATH
	if err := comResult(r, "read shortcut target"); err != nil {
		return result, err
	}
	result.path = windows.UTF16ToString(buf)
	for _, field := range []struct {
		method uintptr
		value  *string
	}{{link.vtbl.GetArguments, &result.arguments}, {link.vtbl.GetWorkingDirectory, &result.directory}} {
		clear(buf)
		r, _, _ = syscall.SyscallN(field.method, uintptr(raw), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if err := comResult(r, "read shortcut launch attributes"); err != nil {
			return result, err
		}
		*field.value = windows.UTF16ToString(buf)
	}
	r, _, _ = syscall.SyscallN(link.vtbl.GetShowCmd, uintptr(raw), uintptr(unsafe.Pointer(&result.show)))
	if err := comResult(r, "read shortcut show command"); err != nil {
		return result, err
	}
	return result, nil
}

// LaunchStartupShortcut opens the original .lnk, not a reconstructed command.
// The shell retains its arguments, working directory, show command, AppUserModel
// metadata and RunAs flag. This one-shot helper runs no WinTray UI/autorun work.
func LaunchStartupShortcut(path, expectedExe string) error {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".lnk") {
		return errors.New("startup shortcut must be an absolute .lnk path")
	}
	link, err := readStartupShortcut(path)
	if err != nil {
		return err
	}
	target, err := expandedPath(link.path)
	if err != nil || !sameExecutablePath(target, expectedExe) {
		return errors.New("startup shortcut target changed; save its WinTray settings again")
	}
	return shellLaunchOriginal(path, "", "", link.show)
}

func shellLaunchOriginal(path, args, directory string, show int32) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(args)
	if err != nil {
		return err
	}
	var dir *uint16
	if directory != "" {
		dir, err = windows.UTF16PtrFromString(directory)
		if err != nil {
			return err
		}
	}
	info := &shellExecuteInfoW{
		cbSize: uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		fMask:  seeMaskNoCloseProcess | 0x00000100 | 0x00000400, // NOASYNC | FLAG_NO_UI
		lpFile: file, lpParameters: params, lpDirectory: dir, nShow: show,
	}
	r, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(info)))
	if r == 0 {
		return fmt.Errorf("launch original startup entry: %w", callErr)
	}
	if info.hProcess != 0 {
		windows.CloseHandle(info.hProcess)
	}
	return nil
}
