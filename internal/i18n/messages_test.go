package i18n

import (
	"testing"

	"wintray/internal/config"
)

func TestFormatManagedParam_ShowsPausedState(t *testing.T) {
	app := config.ManagedAppEntry{
		RunOnStartup:             false,
		LaunchHiddenInBackground: true,
	}

	if got := FormatManagedParam("en-US", app); got != "Paused" {
		t.Fatalf("FormatManagedParam(en-US) = %q, want %q", got, "Paused")
	}

	if got := FormatManagedParam("zh-CN", app); got != "已暂停" {
		t.Fatalf("FormatManagedParam(zh-CN) = %q, want %q", got, "已暂停")
	}
}

func TestFormatManagedParam_ShowsHiddenState(t *testing.T) {
	app := config.ManagedAppEntry{
		ExePath:                  "test.bat",
		RunOnStartup:             true,
		LaunchHiddenInBackground: true,
	}

	if got := FormatManagedParam("en-US", app); got != "LaunchHidden=true" {
		t.Fatalf("FormatManagedParam(en-US) = %q, want %q", got, "LaunchHidden=true")
	}

	if got := FormatManagedParam("zh-CN", app); got != "静默启动=true" {
		t.Fatalf("FormatManagedParam(zh-CN) = %q, want %q", got, "静默启动=true")
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
