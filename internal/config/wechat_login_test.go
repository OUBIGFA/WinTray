package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestWeChatAutoLoginIsOptInAndExecutableScoped(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		enabled, hidden, want bool
	}{
		{"Weixin.exe", false, false, false},
		{"Weixin.exe", true, false, true},
		{"WECHAT.EXE", true, false, true},
		{"Weixin.exe", true, true, false},
		{"WeixinHelper.exe", true, false, false},
		{"Other.exe", true, false, false},
	} {
		entry := ManagedAppEntry{Name: "WeChat", ExePath: filepath.Join(t.TempDir(), tc.name), WeChatAutoLogin: tc.enabled, LaunchHiddenInBackground: tc.hidden}
		if got := WeChatAutoLoginEnabled(entry); got != tc.want {
			t.Errorf("%+v enabled=%t", tc, got)
		}
	}
	var legacy ManagedAppEntry
	if err := json.Unmarshal([]byte(`{"name":"WeChat","exePath":"Weixin.exe"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if WeChatAutoLoginEnabled(legacy) {
		t.Fatal("legacy settings enabled login")
	}
}
