//go:build windows

package traybox

import "testing"

func TestPackagedAppTrayRegistrationKeepsSelectedApplicationIdentity(t *testing.T) {
	_, listener := selectedListener(t)
	owner := uint32(listener.hwnd)
	data := trayData{HWnd: owner, UID: 7, Message: nimAdd}
	raw := trayRequest{hwnd: owner, uid: 7}.bytes()
	listener.record("c:/apps/reader.exe", data, raw, nil)
	path := "shell:appsfolder\\example.app_123456789abcd!reader"
	listener.record(path, data, raw, nil)
	if got := listener.icons[idOf(data)].ExePath; got != path {
		t.Fatalf("new registration kept the old file identity: %q", got)
	}
}

func TestPackagedAppTrayPathStaysVirtual(t *testing.T) {
	path := "SHELL:AppsFolder\\Example.App_123456789abcd!Reader"
	if got := canonicalIconPath(path); got != "shell:appsfolder\\example.app_123456789abcd!reader" {
		t.Fatalf("tray collection changed the application ID: %q", got)
	}
}
