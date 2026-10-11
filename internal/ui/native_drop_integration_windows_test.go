//go:build windows && (amd64 || arm64)

package ui

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	native "github.com/egoist/mygo/ui"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

// Exercise the real OLE target and rendered hit testing, without running the
// app's startup orchestrator or touching the user's settings. Shell creates
// the same file data object Explorer offers; no private toolkit hook is used.
func TestNativeProgramListFileDrops(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive desktop")
	}
	if runWindowTestInChild(t) {
		return
	}
	dir := t.TempDir()
	paths := []string{droppedTestFile(t, dir, "拖入 程序.exe"), droppedTestFile(t, dir, "script.cmd")}
	runTestMainThread(func() {
		initial := config.DefaultSettings()
		initial.RunAtLogon = false
		saves := 0
		w, err := NewMainWindow(initial, Callbacks{OnSave: func(config.Settings) { saves++ }})
		if err != nil {
			t.Error(err)
			return
		}
		w.OnStarting(func() {
			go func() {
				defer w.RequestExplicitClose()
				step := func(fn func() error) bool {
					done := make(chan error, 1)
					w.Synchronize(func() { done <- fn() })
					select {
					case err := <-done:
						if err != nil {
							t.Error(err)
							return false
						}
					case <-time.After(5 * time.Second):
						t.Error("file-drop window did not dispatch")
						return false
					}
					// Drops are delivered to the view on its next frame, not in
					// IDropTarget::Drop itself. Let that frame finish first.
					time.Sleep(150 * time.Millisecond)
					return true
				}
				w.ShowMainWindow()
				if !step(func() error {
					if w.desktop.window == nil || !w.desktop.window.IsVisible() {
						return fmt.Errorf("file-drop window was not shown")
					}
					return nil
				}) {
					return
				}
				count, wantSaves := 0, 0
				drop := func(where string, files []string, accept bool) bool {
					if !step(func() error {
						hwnd := win.HWND(w.desktop.window.NativeHandle())
						var client win.RECT
						win.GetClientRect(hwnd, &client)
						scale := float32(win.GetDpiForWindow(hwnd)) / 96
						width, height := int(float32(client.Right)/scale), int(float32(client.Bottom)/scale)
						x, y := float32(150), float32(250)
						switch where {
						case "empty list", "row control":
							// Resolve the target's DIP bounds from the same layout
							// without changing the live window's widget state.
							layout, _ := NewMainWindow(w.Settings(), Callbacks{})
							tt := native.NewTester(layout.nativeView, width, height)
							m := i18n.For(w.settings.Language)
							label := m.ManagedListEmpty
							if where == "row control" {
								label = m.ManagedColumnOrder + " " + w.settings.ManagedApps[0].Name
							}
							r, ok := tt.Find(label)
							if !ok {
								return fmt.Errorf("missing drop target %q", label)
							}
							x, y = r.X+r.W/2, r.Y+r.H/2
						case "blank list":
							y = float32(height - 140)
						case "editor":
							x = float32(width - 100)
						}
						over, dropped, err := shellDropTestFiles(hwnd, x, y, files)
						if err != nil {
							return err
						}
						if over != accept || dropped != accept {
							return fmt.Errorf("%s: drag-over=%t drop=%t, want %t", where, over, dropped, accept)
						}
						return nil
					}) {
						return false
					}
					if accept {
						count += len(files)
						wantSaves++
					}
					return step(func() error {
						if len(w.settings.ManagedApps) != count || saves != wantSaves {
							return fmt.Errorf("%s: entries=%d saves=%d, want %d/%d", where, len(w.settings.ManagedApps), saves, count, wantSaves)
						}
						if accept && w.desktop.selected != count-1 {
							return fmt.Errorf("%s did not select the added program", where)
						}
						return nil
					})
				}
				if !drop("empty list", paths, true) || !drop("row control", paths[:1], true) || !drop("blank list", paths[1:], true) || !drop("editor", paths, false) {
					return
				}
				if !step(func() error { w.onSettingsPage = true; return nil }) || !drop("settings page", paths, false) {
					return
				}
				if !step(func() error { w.HideMainWindow(); return nil }) {
					return
				}
				w.ShowMainWindow()
				if !step(func() error {
					if w.desktop.window == nil || !w.desktop.window.IsVisible() || w.onSettingsPage {
						return fmt.Errorf("program list did not reopen")
					}
					return nil
				}) {
					return
				}
				drop("reopened list", paths[:1], true)
			}()
		})
		if code := w.Run(); code != 0 {
			t.Errorf("file-drop desktop exited with %d", code)
		}
	})
}

// The OLE window property is only read inside the owning UI thread. It points
// to the registered IDropTarget; Windows and the window retain its lifetime.
func shellDropTestFiles(hwnd win.HWND, x, y float32, paths []string) (over, dropped bool, err error) {
	property, _ := windows.UTF16PtrFromString("OleDropTargetInterface")
	getProp := windows.NewLazySystemDLL("user32.dll").NewProc("GetPropW")
	target, _, _ := getProp.Call(uintptr(hwnd), uintptr(unsafe.Pointer(property)))
	// The rendered content may own a child HWND filling the client area.
	for child := win.GetWindow(hwnd, win.GW_CHILD); target == 0 && child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		target, _, _ = getProp.Call(uintptr(child), uintptr(unsafe.Pointer(property)))
	}
	if target == 0 {
		return false, false, fmt.Errorf("window has no OLE drop target")
	}
	pidls := make([]uintptr, len(paths))
	for i, path := range paths {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return false, false, err
		}
		if hr := win.SHParseDisplayName(name, 0, &pidls[i], 0, nil); int32(hr) < 0 {
			return false, false, fmt.Errorf("parse shell path %q: %#x", path, uint32(hr))
		}
		defer win.CoTaskMemFree(pidls[i])
	}
	var items uintptr
	hr, _, _ := windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateShellItemArrayFromIDLists").Call(uintptr(len(pidls)), uintptr(unsafe.Pointer(&pidls[0])), uintptr(unsafe.Pointer(&items)))
	runtime.KeepAlive(pidls)
	if int32(hr) < 0 || items == 0 {
		return false, false, fmt.Errorf("create shell item array: %#x", hr)
	}
	defer fileDropCOMCall(items, 2)
	// Bind the real filesystem items, rather than SHCreateDataObject, which
	// only supplies CFSTR_SHELLIDLIST unless extra formats are added by hand.
	handler := windows.GUID{Data1: 0xb8c0bd9f, Data2: 0xed24, Data3: 0x455c, Data4: [8]byte{0x83, 0xe6, 0xd5, 0x39, 0x0c, 0x4f, 0xe8, 0xc4}}
	iid := windows.GUID{Data1: 0x0000010e, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	var data uintptr
	hr = fileDropCOMCall(items, 3, 0, uintptr(unsafe.Pointer(&handler)), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&data)))
	if int32(hr) < 0 || data == 0 {
		return false, false, fmt.Errorf("bind shell file data: %#x", hr)
	}
	defer fileDropCOMCall(data, 2)
	format := struct {
		Format uint16
		Device uintptr
		Aspect uint32
		Index  int32
		Tymed  uint32
	}{Format: win.CF_HDROP, Aspect: 1, Index: -1, Tymed: 1}
	if hr = fileDropCOMCall(data, 5, uintptr(unsafe.Pointer(&format))); hr != 0 {
		return false, false, fmt.Errorf("shell data does not offer CF_HDROP: %#x", hr)
	}
	scale := float32(win.GetDpiForWindow(hwnd)) / 96
	point := win.POINT{X: int32(x * scale), Y: int32(y * scale)}
	if !win.ClientToScreen(hwnd, &point) {
		return false, false, fmt.Errorf("convert drop point to screen")
	}
	packed := uintptr(uint32(point.X)) | uintptr(uint32(point.Y))<<32
	const effects = 1 | 2 | 4 // Explorer offers copy, move and link; WinTray must choose copy.
	effect := uint32(effects)
	if hr = fileDropCOMCall(target, 3, data, 1, packed, uintptr(unsafe.Pointer(&effect))); int32(hr) < 0 {
		return false, false, fmt.Errorf("DragEnter: %#x", hr)
	}
	effect = effects
	if hr = fileDropCOMCall(target, 4, 1, packed, uintptr(unsafe.Pointer(&effect))); int32(hr) < 0 {
		fileDropCOMCall(target, 5)
		return false, false, fmt.Errorf("DragOver: %#x", hr)
	}
	over = effect == 1
	effect = effects
	if hr = fileDropCOMCall(target, 6, data, 0, packed, uintptr(unsafe.Pointer(&effect))); int32(hr) < 0 {
		return over, false, fmt.Errorf("Drop: %#x", hr)
	}
	return over, effect == 1, nil
}

func fileDropCOMCall(object uintptr, method int, args ...uintptr) uintptr {
	// These are foreign COM allocations, not pointers into Go-managed memory.
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&object))
	vtable := *(*unsafe.Pointer)(pointer)
	fn := *(*uintptr)(unsafe.Add(vtable, uintptr(method)*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(fn, append([]uintptr{object}, args...)...)
	return hr
}
