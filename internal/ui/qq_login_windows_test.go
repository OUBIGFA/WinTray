//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"path/filepath"
	"testing"
	"wintray/internal/config"
)

func TestQQLoginWaitOptionAndSavedDelay(t *testing.T) {
	for _, name := range []string{"QQ.exe", "qq.EXE", "QQHelper.exe", "other.exe"} {
		t.Run(name, func(t *testing.T) {
			w := nativeProgramsFixture(1)
			entry := &w.settings.ManagedApps[0]
			entry.ExePath = filepath.Join(t.TempDir(), name)
			entry.TrayBehavior.AutoMinimizeAndHideOnLaunch = true
			entry.TrayBehavior.CloseDelaySeconds = 45
			store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
			w.callbacks.OnSave = func(s config.Settings) {
				if err := store.Save(s); err != nil {
					t.Fatal(err)
				}
			}
			tt := native.NewTester(w.nativeView, 1200, 1000)
			if err := tt.Click("Program 000"); err != nil {
				t.Fatal(err)
			}
			_, shown := tt.Find("登录成功后收起")
			if want := name == "QQ.exe" || name == "qq.EXE"; shown != want {
				t.Fatalf("QQ option shown=%t want=%t", shown, want)
			}
			if !shown {
				return
			}
			if err := tt.Click("登录成功后收起"); err != nil {
				t.Fatal(err)
			}
			got, err := store.LoadWithError()
			if err != nil || !got.ManagedApps[0].QQWaitForLogin || got.ManagedApps[0].WeChatAutoLogin {
				t.Fatalf("QQ option save: %+v %v", got, err)
			}
			if tt.HasText("收起前等待（秒）") {
				t.Fatal("inactive delay is still presented as effective")
			}
			captureNativeTest(t, tt, "qq-login-wait-on")
			if err := tt.Click("登录成功后收起"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("收起前等待（秒）") || w.settings.ManagedApps[0].TrayBehavior.CloseDelaySeconds != 45 {
				t.Fatal("switching off lost the original delay")
			}
			captureNativeTest(t, tt, "qq-login-wait-off")
		})
	}
}
