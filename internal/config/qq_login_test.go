package config

import (
	"encoding/json"
	"testing"
)

// The opt-in must survive a save/load cycle without changing older settings.
func TestQQWaitForLoginPersistsIndependently(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		input := `{"exePath":"QQ.exe","qqWaitForLogin":false,"wechatAutoLogin":true}`
		if enabled {
			input = `{"exePath":"QQ.exe","qqWaitForLogin":true,"wechatAutoLogin":false}`
		}
		var entry ManagedAppEntry
		if err := json.Unmarshal([]byte(input), &entry); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var stored map[string]any
		if err := json.Unmarshal(data, &stored); err != nil {
			t.Fatal(err)
		}
		if got, ok := stored["qqWaitForLogin"]; !ok || got != enabled {
			t.Fatalf("QQ login wait was not preserved: %s", data)
		}
		if stored["wechatAutoLogin"] != !enabled {
			t.Fatal("QQ option changed WeChat auto login")
		}
	}
}

func TestLoginWaitOnlyReplacesOptedInTrayDelay(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		qq, wechat, tray, hidden, task, want bool
	}{
		{"QQ.exe", true, false, true, false, false, true},
		{"qq.EXE", true, false, true, false, false, true},
		{"QQ.exe", false, false, true, false, false, false},
		{"QQ.exe", true, false, false, false, false, false},
		{"QQ.exe", true, false, true, true, false, false},
		{"QQ.exe", true, false, true, false, true, false},
		{"other.exe", true, true, true, false, false, false},
		{"Weixin.exe", false, true, true, false, false, true},
		{"WeChat.exe", true, false, true, false, false, false},
	} {
		entry := ManagedAppEntry{ExePath: tc.name, QQWaitForLogin: tc.qq, WeChatAutoLogin: tc.wechat, LaunchHiddenInBackground: tc.hidden, LaunchViaLogonTask: tc.task, TrayBehavior: TrayBehavior{AutoMinimizeAndHideOnLaunch: tc.tray, CloseDelaySeconds: 45}}
		if got := WaitForLoginBeforeTray(entry); got != tc.want {
			t.Errorf("%+v: enabled=%t", tc, got)
		}
	}
}
