//go:build windows

package app

import (
	"fmt"
	"strings"

	"wintray/internal/i18n"
	"wintray/internal/startup"
)

// formatAppTaskError renders a program task sync error for the user. Errors of
// several programs arrive joined and wrapped with the program's name; each
// original startup command that WinTray refused is shown in the UI language,
// in place, so every program keeps its own line and prefix.
func formatAppTaskError(language string, err error) string {
	text := err.Error()
	msg := i18n.For(language)
	for _, refused := range startupCommandErrors(err) {
		template := msg.StartupCommandIndirect
		if refused.Unquoted {
			template = msg.StartupCommandUnquoted
		}
		text = strings.ReplaceAll(text, refused.Error(), fmt.Sprintf(template, refused.Source))
	}
	return text
}

func startupCommandErrors(err error) []*startup.StartupCommandError {
	switch e := err.(type) {
	case *startup.StartupCommandError:
		return []*startup.StartupCommandError{e}
	case interface{ Unwrap() []error }:
		var all []*startup.StartupCommandError
		for _, child := range e.Unwrap() {
			all = append(all, startupCommandErrors(child)...)
		}
		return all
	case interface{ Unwrap() error }:
		return startupCommandErrors(e.Unwrap())
	}
	return nil
}
