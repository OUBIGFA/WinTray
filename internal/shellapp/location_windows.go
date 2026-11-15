//go:build windows

package shellapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func InstallationDirectory(path string) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), "\"")
	var directory string
	if IsPath(path) {
		var err error
		directory, err = packageInstallationDirectory(path)
		if err != nil {
			return "", err
		}
	} else {
		if !filepath.IsAbs(path) {
			return "", errors.New("program file path is not absolute")
		}
		directory = filepath.Dir(path)
	}
	if !filepath.IsAbs(directory) {
		return "", errors.New("application has no installation directory")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("application location is not a folder")
	}
	return filepath.Clean(directory), nil
}

func packageInstallationDirectory(path string) (string, error) {
	leave, err := enterCOM()
	if err != nil {
		return "", err
	}
	defer leave()
	item, err := openShellItem(path)
	if err != nil {
		return "", err
	}
	defer release(unsafe.Pointer(item))
	var properties *shellItem
	result, _, _ := syscall.SyscallN(item.vtbl.QueryInterface, uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(&iidShellItem2)), uintptr(unsafe.Pointer(&properties)))
	if err := hresult(result, "get application location properties"); err != nil {
		return "", err
	}
	defer release(unsafe.Pointer(properties))
	name, _ := windows.UTF16PtrFromString("System.AppUserModel.PackageInstallPath")
	var key propertyKey
	result, _, _ = windows.NewLazySystemDLL("propsys.dll").NewProc("PSGetPropertyKeyFromName").Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&key)))
	if err := hresult(result, "resolve application location property"); err != nil {
		return "", err
	}
	var value *uint16
	result, _, _ = syscall.SyscallN(properties.vtbl.GetString, uintptr(unsafe.Pointer(properties)), uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&value)))
	if err := hresult(result, "read application installation directory"); err != nil {
		return "", err
	}
	return takeString(value), nil
}
