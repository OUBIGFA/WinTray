package i18n

import (
	"fmt"
	"path/filepath"
	"strings"

	"wintray/internal/config"
)

type Lang string

const (
	LangZhCN Lang = "zh-CN"
	LangEnUS Lang = "en-US"
)

type Messages struct {
	WindowTitle                    string
	GlobalSettingsTitle            string
	RunAtLogon                     string
	StartHidden                    string
	ExitOnDone                     string
	RetrySeconds                   string
	LanguageLabel                  string
	ManagedListTitle               string
	ManagedColumnName              string
	ManagedColumnPath              string
	ManagedColumnRule              string
	ManagedEditorTitle             string
	ManagedAppPath                 string
	ManagedAppArgs                 string
	ManagedAutoHide                string
	ManagedLaunchHidden            string
	ManagedPauseTask               string
	ManagedLaunchNow               string
	ManagedLaunchNowBusy           string
	LaunchNowDoneBody              string
	AddProgram                     string
	RemoveSelected                 string
	OpenLogs                       string
	CleanupRestore                 string
	CheckUpdate                    string
	CheckUpdateBusy                string
	GitHubTooltip                  string
	VersionLabel                   string
	UpdateTitle                    string
	UpdateAvailableBody            string
	UpdateLatestBody               string
	UpdateFailedBody               string
	ExitApp                        string
	TrayOpenSettings               string
	TrayExit                       string
	TrayToolTip                    string
	SelectManagedExe               string
	ExeFilter                      string
	AllFilesFilter                 string
	NewAppName                     string
	ManagedListItemTemplate        string
	ManagedListHiddenTemplate      string
	ManagedListParamTemplate       string
	ManagedListParamHiddenTemplate string
	ManagedListParamPausedTemplate string
	RunSummaryNone                 string
	RunSummaryLine                 string
	FatalStartupTitle              string
	FatalStartupBodyTemplate       string
	AlreadyRunningTitle            string
	AlreadyRunningBody             string
	StatusLaunchFailTemplate       string
	StatusRetryExhausted           string
	StatusPermissionHint           string
	StatusOpenLogsFailed           string
	CleanupConfirmTitle            string
	CleanupConfirmBody             string
	CleanupDoneTitle               string
	CleanupDoneBody                string
	CleanupFailedTitle             string
	CleanupFailedBody              string
	LanguageZhLabel                string
	LanguageEnLabel                string
}

var zhCN = Messages{
	WindowTitle:                    "WinTray",
	GlobalSettingsTitle:            "全局设置",
	RunAtLogon:                     "WinTray 开机启动",
	StartHidden:                    "启动后最小化到托盘",
	ExitOnDone:                     "完成所有任务后自动退出",
	RetrySeconds:                   "窗口重试秒数 (0-120):",
	LanguageLabel:                  "语言:",
	ManagedListTitle:               "程序列表",
	ManagedColumnName:              "程序",
	ManagedColumnPath:              "路径",
	ManagedColumnRule:              "规则",
	ManagedEditorTitle:             "程序设置",
	ManagedAppPath:                 "程序路径:",
	ManagedAppArgs:                 "启动参数:",
	ManagedAutoHide:                "启动后关闭窗口",
	ManagedLaunchHidden:            "隐藏后台启动",
	ManagedPauseTask:               "暂停任务",
	ManagedLaunchNow:               "马上启动",
	ManagedLaunchNowBusy:           "启动中…",
	LaunchNowDoneBody:              "已启动: %s",
	AddProgram:                     "添加程序",
	RemoveSelected:                 "删除选中",
	OpenLogs:                       "打开日志",
	CleanupRestore:                 "清理并恢复默认",
	CheckUpdate:                    "检查更新",
	CheckUpdateBusy:                "检查中…",
	GitHubTooltip:                  "在 GitHub 上查看项目",
	VersionLabel:                   "版本 %s",
	UpdateTitle:                    "检查更新",
	UpdateAvailableBody:            "发现新版本 %s（当前 %s）。\r\n\r\n是否前往下载页面？",
	UpdateLatestBody:               "当前已是最新版本 %s。",
	UpdateFailedBody:               "检查更新失败: %s",
	ExitApp:                        "退出 WinTray",
	TrayOpenSettings:               "打开设置",
	TrayExit:                       "退出 WinTray",
	TrayToolTip:                    "WinTray",
	SelectManagedExe:               "选择要托管的程序",
	ExeFilter:                      "程序文件 (*.exe;*.cmd;*.bat;*.ps1;*.py)|*.exe;*.cmd;*.bat;*.ps1;*.py",
	AllFilesFilter:                 "所有文件 (*.*)|*.*",
	NewAppName:                     "新程序",
	ManagedListItemTemplate:        "%s | %s | 启动后关闭窗口=%t",
	ManagedListHiddenTemplate:      "%s | %s | 隐藏后台启动=%t",
	ManagedListParamTemplate:       "关闭窗口=%t",
	ManagedListParamHiddenTemplate: "隐藏后台=%t",
	ManagedListParamPausedTemplate: "已暂停",
	RunSummaryNone:                 "没有可执行的受管任务。",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray 启动失败",
	FatalStartupBodyTemplate:       "%s\n\n日志: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray 已在运行。",
	StatusLaunchFailTemplate:       "启动失败: %s (%s)",
	StatusRetryExhausted:           "重试超时，未找到可托管窗口",
	StatusPermissionHint:           "可能是权限限制 (UIPI): 请尝试以管理员身份运行 WinTray。",
	StatusOpenLogsFailed:           "打开日志失败",
	CleanupConfirmTitle:            "清理并恢复默认",
	CleanupConfirmBody:             "将清除 WinTray 的本地配置与日志，并恢复默认设置。\r\n\r\n是否继续？",
	CleanupDoneTitle:               "已计划清理",
	CleanupDoneBody:                "已恢复默认设置，WinTray 将在退出后清理本地数据。",
	CleanupFailedTitle:             "清理失败",
	CleanupFailedBody:              "清理并恢复默认失败: %s",
	LanguageZhLabel:                "中文",
	LanguageEnLabel:                "English",
}

var enUS = Messages{
	WindowTitle:                    "WinTray",
	GlobalSettingsTitle:            "Global Settings",
	RunAtLogon:                     "Run WinTray at logon",
	StartHidden:                    "Minimize to tray after launch",
	ExitOnDone:                     "Exit automatically after all tasks complete",
	RetrySeconds:                   "Window retry seconds (0-120):",
	LanguageLabel:                  "Language:",
	ManagedListTitle:               "Program List",
	ManagedColumnName:              "Program",
	ManagedColumnPath:              "Path",
	ManagedColumnRule:              "Rule",
	ManagedEditorTitle:             "Program Settings",
	ManagedAppPath:                 "Program path:",
	ManagedAppArgs:                 "Launch arguments:",
	ManagedAutoHide:                "Close window after launch",
	ManagedLaunchHidden:            "Launch hidden in background",
	ManagedPauseTask:               "Pause task",
	ManagedLaunchNow:               "Launch Now",
	ManagedLaunchNowBusy:           "Starting…",
	LaunchNowDoneBody:              "Started: %s",
	AddProgram:                     "Add Program",
	RemoveSelected:                 "Remove Selected",
	OpenLogs:                       "Open Logs",
	CleanupRestore:                 "Cleanup && Restore Defaults",
	CheckUpdate:                    "Check for Updates",
	CheckUpdateBusy:                "Checking…",
	GitHubTooltip:                  "View the project on GitHub",
	VersionLabel:                   "Version %s",
	UpdateTitle:                    "Check for Updates",
	UpdateAvailableBody:            "Version %s is available (current %s).\r\n\r\nOpen the download page?",
	UpdateLatestBody:               "You are on the latest version %s.",
	UpdateFailedBody:               "Update check failed: %s",
	ExitApp:                        "Exit WinTray",
	TrayOpenSettings:               "Open Settings",
	TrayExit:                       "Exit WinTray",
	TrayToolTip:                    "WinTray",
	SelectManagedExe:               "Select program to manage",
	ExeFilter:                      "Program files (*.exe;*.cmd;*.bat;*.ps1;*.py)|*.exe;*.cmd;*.bat;*.ps1;*.py",
	AllFilesFilter:                 "All Files (*.*)|*.*",
	NewAppName:                     "New App",
	ManagedListItemTemplate:        "%s | %s | CloseAfterLaunch=%t",
	ManagedListHiddenTemplate:      "%s | %s | LaunchHidden=%t",
	ManagedListParamTemplate:       "CloseAfterLaunch=%t",
	ManagedListParamHiddenTemplate: "LaunchHidden=%t",
	ManagedListParamPausedTemplate: "Paused",
	RunSummaryNone:                 "No managed tasks to run.",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray startup failed",
	FatalStartupBodyTemplate:       "%s\n\nLog: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray is already running.",
	StatusLaunchFailTemplate:       "Launch failed: %s (%s)",
	StatusRetryExhausted:           "Retry exhausted, no manageable window found",
	StatusPermissionHint:           "Possible UIPI permission limitation: try running WinTray as administrator.",
	StatusOpenLogsFailed:           "Failed to open logs",
	CleanupConfirmTitle:            "Cleanup && Restore Defaults",
	CleanupConfirmBody:             "This will clear WinTray local settings and logs, then restore defaults.\r\n\r\nContinue?",
	CleanupDoneTitle:               "Cleanup Scheduled",
	CleanupDoneBody:                "Default settings restored. WinTray data will be cleaned after exit.",
	CleanupFailedTitle:             "Cleanup Failed",
	CleanupFailedBody:              "Cleanup and restore failed: %s",
	LanguageZhLabel:                "中文",
	LanguageEnLabel:                "English",
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
	if !app.RunOnStartup {
		return fmt.Sprintf("%s | %s | %s", app.Name, app.ExePath, msg.ManagedListParamPausedTemplate)
	}
	if strings.ToLower(filepath.Ext(app.ExePath)) != ".exe" {
		return fmt.Sprintf(msg.ManagedListHiddenTemplate, app.Name, app.ExePath, app.LaunchHiddenInBackground)
	}
	return fmt.Sprintf(msg.ManagedListItemTemplate, app.Name, app.ExePath, app.TrayBehavior.AutoMinimizeAndHideOnLaunch)
}

func FormatManagedParam(language string, app config.ManagedAppEntry) string {
	msg := For(language)
	if !app.RunOnStartup {
		return msg.ManagedListParamPausedTemplate
	}
	if strings.ToLower(filepath.Ext(app.ExePath)) != ".exe" {
		return fmt.Sprintf(msg.ManagedListParamHiddenTemplate, app.LaunchHiddenInBackground)
	}
	return fmt.Sprintf(msg.ManagedListParamTemplate, app.TrayBehavior.AutoMinimizeAndHideOnLaunch)
}

func IsLikelyPermissionIssue(message string) bool {
	return message == "no window managed" || message == "no existing window managed"
}

func IsLikelyPermissionCode(code string) bool {
	return code == "no_window_managed" || code == "no_existing_window_managed"
}

func TranslateResultCode(language, code string) string {
	messages := map[string]string{
		"empty_exe_path":             "empty exe path",
		"invalid_exe_path":           "invalid exe path",
		"process_start_failed":       "process start failed",
		"started_only":               "started only",
		"started_hidden":             "started hidden",
		"already_running_skipped":    "already running skipped",
		"already_running_managed":    "already running managed existing",
		"no_window_managed":          "no window managed",
		"invalid_process_name":       "invalid process name",
		"no_existing_window_managed": "no existing window managed",
		"managed":                    "managed",
		"managed_existing":           "managed existing",
	}
	message, ok := messages[code]
	if !ok {
		return ""
	}
	return TranslateResultMessage(language, message)
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
		return "仅启动，未执行托管动作"
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
		return "前台窗口已关闭"
	case "invalid process name":
		if Resolve(language) == LangEnUS {
			return "invalid process name"
		}
		return "进程名无效"
	default:
		return message
	}
}
