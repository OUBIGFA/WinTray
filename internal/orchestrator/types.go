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
}

type MatchCandidate struct {
	Window ManagedWindowInfo
	Score  int
}
