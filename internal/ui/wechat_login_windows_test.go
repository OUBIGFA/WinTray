//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"path/filepath"
	"testing"
	"wintray/internal/config"
)

// The option follows the executable, survives saving, and cannot act in hidden mode.
func TestWeChatLoginOptionVisibilityAndSave(t *testing.T) {
	for _, name := range []string{"Weixin.exe", "WECHAT.EXE", "other.exe", "WeixinHelper.exe"} {
		t.Run(name, func(t *testing.T) {
			w := nativeProgramsFixture(1)
			w.settings.ManagedApps[0].ExePath = filepath.Join(t.TempDir(), name)
			store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
			w.callbacks.OnSave = func(s config.Settings) {
				if err := store.Save(s); err != nil {
					t.Fatal(err)
				}
			}
			tt := native.NewTester(w.nativeView, 1200, 900)
			if err := tt.Click("Program 000"); err != nil {
				t.Fatal(err)
			}
			_, shown := tt.Find("微信自动登录")
			want := name == "Weixin.exe" || name == "WECHAT.EXE"
			if shown != want {
				t.Fatalf("login option shown=%t, want %t", shown, want)
			}
			if !want {
				return
			}
			if err := tt.Click("微信自动登录"); err != nil {
				t.Fatal(err)
			}
			got, err := store.LoadWithError()
			if err != nil || !got.ManagedApps[0].WeChatAutoLogin {
				t.Fatalf("login setting not saved: %v, %+v", err, got.ManagedApps)
			}
			captureNativeTest(t, tt, "wechat-login")
		})
	}
}
