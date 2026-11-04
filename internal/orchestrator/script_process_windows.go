//go:build windows

package orchestrator

import (
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A script's process is its interpreter, not the script file. Match the script
// operand as well as the interpreter; matching python.exe/cmd.exe alone could
// reveal, hide or terminate an unrelated program using the same interpreter.
func scriptInterpreter(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".cmd":
		return "cmd.exe"
	case ".ps1":
		return "powershell.exe"
	case ".py":
		return "python.exe"
	case ".pyw":
		return "pythonw.exe"
	case ".ahk":
		return "autohotkey.exe"
	}
	return ""
}

func scriptProcessMatches(pid uint32, name, script string) bool {
	interpreter := strings.EqualFold(filepath.Base(name), scriptInterpreter(script))
	if isAutoHotkeyScript(script) {
		// Match the runtime, not the short-lived version-selecting UX launcher.
		switch strings.ToLower(filepath.Base(name)) {
		case "autohotkey.exe", "autohotkey32.exe", "autohotkey64.exe",
			"autohotkeya32.exe", "autohotkeyu32.exe", "autohotkeyu64.exe",
			"autohotkey32_uia.exe", "autohotkey64_uia.exe",
			"autohotkeyu32_uia.exe", "autohotkeyu64_uia.exe":
			interpreter = true
		}
	}
	return interpreter && scriptCommandMatches(processCommandLine(pid), script)
}

func scriptCommandMatches(line, script string) bool {
	args := parseArgs(line)
	if len(args) < 2 {
		return false
	}
	var operand string
	switch scriptInterpreter(script) {
	case "cmd.exe":
		// /s /c uses an extra outer quote pair, which CommandLineToArgvW
		// cannot interpret as cmd does. Read only the actual script operand.
		for rest := line; rest != ""; {
			var token string
			token, rest = commandToken(rest)
			if strings.EqualFold(token, "/c") || strings.EqualFold(token, "/k") {
				if strings.HasPrefix(rest, `""`) {
					rest = rest[1:]
				}
				operand, _ = commandToken(rest)
				break
			}
		}
	case "powershell.exe":
		for i := 1; i+1 < len(args); i++ {
			if strings.EqualFold(args[i], "-Command") || strings.EqualFold(args[i], "-EncodedCommand") {
				return false
			}
			if strings.EqualFold(args[i], "-File") {
				operand = args[i+1]
				break
			}
		}
	case "python.exe", "pythonw.exe":
		operand = args[1]
	case "autohotkey.exe":
		// Only the first script operand owns the process. A different script
		// may take the wanted path as data; never stop that unrelated script.
		for i := 1; i < len(args); i++ {
			arg := strings.ToLower(args[i])
			switch {
			case arg == "/include" || arg == "/ilib":
				i++ // these switches consume a file which is not the main script
			case arg == "/f" || arg == "/force" || arg == "/r" || arg == "/restart" || arg == "/script" || arg == "/validate",
				arg == "/errorstdout" || strings.HasPrefix(arg, "/errorstdout="),
				arg == "/debug" || strings.HasPrefix(arg, "/debug="), strings.HasPrefix(arg, "/cp"):
				continue
			case strings.HasPrefix(arg, "/"):
				return false
			default:
				operand = args[i]
			}
			if operand != "" {
				break
			}
		}
	}
	// Relative operands cannot be resolved against WinTray's working directory.
	// Our launcher passes absolute script paths, so fail closed for other forms.
	return filepath.IsAbs(operand) && strings.EqualFold(normalizePath(operand), normalizePath(script))
}

func commandToken(line string) (token, rest string) {
	line = strings.TrimSpace(line)
	quoted := false
	i := 0
	for ; i < len(line); i++ {
		if line[i] == '"' {
			quoted = !quoted
		} else if !quoted && (line[i] == ' ' || line[i] == '\t') {
			break
		}
	}
	if args := parseArgs(line[:i]); len(args) == 1 {
		token = args[0]
	}
	return token, strings.TrimSpace(line[i:])
}

// Query the command line through the kernel, without attaching to the target
// console or reading architecture-dependent PEB layouts. Windows 10/11 support
// this query with PROCESS_QUERY_LIMITED_INFORMATION, including 32-bit children.
func processCommandLine(pid uint32) string {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(process)
	var size uint32
	_ = windows.NtQueryInformationProcess(process, windows.ProcessCommandLineInformation, nil, 0, &size)
	if size < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) || size > 128*1024 {
		return ""
	}
	buffer := make([]byte, size)
	if windows.NtQueryInformationProcess(process, windows.ProcessCommandLineInformation, unsafe.Pointer(&buffer[0]), size, &size) != nil {
		return ""
	}
	text := (*windows.NTUnicodeString)(unsafe.Pointer(&buffer[0]))
	start := uintptr(unsafe.Pointer(&buffer[0]))
	data := uintptr(unsafe.Pointer(text.Buffer))
	if text.Length%2 != 0 || data < start || data-start > uintptr(len(buffer)) || uintptr(text.Length) > uintptr(len(buffer))-(data-start) {
		return ""
	}
	value := windows.UTF16ToString(unsafe.Slice(text.Buffer, int(text.Length)/2))
	runtime.KeepAlive(buffer)
	return value
}
