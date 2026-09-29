package orchestrator

import (
	"path/filepath"
	"time"

	"wintray/internal/config"
	"wintray/internal/startup"
)

type ManagedWindowInfo struct {
	Handle       uintptr
	ProcessID    uint32
	ProcessName  string
	ProcessPath  string
	Title        string
	ClassName    string
	IsVisible    bool
	IsMinimized  bool
	IsForeground bool
	OwnerHandle  uintptr
	IsToolWindow bool
}

type WindowEnumerator interface {
	EnumerateTopLevelWindows() []ManagedWindowInfo
}

type WindowManager interface {
	CloseWindow(hwnd uintptr) (bool, error)
	HideWindow(hwnd uintptr) (bool, error)
}

type Logger interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

type ResultCode string

const (
	ResultEmptyExePath            ResultCode = "empty_exe_path"
	ResultInvalidExePath          ResultCode = "invalid_exe_path"
	ResultProcessStartFailed      ResultCode = "process_start_failed"
	ResultStartupCheckFailed      ResultCode = "startup_check_failed"
	ResultFrequencyLimit          ResultCode = "startup_frequency_limit"
	ResultFrequencyCheckFailed    ResultCode = "startup_frequency_check_failed"
	ResultExternalStartupTimeout  ResultCode = "external_startup_timeout"
	ResultCancelled               ResultCode = "cancelled"
	ResultAlreadyRunningManaged   ResultCode = "already_running_managed"
	ResultAlreadyRunningSkipped   ResultCode = "already_running_skipped"
	ResultNoWindowManaged         ResultCode = "no_window_managed"
	ResultStartedHidden           ResultCode = "started_hidden"
	ResultStartedOnly             ResultCode = "started_only"
	ResultInvalidProcessName      ResultCode = "invalid_process_name"
	ResultNoExistingWindowManaged ResultCode = "no_existing_window_managed"
	ResultManaged                 ResultCode = "managed"
	ResultManagedExisting         ResultCode = "managed_existing"
	ResultHiddenToTray            ResultCode = "hidden_to_tray"
)

type Service struct {
	visibility     *startupVisibility
	enumerator     WindowEnumerator
	manager        WindowManager
	logger         Logger
	startupHistory *config.StartupHistory
	// Instance-local probes keep tests isolated from the user's startup setup.
	externalStartupLookup func(string) (string, error)
	externalStartupWait   time.Duration
	logonTaskLaunch       func(config.ManagedAppEntry) error
	logonTaskLaunchWait   time.Duration
	// Process termination is best effort; the run-limit worker must separately
	// observe survivors before allowing the session to exit.
	terminateProcesses func([]uint32) int
}

func NewService(enumerator WindowEnumerator, manager WindowManager, logger Logger) *Service {
	var history *config.StartupHistory
	if dir, err := config.AppDirWithError(); err == nil {
		history = config.NewStartupHistory(filepath.Join(dir, "startup-history.json"))
	}
	return &Service{
		enumerator: enumerator, manager: manager, logger: logger,
		startupHistory:        history,
		externalStartupLookup: startup.NewExternalStartupLookup(),
		logonTaskLaunch:       startup.LaunchAppTaskNow,
		logonTaskLaunchWait:   30 * time.Second,
		terminateProcesses:    terminateProcessTrees,
		// WinTray's logon task runs before Explorer works through its Run
		// queue one entry at a time, so a late entry may take minutes.
		externalStartupWait: 300 * time.Second,
	}
}

type Result struct {
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
}

type MatchCandidate struct {
	Window ManagedWindowInfo
	Score  int
}
