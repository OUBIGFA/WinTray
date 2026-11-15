package i18n

import (
	"testing"

	"wintray/internal/config"
)

func TestStartupSummaryUsesModeInsteadOfRemovedSwitch(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		messages := For(language)
		for _, scenario := range []struct {
			entry config.ManagedAppEntry
			want  string
		}{
			{entry: config.ManagedAppEntry{}, want: messages.ManagedLaunchOnly},
			{entry: config.ManagedAppEntry{LaunchHiddenInBackground: true}, want: messages.ManagedLaunchHidden},
			{entry: config.ManagedAppEntry{LaunchViaLogonTask: true}, want: messages.ManagedTaskLaunch},
			{entry: config.ManagedAppEntry{TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}}, want: messages.ManagedAutoHide},
		} {
			if got := FormatManagedParam(language, scenario.entry); got != scenario.want {
				t.Errorf("%s startup summary = %q, want %q", language, got, scenario.want)
			}
		}
	}
}
