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
