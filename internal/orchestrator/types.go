package orchestrator

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
	enumerator WindowEnumerator
	manager    WindowManager
	logger     Logger
}

func NewService(enumerator WindowEnumerator, manager WindowManager, logger Logger) *Service {
	return &Service{enumerator: enumerator, manager: manager, logger: logger}
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
