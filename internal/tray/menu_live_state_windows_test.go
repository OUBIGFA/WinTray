//go:build windows

package tray

import (
	"image"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/i18n"
	"wintray/internal/traybox"
)

// Windows owns highlight/check/default state. A timer must not treat those
// bits as an icon update, repaint an unchanged row, or overwrite them when a
// program changes its tooltip/image.
func TestOpenMenuRefreshPreservesNativeInteractionState(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, flags := range []uint32{win.MFS_HILITE, win.MFS_CHECKED, win.MFS_DEFAULT, win.MFS_HILITE | win.MFS_CHECKED | win.MFS_DEFAULT} {
		menu, icons, bitmaps := newRefreshStateFixture(t)
		setRefreshTestState(t, menu, win.MFS_DISABLED|flags)
		before := refreshTestState(t, menu)
		for range 3 {
			if refreshBoxMenu(menu, icons, bitmaps, icons) {
				t.Errorf("unchanged icon requested repaint for native flags %#x", flags)
			}
		}
		if got := refreshTestState(t, menu); got != before {
			t.Errorf("unchanged icon changed native state: %#x -> %#x", before, got)
		}
	}

	menu, icons, bitmaps := newRefreshStateFixture(t)
	state := uint32(win.MFS_DISABLED | win.MFS_HILITE | win.MFS_CHECKED | win.MFS_DEFAULT)
	setRefreshTestState(t, menu, state)
	current := icons[0]
	current.Tooltip = "updated tooltip"
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) {
		t.Error("tooltip update was not applied")
	}
	if got := refreshTestState(t, menu); got != state {
		t.Errorf("tooltip update changed native state: %#x -> %#x", state, got)
	}
	current.Image = image.NewNRGBA(image.Rect(0, 0, 16, 16))
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) || bitmaps[0] == 0 {
		t.Error("image update was not applied")
	}
	if got := refreshTestState(t, menu); got != state {
		t.Errorf("image update changed native state: %#x -> %#x", state, got)
	}
	if refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) {
		t.Error("unchanged frame requested another repaint")
	}
}

func TestRemovedMenuIconIsDisabledOnlyOnce(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	menu, icons, bitmaps := newRefreshStateFixture(t)
	state := uint32(win.MFS_HILITE | win.MFS_CHECKED | win.MFS_DEFAULT)
	setRefreshTestState(t, menu, state)
	if !refreshBoxMenu(menu, icons, bitmaps, nil) {
		t.Error("removed icon was not disabled")
	}
	if got := refreshTestState(t, menu); got&win.MFS_DISABLED == 0 || got&^uint32(win.MFS_DISABLED) != state {
		t.Errorf("disabling removed icon overwrote other native state: %#x", got)
	}
	for range 5 {
		if refreshBoxMenu(menu, icons, bitmaps, nil) {
			t.Error("already-disabled removed icon requested another repaint")
		}
	}
}

func TestOpenMenuIgnoresTooltipChangesOutsideDisplayedLabel(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	menu, icons, bitmaps := newRefreshStateFixture(t)
	current := icons[0]
	// Only the first nonblank tooltip line is displayed in the menu.
	current.Tooltip += "\nupdated details"
	if refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) {
		t.Error("unchanged display name requested repaint")
	}
	if icons[0].Tooltip != current.Tooltip {
		t.Error("latest icon snapshot was not retained")
	}
}

func TestOpenMenuTooltipUpdateKeepsExistingBitmap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	menu, icons, bitmaps := newRefreshStateFixture(t)
	current := icons[0]
	current.Image = image.NewNRGBA(image.Rect(0, 0, 16, 16))
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) || bitmaps[0] == 0 {
		t.Fatal("image update was not applied")
	}
	bitmap := bitmaps[0]
	current.Tooltip = "a different label"
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) {
		t.Fatal("tooltip update was not applied")
	}
	var label [128]uint16
	item := win.MENUITEMINFO{
		CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_BITMAP | win.MIIM_STRING,
		DwTypeData: &label[0], Cch: uint32(len(label)),
	}
	if !win.GetMenuItemInfo(menu, trayBoxedBase, 0, &item) || item.HbmpItem != bitmap || bitmaps[0] != bitmap {
		t.Error("tooltip update replaced the icon bitmap")
	}
	if got := syscall.UTF16ToString(label[:]); got != current.DisplayName() {
		t.Errorf("menu label = %q, want %q", got, current.DisplayName())
	}
}

func newRefreshStateFixture(t *testing.T) (win.HMENU, []traybox.Icon, []win.HBITMAP) {
	t.Helper()
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("CreatePopupMenu")
	}
	icons := []traybox.Icon{{ExePath: `C:\WinTray-test\icon.exe`, Tooltip: "original tooltip"}}
	bitmaps, ok := appendContextMenu(menu, i18n.For("en-US"), icons)
	t.Cleanup(func() {
		win.DestroyMenu(menu)
		for _, bitmap := range bitmaps {
			if bitmap != 0 {
				win.DeleteObject(win.HGDIOBJ(bitmap))
			}
		}
	})
	if !ok {
		t.Fatal("appendContextMenu")
	}
	return menu, icons, bitmaps
}

func setRefreshTestState(t *testing.T, menu win.HMENU, state uint32) {
	t.Helper()
	item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STATE, FState: state}
	if !win.SetMenuItemInfo(menu, trayBoxedBase, false, &item) || refreshTestState(t, menu) != state {
		t.Fatalf("could not set native state %#x", state)
	}
}

func refreshTestState(t *testing.T, menu win.HMENU) uint32 {
	t.Helper()
	item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STATE}
	if !win.GetMenuItemInfo(menu, trayBoxedBase, 0, &item) {
		t.Fatal("GetMenuItemInfo state")
	}
	return item.FState
}
