//go:build windows

package branding

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Like the shipped executable, resource icon loading uses common controls v6.
func activateIconTestManifest(t testing.TB) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	path, err := filepath.Abs("../../build/WinTray.exe.manifest")
	if err != nil {
		t.Fatal(err)
	}
	source, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	act := win.CreateActCtx(&win.ACTCTX{Source: source})
	if act == win.HANDLE(windows.InvalidHandle) {
		t.Fatal("icon test manifest unavailable")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	t.Cleanup(func() { kernel.NewProc("ReleaseActCtx").Call(uintptr(act)) })
	cookie, ok := win.ActivateActCtx(act)
	if !ok {
		t.Fatal("activate icon test manifest")
	}
	t.Cleanup(func() { kernel.NewProc("DeactivateActCtx").Call(0, cookie) })
}

func TestAppIconUsesNativeWindowIconSize(t *testing.T) {
	activateIconTestManifest(t)
	icon, err := AppIcon()
	if err != nil {
		t.Fatal(err)
	}
	size := icon.Size()
	if size.Width != int(win.GetSystemMetricsForDpi(win.SM_CXICON, 96)) || size.Height != int(win.GetSystemMetricsForDpi(win.SM_CYICON, 96)) {
		t.Fatalf("application icon retains oversized bitmap: %+v", size)
	}
}

// Each iteration measures the first load, not the singleton cache lookup.
func BenchmarkAppIconLoad(b *testing.B) {
	activateIconTestManifest(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		appIconOnce = sync.Once{}
		icon, err := AppIcon()
		if err != nil {
			b.Fatal(err)
		}
		icon.Dispose()
	}
	appIconOnce = sync.Once{}
	appIcon, appIconErr = nil, nil
}
