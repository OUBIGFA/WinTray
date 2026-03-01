package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

func startProcess(exePath, args string, hidden bool) (*exec.Cmd, error) {
	dir := filepath.Dir(exePath)
	cmd := buildLaunchCommand(exePath, args, hidden)
	if _, err := os.Stat(dir); err == nil {
		cmd.Dir = dir
	}

	startErr := cmd.Start()
	if startErr == nil {
		return cmd, nil
	}

	if runtime.GOOS == "windows" {
		// Retry with cmd.exe /c start "" as a last resort (handles shell-associated executables)
		cleanPath := strings.Trim(strings.TrimSpace(exePath), "\"")
		commandLine := fmt.Sprintf("cmd.exe /c start \"\" \"%s\"", cleanPath)
		if trimmedArgs := strings.TrimSpace(args); trimmedArgs != "" {
			commandLine += " " + trimmedArgs
		}
		shellCmd := exec.Command("cmd.exe")
		attr := &syscall.SysProcAttr{CmdLine: commandLine}
		if hidden {
			attr.CreationFlags = createNoWindow
			attr.HideWindow = true
		}
		shellCmd.SysProcAttr = attr
		if _, err := os.Stat(dir); err == nil {
			shellCmd.Dir = dir
		}
		if shellErr := shellCmd.Start(); shellErr == nil {
			return shellCmd, nil
		}
	}

	return nil, startErr
}

const createNoWindow = 0x08000000

func buildLaunchCommand(exePath, args string, hidden bool) *exec.Cmd {
	trimmedArgs := strings.TrimSpace(args)
	if runtime.GOOS == "windows" {
		needsShell := isCmdScript(exePath) || trimmedArgs != ""
		if hidden || needsShell {
			cleanPath := strings.Trim(strings.TrimSpace(exePath), "\"")
			commandLine := fmt.Sprintf("cmd.exe /c \"%s\"", cleanPath)
			if trimmedArgs != "" {
				commandLine = commandLine + " " + trimmedArgs
			}
			cmd := exec.Command("cmd.exe")
			attr := &syscall.SysProcAttr{CmdLine: commandLine}
			if hidden {
				attr.CreationFlags = createNoWindow
				attr.HideWindow = true
			}
			cmd.SysProcAttr = attr
			return cmd
		}
	}
	if trimmedArgs == "" {
		return exec.Command(exePath)
	}
	return exec.Command(exePath, parseArgs(trimmedArgs)...)
}

func isCmdScript(exePath string) bool {
	ext := strings.ToLower(filepath.Ext(exePath))
	return ext == ".bat" || ext == ".cmd"
}

// parseArgs splits an argument string respecting double-quoted segments.
// e.g. `--config "C:\My Path\cfg.json" --verbose` → ["--config", "C:\My Path\cfg.json", "--verbose"]
func parseArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' || ch == '\t':
			if inQuote {
				current.WriteByte(ch)
			} else if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
