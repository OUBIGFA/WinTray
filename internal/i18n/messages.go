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
	WindowTitle                    string
	WindowSubtitle                 string
	GlobalSettingsTitle            string
	RunAtLogon                     string
	StartHidden                    string
	ExitOnDone                     string
	RetrySeconds                   string
	RetrySecondsInvalid            string
	LanguageLabel                  string
	ManagedListTitle               string
	ManagedListCount               string
	ManagedListEmpty               string
	ManagedListEmptyHint           string
	ManagedColumnName              string
	ManagedColumnPath              string
	ManagedColumnRule              string
	ManagedEditorTitle             string
	ManagedEditorHint              string
	ManagedSelectedTitle           string
	ManagedSelectionHint           string
	ManagedAppPath                 string
	BrowseProgram                  string
	ManagedAppArgs                 string
	ManagedArgsPlaceholder         string
	ManagedLaunchOnly              string
	ManagedAutoHide                string
	ManagedAutoHideHint            string
	ManagedLaunchHidden            string
	ManagedLaunchHiddenHint        string
	ManagedPauseTask               string
	ManagedPauseTaskHint           string
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
	HostedShowWindow               string
	HostedHideWindow               string
	HostedQuitProgram              string
	HostedReleaseWindow            string
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
	WindowSubtitle:                 "开机有序，桌面清爽",
	GlobalSettingsTitle:            "全局设置",
	RunAtLogon:                     "WinTray 开机自启动",
	StartHidden:                    "启动后最小化到托盘",
	ExitOnDone:                     "完成所有任务后自动退出",
	RetrySeconds:                   "窗口检测超时 (0–120 秒)",
	RetrySecondsInvalid:            "超时秒数必须是 0 到 120 的数字。",
	LanguageLabel:                  "语言:",
	ManagedListTitle:               "程序列表",
	ManagedListCount:               "%d 个程序 · %d 个已启用",
	ManagedListEmpty:               "还没有添加程序",
	ManagedListEmptyHint:           "点击“添加程序”，选择应用或脚本开始配置。",
	ManagedColumnName:              "程序",
	ManagedColumnPath:              "路径",
	ManagedColumnRule:              "启动方式",
	ManagedEditorTitle:             "程序设置",
	ManagedEditorHint:              "更改会自动保存",
	ManagedSelectedTitle:           "程序设置 · %s",
	ManagedSelectionHint:           "选择上方的程序以编辑启动方式。",
	ManagedAppPath:                 "程序路径:",
	BrowseProgram:                  "更换…",
	ManagedAppArgs:                 "启动参数:",
	ManagedArgsPlaceholder:         "可选，例如 --minimized",
	ManagedLaunchOnly:              "仅启动",
	ManagedAutoHide:                "启动后关闭窗口",
	ManagedAutoHideHint:            "发送关闭消息（WM_CLOSE）：托盘应用通常会收起，其他应用可能退出；控制台窗口由 WinTray 隐藏并代管托盘图标。",
	ManagedLaunchHidden:            "后台静默启动",
	ManagedLaunchHiddenHint:        "适用于脚本或命令行程序；不能与“启动后关闭窗口”同时启用。",
	ManagedPauseTask:               "暂停任务",
	ManagedPauseTaskHint:           "暂停后跳过该程序的开机启动，仍可点击“立即启动”。",
	ManagedLaunchNow:               "立即启动",
	ManagedLaunchNowBusy:           "启动中…",
	LaunchNowDoneBody:              "已启动: %s",
	AddProgram:                     "添加程序",
	RemoveSelected:                 "移除",
	OpenLogs:                       "打开日志",
	CleanupRestore:                 "重置数据…",
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
	HostedShowWindow:               "显示窗口",
	HostedHideWindow:               "隐藏窗口",
	HostedQuitProgram:              "退出 %s",
	HostedReleaseWindow:            "结束托管并显示窗口",
	SelectManagedExe:               "选择要托管的程序",
	ExeFilter:                      "程序文件 (*.exe;*.cmd;*.bat;*.ps1;*.py)|*.exe;*.cmd;*.bat;*.ps1;*.py",
	AllFilesFilter:                 "所有文件 (*.*)|*.*",
	NewAppName:                     "新程序",
	ManagedListItemTemplate:        "%s | %s | 启动后关闭窗口=%t",
	ManagedListHiddenTemplate:      "%s | %s | 后台静默启动=%t",
	ManagedListParamTemplate:       "关闭窗口=%t",
	ManagedListParamHiddenTemplate: "静默启动=%t",
	ManagedListParamPausedTemplate: "已暂停",
	RunSummaryNone:                 "没有可执行的受管任务。",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray 启动失败",
	FatalStartupBodyTemplate:       "%s\n\n日志: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray 已在运行。",
	StatusLaunchFailTemplate:       "启动失败: %s (%s)",
	StatusRetryExhausted:           "等待超时，未检测到程序窗口",
	StatusPermissionHint:           "可能是权限限制 (UIPI): 请尝试以管理员身份运行 WinTray。",
	StatusOpenLogsFailed:           "打开日志失败",
	CleanupConfirmTitle:            "重置并清理数据",
	CleanupConfirmBody:             "将清除 WinTray 的本地配置与日志，并恢复默认设置。\r\n\r\n是否继续？",
	CleanupDoneTitle:               "已计划清理",
	CleanupDoneBody:                "已恢复默认设置，WinTray 将在退出后清理本地数据。",
	CleanupFailedTitle:             "清理失败",
	CleanupFailedBody:              "重置并清理数据失败: %s",
	LanguageZhLabel:                "中文",
	LanguageEnLabel:                "English",
}

var enUS = Messages{
	WindowTitle:                    "WinTray",
	WindowSubtitle:                 "A quieter start. A cleaner desktop.",
	GlobalSettingsTitle:            "Global Settings",
	RunAtLogon:                     "Run WinTray at logon",
	StartHidden:                    "Minimize to tray after launch",
	ExitOnDone:                     "Exit after all tasks complete",
	RetrySeconds:                   "Window timeout (0–120 s)",
	RetrySecondsInvalid:            "Retry seconds must be a number between 0 and 120.",
	LanguageLabel:                  "Language:",
	ManagedListTitle:               "Program List",
	ManagedListCount:               "%d programs · %d enabled",
	ManagedListEmpty:               "No programs added yet",
	ManagedListEmptyHint:           "Add an application or script to get started.",
	ManagedColumnName:              "Program",
	ManagedColumnPath:              "Path",
	ManagedColumnRule:              "Startup behavior",
	ManagedEditorTitle:             "Program Settings",
	ManagedEditorHint:              "Changes are saved automatically",
	ManagedSelectedTitle:           "Program settings · %s",
	ManagedSelectionHint:           "Select a program above to edit its startup behavior.",
	ManagedAppPath:                 "Program path:",
	BrowseProgram:                  "Change…",
	ManagedAppArgs:                 "Launch arguments:",
	ManagedArgsPlaceholder:         "Optional, e.g. --minimized",
	ManagedLaunchOnly:              "Launch only",
	ManagedAutoHide:                "Close window after launch",
	ManagedAutoHideHint:            "Sends WM_CLOSE: tray-aware apps usually stay in the tray; other apps may exit. WinTray hides console windows and provides their tray icons.",
	ManagedLaunchHidden:            "Launch hidden in background",
	ManagedLaunchHiddenHint:        "For scripts or command-line programs. Cannot be combined with \"Close window after launch\".",
	ManagedPauseTask:               "Pause task",
	ManagedPauseTaskHint:           "Skips this program at startup. You can still use \"Launch Now\".",
	ManagedLaunchNow:               "Launch Now",
	ManagedLaunchNowBusy:           "Starting…",
	LaunchNowDoneBody:              "Started: %s",
	AddProgram:                     "Add Program",
	RemoveSelected:                 "Remove",
	OpenLogs:                       "Open Logs",
	CleanupRestore:                 "Reset data…",
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
	HostedShowWindow:               "Show Window",
	HostedHideWindow:               "Hide Window",
	HostedQuitProgram:              "Quit %s",
	HostedReleaseWindow:            "Stop Hosting and Show Window",
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
	return fmt.Sprintf("%s | %s | %s", app.Name, app.ExePath, FormatManagedParam(language, app))
}

func FormatManagedParam(language string, app config.ManagedAppEntry) string {
	msg := For(language)
	if !app.RunOnStartup {
		return msg.ManagedListParamPausedTemplate
	}
	if app.LaunchHiddenInBackground {
		return msg.ManagedLaunchHidden
	}
	if app.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		return msg.ManagedAutoHide
	}
	return msg.ManagedLaunchOnly
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
		"hidden_to_tray":             "hidden to tray",
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
		return "已启动 (无窗口规则)"
	case "started hidden":
		if Resolve(language) == LangEnUS {
			return "started hidden in background"
		}
		return "已后台静默启动"
	case "already running skipped":
		if Resolve(language) == LangEnUS {
			return "already running, skipped relaunch"
		}
		return "程序已在运行，已跳过启动"
	case "already running managed existing":
		if Resolve(language) == LangEnUS {
			return "already running, managed existing window"
		}
		return "程序已在运行，已处理现有窗口"
	case "no window managed", "no existing window managed":
		return msg.StatusRetryExhausted
	case "hidden to tray":
		if Resolve(language) == LangEnUS {
			return "hidden to tray; use its tray icon to open the window"
		}
		return "已收入托盘，可通过托盘图标打开窗口"
	case "managed", "managed existing":
		if Resolve(language) == LangEnUS {
			return "front window closed"
		}
		return "前台窗口已成功收起"
	case "invalid process name":
		if Resolve(language) == LangEnUS {
			return "invalid process name"
		}
		return "进程名无效"
	default:
		return message
	}
}
