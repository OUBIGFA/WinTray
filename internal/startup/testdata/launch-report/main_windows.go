// launch-report is the inert target of startup integration tests. It records
// one launch and exits; it cannot register tasks or change startup settings.
package main

import (
	"encoding/json"
	"os"

	"golang.org/x/sys/windows"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "--wintray-startup-test-report" {
		os.Exit(2)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	var info windows.StartupInfo
	windows.GetStartupInfo(&info)
	report := struct {
		Args     []string `json:"args"`
		Cwd      string   `json:"cwd"`
		Elevated bool     `json:"elevated"`
		Show     uint16   `json:"show"`
		Flags    uint32   `json:"flags"`
	}{os.Args[3:], cwd, windows.GetCurrentProcessToken().IsElevated(), info.ShowWindow, info.Flags}
	data, err := json.Marshal(report)
	if err != nil || os.WriteFile(os.Args[2], data, 0o600) != nil {
		os.Exit(1)
	}
}
