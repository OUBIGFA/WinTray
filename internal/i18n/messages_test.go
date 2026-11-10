package i18n

import (
	"reflect"
	"strings"
	"testing"

	"wintray/internal/config"
)

func TestScriptAssociationErrorGivesNextStep(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		text := TranslateResultCode(language, "script_association_missing")
		if !strings.Contains(text, "AutoHotkey") || !strings.Contains(text, ".ahk") {
			t.Fatalf("%s association error lacks installation guidance: %q", language, text)
		}
	}
}

func TestLoginAwareModeDoesNotAdvertiseSavedDelay(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		entry := config.ManagedAppEntry{ExePath: "QQ.exe", RunOnStartup: true, QQWaitForLogin: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 45}}
		if got := formatManagedMode(language, entry); strings.Contains(got, "45") {
			t.Fatalf("inactive delay advertised: %q", got)
		}
		entry.QQWaitForLogin = false
		if got := formatManagedMode(language, entry); !strings.Contains(got, "45") {
			t.Fatalf("normal delay omitted: %q", got)
		}
		entry.QQWaitForLogin = true
		entry.ExePath = "other.exe"
		if got := formatManagedMode(language, entry); !strings.Contains(got, "45") {
			t.Fatalf("unrelated program's delay changed: %q", got)
		}
	}
}

func TestUICopyHasNoSentencePeriods(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		messages := reflect.ValueOf(For(language))
		fields := messages.Type()
		for i := 0; i < messages.NumField(); i++ {
			name, text := fields.Field(i).Name, messages.Field(i).String()
			if strings.Contains(text, "。") || strings.HasSuffix(text, ".") || strings.Contains(text, ". ") || strings.Contains(text, ".\\r\\n") {
				t.Errorf("%s %s contains a sentence period: %q", language, name, text)
			}
		}
	}
}

func TestFormatManagedRules(t *testing.T) {
	languages := []struct {
		language        string
		launchOnly      string
		taskLaunch      string
		autoHide        string
		autoHideDelayed string
		hidden          string
		paused          string
	}{
		{
			language:        "zh-CN",
			launchOnly:      "正常启动",
			taskLaunch:      "开机即启动",
			autoHide:        "收进托盘",
			autoHideDelayed: "收进托盘（等 30 秒）",
			hidden:          "后台启动",
			paused:          "原生行为",
		},
		{
			language:        "en-US",
			launchOnly:      "Start normally",
			taskLaunch:      "Launch at boot",
			autoHide:        "Close to tray",
			autoHideDelayed: "Close to tray (after 30 s)",
			hidden:          "Run in background",
			paused:          "Native behavior",
		},
		{
			language:        "unknown",
			launchOnly:      "正常启动",
			taskLaunch:      "开机即启动",
			autoHide:        "收进托盘",
			autoHideDelayed: "收进托盘（等 30 秒）",
			hidden:          "后台启动",
			paused:          "原生行为",
		},
	}

	for _, lang := range languages {
		t.Run(lang.language, func(t *testing.T) {
			states := []struct {
				name         string
				runOnStartup bool
				launchHidden bool
				autoHide     bool
				viaTask      bool
				closeDelay   int
				want         string
			}{
				{name: "launch_only", runOnStartup: true, want: lang.launchOnly},
				{name: "logon_task", runOnStartup: true, viaTask: true, want: lang.taskLaunch},
				{name: "close_window", runOnStartup: true, autoHide: true, want: lang.autoHide},
				{name: "close_window_delayed", runOnStartup: true, autoHide: true, closeDelay: 30, want: lang.autoHideDelayed},
				{name: "delay_without_close_window", runOnStartup: true, closeDelay: 30, want: lang.launchOnly},
				{name: "hidden_overrides_delayed_close", runOnStartup: true, launchHidden: true, autoHide: true, closeDelay: 30, want: lang.hidden},
				{name: "paused_overrides_delayed_close", autoHide: true, closeDelay: 30, want: lang.paused},
				{name: "hidden", runOnStartup: true, launchHidden: true, want: lang.hidden},
				{name: "hidden_overrides_close", runOnStartup: true, launchHidden: true, autoHide: true, want: lang.hidden},
				{name: "paused", want: lang.paused},
				{name: "paused_overrides_close", autoHide: true, want: lang.paused},
				{name: "paused_overrides_hidden", launchHidden: true, want: lang.paused},
				{name: "paused_overrides_task", viaTask: true, want: lang.paused},
				{name: "paused_overrides_both", launchHidden: true, autoHide: true, want: lang.paused},
			}
			for _, exePath := range []string{"test.exe", "test.bat"} {
				for _, state := range states {
					t.Run(exePath+"/"+state.name, func(t *testing.T) {
						app := config.ManagedAppEntry{
							Name:                     "Example app",
							ExePath:                  exePath,
							RunOnStartup:             state.runOnStartup,
							LaunchViaLogonTask:       state.viaTask,
							LaunchHiddenInBackground: state.launchHidden,
							TrayBehavior: config.TrayBehavior{
								AutoMinimizeAndHideOnLaunch: state.autoHide,
								CloseDelaySeconds:           state.closeDelay,
							},
						}
						if got := FormatManagedParam(lang.language, app); got != state.want {
							t.Errorf("FormatManagedParam() = %q, want %q", got, state.want)
						}
						wantItem := app.Name + " | " + app.ExePath + " | " + state.want
						if got := FormatManagedListItem(lang.language, app); got != wantItem {
							t.Errorf("FormatManagedListItem() = %q, want %q", got, wantItem)
						}
					})
				}
			}
		})
	}
}

func TestFor_UnknownLanguageFallsBackToChinese(t *testing.T) {
	for _, language := range []string{"", "unknown", "fr-FR"} {
		if got := Resolve(language); got != LangZhCN {
			t.Errorf("Resolve(%q) = %q, want %q", language, got, LangZhCN)
		}
		if For(language) != For(string(LangZhCN)) {
			t.Errorf("For(%q) did not return Chinese messages", language)
		}
	}
}

func TestMessages_AllFieldsArePopulated(t *testing.T) {
	for _, language := range LanguageOptions() {
		t.Run(language, func(t *testing.T) {
			messages := reflect.ValueOf(For(language))
			for i := 0; i < messages.NumField(); i++ {
				if strings.TrimSpace(messages.Field(i).String()) == "" {
					t.Errorf("%s is empty", messages.Type().Field(i).Name)
				}
			}
		})
	}
}

func TestTranslateResultCode_UsesTypedCode(t *testing.T) {
	if got := TranslateResultCode("en-US", "started_hidden"); got != "started in the background" {
		t.Fatalf("TranslateResultCode(en-US, started_hidden) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "started_hidden"); got != "已后台启动" {
		t.Fatalf("TranslateResultCode(zh-CN, started_hidden) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "managed"); got != "程序窗口已收起" {
		t.Fatalf("TranslateResultCode(zh-CN, managed) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "started_only"); got != "程序已启动" {
		t.Fatalf("TranslateResultCode(zh-CN, started_only) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "already_running_skipped"); got != "程序已在运行，已跳过启动" {
		t.Fatalf("TranslateResultCode(zh-CN, already_running_skipped) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "no_window_managed"); got != "等待超时，未检测到程序窗口" {
		t.Fatalf("TranslateResultCode(zh-CN, no_window_managed) = %q", got)
	}
	for _, code := range []string{"startup_check_failed", "external_startup_timeout", "cancelled"} {
		for _, lang := range []string{"zh-CN", "en-US"} {
			if got := TranslateResultCode(lang, code); got == "" || got == code {
				t.Errorf("TranslateResultCode(%s, %s) = %q, want localized result", lang, code, got)
			}
		}
		if IsLikelyPermissionCode(code) {
			t.Errorf("%s must not suggest window permissions", code)
		}
	}
	if got := TranslateResultCode("en-US", "unknown"); got != "" {
		t.Fatalf("TranslateResultCode(unknown) = %q, want empty", got)
	}
}
