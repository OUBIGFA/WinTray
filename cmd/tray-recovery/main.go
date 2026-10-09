//go:build windows

// WinTray-Recovery is a pipe-driven companion, not a second application session.
package main

import (
	"fmt"
	"os"

	"wintray/internal/config"
	"wintray/internal/logging"
	"wintray/internal/traybox"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "--watch-tray" {
		os.Exit(2)
	}
	if err := traybox.RunRecovery(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		// The main process may already be gone. Preserve failures in the same
		// user log so an unavailable Explorer is not reported as a success.
		if dir, dirErr := config.AppDirWithError(); dirErr == nil {
			if logger, logErr := logging.New(dir); logErr == nil {
				logger.Warn(fmt.Sprintf("tray recovery: %v", err))
				logger.Close()
			}
		}
		os.Exit(1)
	}
}
