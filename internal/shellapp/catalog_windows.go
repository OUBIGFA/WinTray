//go:build windows

package shellapp

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type shellItemVtbl struct {
	unknownVtbl
	BindToHandler, GetParent, GetDisplayName, GetAttributes, Compare            uintptr
	GetPropertyStore, GetPropertyStoreWithCreateObject, GetPropertyStoreForKeys uintptr
	GetPropertyDescriptionList, Update, GetProperty, GetCLSID, GetFileTime      uintptr
	GetInt32, GetString, GetUInt32, GetUInt64, GetBool                          uintptr
}

type shellItem struct{ vtbl *shellItemVtbl }
type enumItemsVtbl struct {
	unknownVtbl
	Next, Skip, Reset, Clone uintptr
}
type enumItems struct{ vtbl *enumItemsVtbl }
type propertyKey struct {
	Format windows.GUID
	ID     uint32
}

var (
	iidShellItem  = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	iidShellItem2 = windows.GUID{Data1: 0x7e9fb0d3, Data2: 0x919f, Data3: 0x4307, Data4: [8]byte{0xab, 0x2e, 0x9b, 0x18, 0x60, 0x31, 0x0c, 0x93}}
	iidEnumItems  = windows.GUID{Data1: 0x70629033, Data2: 0xe363, Data3: 0x4a28, Data4: [8]byte{0xa5, 0x67, 0x0d, 0xb7, 0x80, 0x06, 0xe6, 0xd7}}
	bhidEnumItems = windows.GUID{Data1: 0x94f60519, Data2: 0x2850, Data3: 0x4924, Data4: [8]byte{0xaa, 0x5a, 0xd1, 0x5e, 0x84, 0x86, 0x80, 0x39}}
)

func openShellItem(path string) (*shellItem, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var item *shellItem
	result, _, _ := shell32.NewProc("SHCreateItemFromParsingName").Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item)))
	return item, hresult(result, "open application Shell item")
}

// Enumerate uses Windows' launchable-app catalog, not the uninstall inventory,
// which also contains runtimes, updates and components that cannot be opened.
func Enumerate(ctx context.Context) ([]Application, error) {
	leave, err := enterCOM()
	if err != nil {
		return nil, err
	}
	defer leave()
	folder, err := openShellItem("shell:AppsFolder")
	if err != nil {
		return nil, err
	}
	defer release(unsafe.Pointer(folder))
	var items *enumItems
	result, _, _ := syscall.SyscallN(folder.vtbl.BindToHandler, uintptr(unsafe.Pointer(folder)), 0, uintptr(unsafe.Pointer(&bhidEnumItems)), uintptr(unsafe.Pointer(&iidEnumItems)), uintptr(unsafe.Pointer(&items)))
	if err := hresult(result, "enumerate installed applications"); err != nil {
		return nil, err
	}
	defer release(unsafe.Pointer(items))
	keys := make(map[string]propertyKey)
	for _, name := range []string{"System.AppUserModel.ID", "System.Link.TargetParsingPath", "System.Link.Arguments", "System.Link.WorkingDirectory", "System.AppUserModel.PackageInstallPath"} {
		var key propertyKey
		wide, _ := windows.UTF16PtrFromString(name)
		result, _, _ := windows.NewLazySystemDLL("propsys.dll").NewProc("PSGetPropertyKeyFromName").Call(uintptr(unsafe.Pointer(wide)), uintptr(unsafe.Pointer(&key)))
		if int32(result) >= 0 {
			keys[name] = key
		}
	}
	var apps []Application
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var item *shellItem
		var fetched uint32
		result, _, _ := syscall.SyscallN(items.vtbl.Next, uintptr(unsafe.Pointer(items)), 1, uintptr(unsafe.Pointer(&item)), uintptr(unsafe.Pointer(&fetched)))
		if err := hresult(result, "read installed application"); err != nil {
			return nil, err
		}
		if fetched == 0 {
			break
		}
		app := applicationFromItem(item, keys)
		if app.Path != "" && !seen[app.Key()] {
			icon, err := iconFromItem(item, 64)
			if err != nil {
				log.Printf("read installed application icon %q: %v", app.Name, err)
			} else {
				app.Icon = icon
			}
			seen[app.Key()] = true
			apps = append(apps, app)
		}
		release(unsafe.Pointer(item))
	}
	sort.SliceStable(apps, func(first, second int) bool {
		left, right := strings.ToLower(apps[first].Name), strings.ToLower(apps[second].Name)
		if left == right {
			return apps[first].Key() < apps[second].Key()
		}
		return left < right
	})
	return apps, nil
}

func applicationFromItem(item *shellItem, keys map[string]propertyKey) Application {
	var displayName *uint16
	result, _, _ := syscall.SyscallN(item.vtbl.GetDisplayName, uintptr(unsafe.Pointer(item)), 0, uintptr(unsafe.Pointer(&displayName)))
	if int32(result) < 0 {
		return Application{}
	}
	name := takeString(displayName)
	var properties *shellItem
	result, _, _ = syscall.SyscallN(item.vtbl.QueryInterface, uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(&iidShellItem2)), uintptr(unsafe.Pointer(&properties)))
	if int32(result) < 0 {
		return Application{}
	}
	defer release(unsafe.Pointer(properties))
	get := func(name string) string {
		key, exists := keys[name]
		if !exists {
			return ""
		}
		var value *uint16
		result, _, _ := syscall.SyscallN(properties.vtbl.GetString, uintptr(unsafe.Pointer(properties)), uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&value)))
		if int32(result) < 0 {
			return ""
		}
		return takeString(value)
	}
	if identifier := get("System.AppUserModel.ID"); get("System.AppUserModel.PackageInstallPath") != "" && IsPath(PathPrefix+identifier) {
		return Application{Name: name, Path: PathPrefix + identifier}
	}
	path := strings.Trim(get("System.Link.TargetParsingPath"), "\"")
	if expanded, err := registry.ExpandString(path); err == nil {
		path = expanded
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe", ".bat", ".cmd", ".ps1", ".py", ".pyw", ".ahk":
	default:
		return Application{}
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return Application{}
	}
	return Application{Name: name, Path: filepath.Clean(path), Args: get("System.Link.Arguments"), WorkingDirectory: get("System.Link.WorkingDirectory")}
}
