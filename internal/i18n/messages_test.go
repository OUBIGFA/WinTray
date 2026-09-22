package i18n

import (
	"reflect"
	"strings"
	"testing"

	"wintray/internal/config"
)

func TestFormatManagedRules(t *testing.T) {
	languages := []struct {
		language   string
		launchOnly string
		autoHide   string
		hidden     string
		paused     string
	}{
		{
			language:   "zh-CN",
			launchOnly: "仅启动",
			autoHide:   "启动后关闭窗口",
			hidden:     "后台静默启动",
			paused:     "已暂停",
		},
		{
			language:   "en-US",
			launchOnly: "Launch only",
			autoHide:   "Close window after launch",
			hidden:     "Launch hidden in background",
			paused:     "Paused",
		},
		{
			language:   "unknown",
			launchOnly: "仅启动",
			autoHide:   "启动后关闭窗口",
			hidden:     "后台静默启动",
			paused:     "已暂停",
		},
	}

	for _, lang := range languages {
		t.Run(lang.language, func(t *testing.T) {
			states := []struct {
				name         string
				runOnStartup bool
				launchHidden bool
				autoHide     bool
				want         string
			}{
				{name: "launch_only", runOnStartup: true, want: lang.launchOnly},
				{name: "close_window", runOnStartup: true, autoHide: true, want: lang.autoHide},
				{name: "hidden", runOnStartup: true, launchHidden: true, want: lang.hidden},
				{name: "hidden_overrides_close", runOnStartup: true, launchHidden: true, autoHide: true, want: lang.hidden},
				{name: "paused", want: lang.paused},
				{name: "paused_overrides_close", autoHide: true, want: lang.paused},
				{name: "paused_overrides_hidden", launchHidden: true, want: lang.paused},
				{name: "paused_overrides_both", launchHidden: true, autoHide: true, want: lang.paused},
			}
			for _, exePath := range []string{"test.exe", "test.bat"} {
				for _, state := range states {
					t.Run(exePath+"/"+state.name, func(t *testing.T) {
						app := config.ManagedAppEntry{
							Name:                     "Example app",
							ExePath:                  exePath,
							RunOnStartup:             state.runOnStartup,
							LaunchHiddenInBackground: state.launchHidden,
							TrayBehavior: config.TrayBehavior{
								AutoMinimizeAndHideOnLaunch: state.autoHide,
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
	if got := TranslateResultCode("en-US", "started_hidden"); got != "started hidden in background" {
		t.Fatalf("TranslateResultCode(en-US, started_hidden) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "started_hidden"); got != "已后台静默启动" {
		t.Fatalf("TranslateResultCode(zh-CN, started_hidden) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "managed"); got != "前台窗口已成功收起" {
		t.Fatalf("TranslateResultCode(zh-CN, managed) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "started_only"); got != "已启动 (无窗口规则)" {
		t.Fatalf("TranslateResultCode(zh-CN, started_only) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "already_running_skipped"); got != "程序已在运行，已跳过启动" {
		t.Fatalf("TranslateResultCode(zh-CN, already_running_skipped) = %q", got)
	}
	if got := TranslateResultCode("zh-CN", "no_window_managed"); got != "等待超时，未检测到程序窗口" {
		t.Fatalf("TranslateResultCode(zh-CN, no_window_managed) = %q", got)
	}
	if got := TranslateResultCode("en-US", "unknown"); got != "" {
		t.Fatalf("TranslateResultCode(unknown) = %q, want empty", got)
	}
}
