//go:build windows

package orchestrator

import (
	"context"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

func TestStartupVisibilityPartialCloseIsNotReportedAsComplete(t *testing.T) {
	entry, file := startupFixtureEntry(t, "partial")
	entry.TrayBehavior.CloseDelaySeconds = 0
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	got := svc.StartAndManage(context.Background(), entry, 2)
	state := awaitStartupFixture(t, file)
	if got.Managed || got.Code != ResultNoWindowManaged {
		t.Fatalf("partial close reported success: %+v", got)
	}
	if !isWindowVisible(state.Forms[1]) || visibilityOwner(state.Forms[1]) != 0 {
		t.Fatal("refusing window was stranded or disappeared")
	}
}

func TestStartupVisibilityPreservesNativeSilentWindows(t *testing.T) {
	entry, file := startupFixtureEntry(t, "silent")
	entry.TrayBehavior.CloseDelaySeconds = 0
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	got := svc.StartNow(context.Background(), entry, 1)
	state := awaitStartupFixture(t, file)
	if !got.Managed || got.Code != ResultStartedOnly {
		t.Fatalf("native silent launch=%+v", got)
	}
	for _, form := range state.Forms {
		if isWindowVisible(form) || visibilityOwner(form) != 0 {
			t.Fatal("an already-native-hidden window was revealed or left shielded")
		}
	}
	if win.SendMessage(win.HWND(state.Owner), win.WM_APP+22, 0, 0) != 0 {
		t.Fatal("native silent UI received an unnecessary close")
	}
}

func TestStartupVisibilityRestoresExistingLayeredAttributesAndAppStyleChanges(t *testing.T) {
	entry, file := startupFixtureEntry(t, "layered")
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	state := awaitStartupFixture(t, file)
	g, err := beginStartupVisibility(entry.ExePath, &testLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	if len(g.candidates()) != 2 {
		t.Fatal("layered fixture windows were not discovered")
	}
	for _, form := range state.Forms {
		var color, flags uint32
		var alpha byte
		procGetLayeredWindowAttributes.Call(form, uintptr(unsafe.Pointer(&color)), uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags)))
		if alpha != 0 {
			t.Fatal("layered UI was not shielded")
		}
		// A style change made by the app while startup is in progress must
		// survive release. WinTray removes only the flags it added itself.
		style := win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE)
		win.SetWindowLong(win.HWND(form), win.GWL_EXSTYLE, style|win.WS_EX_TRANSPARENT)
	}
	if err := g.close(); err != nil {
		t.Fatal(err)
	}
	for _, form := range state.Forms {
		var color, flags uint32
		var alpha byte
		procGetLayeredWindowAttributes.Call(form, uintptr(unsafe.Pointer(&color)), uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags)))
		style := win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE)
		if color != 0x112233 || alpha != 173 || flags != 3 || style&exLayered == 0 || style&win.WS_EX_TRANSPARENT == 0 || style&(exNoActivate|exToolWindow) != 0 {
			t.Fatalf("native attributes lost: color=%x alpha=%d flags=%x style=%x", color, alpha, flags, style)
		}
		if !isWindowVisible(form) {
			t.Fatal("release changed native window visibility")
		}
	}
}
