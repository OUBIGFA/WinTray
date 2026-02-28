package orchestrator

import "wintray/internal/config"

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
	MinimizeWindow(hwnd uintptr) (bool, error)
}

type Logger interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

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
	Message string
}

type MatchCandidate struct {
	Window ManagedWindowInfo
	Score  int
}

func matchStrategy(window ManagedWindowInfo, strategy config.MatchStrategy) bool {
	hasTitle := window.Title != ""
	hasClass := window.ClassName != ""

	switch strategy {
	case config.MatchAny,
		"":
		return true
	case config.MatchProcessNameThenTitle:
		return true
	case config.MatchTitleContains:
		return hasTitle
	case config.MatchClassName:
		return hasClass
	default:
		return hasTitle || hasClass
	}
}
