//go:build windows

package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"wintray/internal/i18n"
	"wintray/internal/startup"
)

func TestFormatAppTaskErrorLocalizesEveryRefusedCommand(t *testing.T) {
	idm := &startup.StartupCommandError{Source: `HKCU\Run\IDMan`, Unquoted: true}
	filen := &startup.StartupCommandError{Source: `HKCU\Run\io.filen.desktop`, Unquoted: true}
	wrapper := &startup.StartupCommandError{Source: `HKCU\Run\Tool`}
	// Shaped like AppTasks.Sync: per-program wrap, a failed release joined to
	// the cause, and all programs joined.
	err := errors.Join(
		fmt.Errorf("%s: %w", "IDMan", errors.Join(idm, errors.New("restore IDMan: access denied"))),
		fmt.Errorf("%s: %w", "Filen", filen),
		fmt.Errorf("%s: %w", "Tool", wrapper),
		errors.New("Other: no supported original startup entry found"),
	)
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			msg := i18n.For(language)
			got := formatAppTaskError(language, err)
			for _, want := range []string{
				"IDMan: " + fmt.Sprintf(msg.StartupCommandUnquoted, idm.Source),
				"restore IDMan: access denied",
				"Filen: " + fmt.Sprintf(msg.StartupCommandUnquoted, filen.Source),
				"Tool: " + fmt.Sprintf(msg.StartupCommandIndirect, wrapper.Source),
				"Other: no supported original startup entry found",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
			if lines := strings.Count(got, "\n") + 1; lines != 5 {
				t.Errorf("%d lines, want one per error:\n%s", lines, got)
			}
			if strings.Contains(got, "select its original launcher") {
				t.Errorf("unactionable advice left in:\n%s", got)
			}
		})
	}
	if zh := formatAppTaskError("zh-CN", idm); !strings.Contains(zh, "加上引号") {
		t.Errorf("Chinese message does not tell the user to quote the path: %s", zh)
	}
}
