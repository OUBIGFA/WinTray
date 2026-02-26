package app

import "strings"

func isBackgroundLaunch(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--background") {
			return true
		}
	}
	return false
}

func isAutorunLaunch(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--autorun") {
			return true
		}
	}
	return false
}

func shouldShowMainWindow(args []string) bool {
	return !isBackgroundLaunch(args)
}

func shouldSignalRunningInstance(args []string) bool {
	return !isBackgroundLaunch(args)
}
