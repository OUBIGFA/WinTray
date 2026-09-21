import io

def patch(path, pairs):
    s = io.open(path, encoding='utf-8').read()
    for old, new in pairs:
        assert old in s, (path, old[:60])
        s = s.replace(old, new)
    io.open(path, 'w', encoding='utf-8', newline='\n').write(s)

# ---------- types.go ----------
patch('internal/orchestrator/types.go', [
('''	ResultManaged                 ResultCode = "managed"
	ResultManagedExisting         ResultCode = "managed_existing"
)''', '''	ResultManaged                 ResultCode = "managed"
	ResultManagedExisting         ResultCode = "managed_existing"
	ResultHiddenToTray            ResultCode = "hidden_to_tray"
)'''),
('''type Result struct {
	AppName string
	Managed bool
	Action  string
	Code    ResultCode
	Message string
}''', '''type Result struct {
	AppName string
	Managed bool
	Action  string
	Code    ResultCode
	Message string
	// Hidden is set when WinTray hid the window itself (SW_HIDE) instead of the
	// program closing it into its own tray icon. Such a program has no way back
	// to its window, so the caller must offer one (a hosted tray icon).
	Hidden *HiddenWindow
}

// HiddenWindow identifies a window WinTray keeps hidden on behalf of a program
// that has no tray icon of its own (console programs such as syncthing.exe).
// Handle may be 0 when the window could not be located yet; ProcessID always
// identifies the running program.
type HiddenWindow struct {
	Handle    uintptr
	ProcessID uint32
}

// managedWindow is the outcome of a successful window action.
type managedWindow struct {
	Handle    uintptr
	ProcessID uint32
	// Hidden reports that WinTray hid the window itself rather than the
	// program handling a close request.
	Hidden bool
}'''),
])

# ---------- runner.go ----------
patch('internal/orchestrator/runner.go', [
('''func startProcess(exePath, args string, hidden bool) (*exec.Cmd, error) {
	dir := filepath.Dir(exePath)
	cmd := buildLaunchCommand(exePath, args, hidden)
	if _, err := os.Stat(dir); err == nil {
		cmd.Dir = dir
	}
''', '''// launchMode selects how the new process gets (or does not get) a window.
type launchMode int

const (
	// launchVisible starts the program normally.
	launchVisible launchMode = iota
	// launchNoWindow starts the program without any console window
	// (CREATE_NO_WINDOW); used for scripts and "launch hidden" entries.
	launchNoWindow
	// launchHiddenConsole starts a console program with its console window
	// created hidden (SW_HIDE). The window still exists, so it can be shown
	// again later from a tray icon.
	launchHiddenConsole
)

func startProcess(exePath, args string, mode launchMode) (*exec.Cmd, error) {
	hidden := mode == launchNoWindow
	dir := filepath.Dir(exePath)
	cmd := buildLaunchCommand(exePath, args, hidden)
	if mode == launchHiddenConsole {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	if _, err := os.Stat(dir); err == nil {
		cmd.Dir = dir
	}
'''),
('''		if hidden {
			attr.CreationFlags = createNoWindow
			attr.HideWindow = true
		}
		shellCmd.SysProcAttr = attr''', '''		if hidden {
			attr.CreationFlags = createNoWindow
			attr.HideWindow = true
		}
		if mode == launchHiddenConsole {
			attr.HideWindow = true
		}
		shellCmd.SysProcAttr = attr'''),
])

# ---------- process_scan_windows.go ----------
patch('internal/orchestrator/process_scan_windows.go', [
('''func hasRunningProcessByIdentity(expectedPath, expectedName string) bool {
	expectedPath = normalizePath(expectedPath)
	targetIdentity := normalizeIdentity(expectedName)
	if expectedPath == "" && targetIdentity == "" {
		return false
	}

	hSnapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(hSnapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err = windows.Process32First(hSnapshot, &pe); err != nil {
		return false
	}

	for {
		if processIdentityMatches(pe.ProcessID, windows.UTF16ToString(pe.ExeFile[:]), expectedPath, targetIdentity) {
			return true
		}
		err = windows.Process32Next(hSnapshot, &pe)
		if err != nil {
			break
		}
	}

	return false
}''', '''func hasRunningProcessByIdentity(expectedPath, expectedName string) bool {
	return findRunningProcessByIdentity(expectedPath, expectedName) != 0
}

// findRunningProcessByIdentity returns the PID of the first process matching
// the executable identity, or 0 when none is running.
func findRunningProcessByIdentity(expectedPath, expectedName string) uint32 {
	expectedPath = normalizePath(expectedPath)
	targetIdentity := normalizeIdentity(expectedName)
	if expectedPath == "" && targetIdentity == "" {
		return 0
	}

	hSnapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(hSnapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err = windows.Process32First(hSnapshot, &pe); err != nil {
		return 0
	}

	for {
		if processIdentityMatches(pe.ProcessID, windows.UTF16ToString(pe.ExeFile[:]), expectedPath, targetIdentity) {
			return pe.ProcessID
		}
		err = windows.Process32Next(hSnapshot, &pe)
		if err != nil {
			break
		}
	}

	return 0
}'''),
])
print('ok')
