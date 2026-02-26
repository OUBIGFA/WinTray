package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func startProcess(exePath string) (*exec.Cmd, error) {
	cmd := exec.Command(exePath)
	dir := filepath.Dir(exePath)
	if _, err := os.Stat(dir); err == nil {
		cmd.Dir = dir
	}
	startErr := cmd.Start()
	if startErr == nil {
		return cmd, nil
	}
	if runtime.GOOS == "windows" {
		shellCmd := exec.Command("cmd", "/c", "start", "", exePath)
		if _, err := os.Stat(dir); err == nil {
			shellCmd.Dir = dir
		}
		if shellErr := shellCmd.Start(); shellErr == nil {
			return shellCmd, nil
		}
	}
	return nil, startErr
}
