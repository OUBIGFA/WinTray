package i18n

import (
	"fmt"

	"wintray/internal/config"
)

type Lang string

const (
	LangZhCN Lang = "zh-CN"
	LangEnUS Lang = "en-US"
)

type Messages struct {
	WindowTitle              string
	RunAtLogon               string
	StartHidden              string
	ExitOnDone               string
	RetrySeconds             string
	LanguageLabel            string
	ManagedListTitle         string
	ManagedEditorTitle       string
	ManagedAppPath           string
	ManagedAppArgs           string
	SelectProgram            string
	ManagedAutoHide          string
	ManagedLaunchHidden      string
	ManagedNoSelectionHint   string
	AddProgram               string
	RemoveSelected           string
	OpenLogs                 string
	CleanupRestore           string
	ExitApp                  string
	TrayOpenSettings         string
	TrayOpenLogs             string
	TrayCleanupRestore       string
	TrayExit                 string
	TrayToolTip              string
	SelectManagedExe         string
	ExeFilter                string
	AllFilesFilter           string
	NewAppName               string
	ManagedListItemTemplate  string
	RunSummaryTitle          string
	RunSummaryNone           string
	RunSummaryLine           string
	RunSummaryHeader         string
	FatalStartupTitle        string
	FatalStartupBodyTemplate string
	AlreadyRunningTitle      string
	AlreadyRunningBody       string
	StatusLaunchFailTemplate string
	StatusManageFailTemplate string
	StatusManageOkTemplate   string
	StatusNoTasks            string
	StatusRetryExhausted     string
	StatusPermissionHint     string
	StatusOpenLogsFailed     string
	CleanupConfirmTitle      string
	CleanupConfirmBody       string
	CleanupDoneTitle         string
	CleanupDoneBody          string
	CleanupFailedTitle       string
	CleanupFailedBody        string
	LanguageZhLabel          string
	LanguageEnLabel          string
}

var zhCN = Messages{
	WindowTitle:              "WinTray",
	RunAtLogon:               "WinTray 开机启动",
	StartHidden:              "启动后最小化到托盘",
	ExitOnDone:               "完成所有任务后自行退出",
	RetrySeconds:             "窗口重试秒数 (0-120):",
	LanguageLabel:            "语言：",
	ManagedListTitle:         "受管程序列表（开机时按配置自动处理前台窗口）",
	ManagedEditorTitle:       "程序设置",
	ManagedAppPath:           "程序路径：",
	ManagedAppArgs:           "启动参数（可选）：",
	SelectProgram:            "选择程序",
	ManagedAutoHide:          "启动后关闭界面",
	ManagedLaunchHidden:      "隐藏后台启动（适用于 cmd/bat）",
	ManagedNoSelectionHint:   "请选择一个程序进行编辑，或点击“选择程序”新增。",
	AddProgram:               "添加程序",
	RemoveSelected:           "删除选中",
	OpenLogs:                 "打开日志",
	CleanupRestore:           "清理并恢复默认",
	ExitApp:                  "退出 WinTray",
	TrayOpenSettings:         "打开设置",
	TrayOpenLogs:             "打开日志",
	TrayCleanupRestore:       "清理并恢复默认",
	TrayExit:                 "退出 WinTray",
	TrayToolTip:              "WinTray",
	SelectManagedExe:         "选择要托管的 EXE",
	ExeFilter:                "可执行文件 (*.exe)|*.exe",
	AllFilesFilter:           "所有文件 (*.*)|*.*",
	NewAppName:               "新程序",
	ManagedListItemTemplate:  "%s | %s | 启动后关闭界面=%t",
	RunSummaryTitle:          "受管任务结果",
	RunSummaryNone:           "没有可执行的受管任务。",
	RunSummaryLine:           "%s：%s",
	RunSummaryHeader:         "执行完成：",
	FatalStartupTitle:        "WinTray 启动失败",
	FatalStartupBodyTemplate: "%s\n\n日志：%s",
	AlreadyRunningTitle:      "WinTray",
	AlreadyRunningBody:       "WinTray 已在运行。",
	StatusLaunchFailTemplate: "启动失败：%s (%s)",
	StatusManageFailTemplate: "托管失败：%s (%s)",
	StatusManageOkTemplate:   "托管成功：%s",
	StatusNoTasks:            "没有受管任务。",
	StatusRetryExhausted:     "重试超时，未找到可托管窗口",
	StatusPermissionHint:     "可能是权限限制（UIPI）：请尝试以管理员身份运行 WinTray。",
	StatusOpenLogsFailed:     "打开日志失败",
	CleanupConfirmTitle:      "清理并恢复默认",
	CleanupConfirmBody:       "将清除 WinTray 的本地配置与日志，并恢复默认设置。\r\n\r\n是否继续？",
	CleanupDoneTitle:         "已计划清理",
	CleanupDoneBody:          "已恢复默认设置，WinTray 将在退出后清理本地数据。",
	CleanupFailedTitle:       "清理失败",
	CleanupFailedBody:        "清理并恢复默认失败：%s",
	LanguageZhLabel:          "中文",
	LanguageEnLabel:          "English",
}

var enUS = Messages{
	WindowTitle:              "WinTray",
	RunAtLogon:               "Run WinTray at logon",
	StartHidden:              "Minimize to tray after launch",
	ExitOnDone:               "Exit automatically after all tasks complete",
	RetrySeconds:             "Window retry seconds (0-120):",
	LanguageLabel:            "Language:",
	ManagedListTitle:         "Managed apps (apply window handling at startup)",
	ManagedEditorTitle:       "Program Settings",
	ManagedAppPath:           "Executable path:",
	ManagedAppArgs:           "Launch arguments (optional):",
	SelectProgram:            "Select Program",
	ManagedAutoHide:          "Close window after launch",
	ManagedLaunchHidden:      "Launch hidden in background (for cmd/bat)",
	ManagedNoSelectionHint:   "Select a program to edit, or click Select Program to add one.",
	AddProgram:               "Add Program",
	RemoveSelected:           "Remove Selected",
	OpenLogs:                 "Open Logs",
	CleanupRestore:           "Cleanup && Restore Defaults",
	ExitApp:                  "Exit WinTray",
	TrayOpenSettings:         "Open Settings",
	TrayOpenLogs:             "Open Logs",
	TrayCleanupRestore:       "Cleanup && Restore Defaults",
	TrayExit:                 "Exit WinTray",
	TrayToolTip:              "WinTray",
	SelectManagedExe:         "Select EXE to manage",
	ExeFilter:                "Executable (*.exe)|*.exe",
	AllFilesFilter:           "All Files (*.*)|*.*",
	NewAppName:               "New App",
	ManagedListItemTemplate:  "%s | %s | CloseAfterLaunch=%t",
	RunSummaryTitle:          "Managed Task Results",
	RunSummaryNone:           "No managed tasks to run.",
	RunSummaryLine:           "%s: %s",
	RunSummaryHeader:         "Completed:",
	FatalStartupTitle:        "WinTray startup failed",
	FatalStartupBodyTemplate: "%s\n\nLog: %s",
	AlreadyRunningTitle:      "WinTray",
	AlreadyRunningBody:       "WinTray is already running.",
	StatusLaunchFailTemplate: "Launch failed: %s (%s)",
	StatusManageFailTemplate: "Manage failed: %s (%s)",
	StatusManageOkTemplate:   "Managed: %s",
	StatusNoTasks:            "No managed tasks.",
	StatusRetryExhausted:     "Retry exhausted, no manageable window found",
	StatusPermissionHint:     "Possible UIPI permission limitation: try running WinTray as administrator.",
	StatusOpenLogsFailed:     "Failed to open logs",
	CleanupConfirmTitle:      "Cleanup && Restore Defaults",
	CleanupConfirmBody:       "This will clear WinTray local settings and logs, then restore defaults.\r\n\r\nContinue?",
	CleanupDoneTitle:         "Cleanup Scheduled",
	CleanupDoneBody:          "Default settings restored. WinTray data will be cleaned after exit.",
	CleanupFailedTitle:       "Cleanup Failed",
	CleanupFailedBody:        "Cleanup and restore failed: %s",
	LanguageZhLabel:          "中文",
	LanguageEnLabel:          "English",
}

func Resolve(language string) Lang {
	if language == string(LangEnUS) {
		return LangEnUS
	}
	return LangZhCN
}

func For(language string) Messages {
	if Resolve(language) == LangEnUS {
		return enUS
	}
	return zhCN
}

func LanguageOptions() []string {
	return []string{string(LangZhCN), string(LangEnUS)}
}

func FormatManagedListItem(language string, app config.ManagedAppEntry) string {
	msg := For(language)
	return fmt.Sprintf(msg.ManagedListItemTemplate, app.Name, app.ExePath, app.TrayBehavior.AutoMinimizeAndHideOnLaunch)
}

func IsLikelyPermissionIssue(message string) bool {
	return message == "no window managed" || message == "no existing window managed"
}

func TranslateResultMessage(language, message string) string {
	msg := For(language)
	switch message {
	case "empty exe path":
		if Resolve(language) == LangEnUS {
			return "empty executable path"
		}
		return "可执行路径为空"
	case "invalid exe path":
		if Resolve(language) == LangEnUS {
			return "invalid executable path"
		}
		return "可执行路径无效"
	case "process start failed":
		if Resolve(language) == LangEnUS {
			return "process start failed"
		}
		return "启动进程失败"
	case "started only":
		if Resolve(language) == LangEnUS {
			return "started only"
		}
		return "仅启动（未执行托管动作）"
	case "started hidden":
		if Resolve(language) == LangEnUS {
			return "started hidden in background"
		}
		return "已隐藏后台启动"
	case "already running skipped":
		if Resolve(language) == LangEnUS {
			return "already running, skipped relaunch"
		}
		return "程序已在运行，已跳过重复拉起"
	case "already running managed existing":
		if Resolve(language) == LangEnUS {
			return "already running, managed existing window"
		}
		return "程序已在运行，已处理现有窗口"
	case "no window managed", "no existing window managed":
		return msg.StatusRetryExhausted
	case "managed", "managed existing":
		if Resolve(language) == LangEnUS {
			return "front window closed"
		}
		return "前台界面已关闭"
	case "invalid process name":
		if Resolve(language) == LangEnUS {
			return "invalid process name"
		}
		return "进程名无效"
	default:
		return message
	}
}
