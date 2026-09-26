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
	OpenSettings                   string
	BackToPrograms                 string
	SettingsTitle                  string
	LogonOffNotice                 string
	LogonOffEnable                 string
	SettingsStartupTitle           string
	RunAtLogon                     string
	RunAtLogonHint                 string
	StartHidden                    string
	StartHiddenHint                string
	ExitOnDone                     string
	ExitOnDoneHint                 string
	SettingsTimingTitle            string
	RetrySeconds                   string
	RetrySecondsHint               string
	RetrySecondsInvalid            string
	StartupInterval                string
	StartupIntervalInvalid         string
	StartupIntervalHint            string
	SecondsUnit                    string
	LanguageLabel                  string
	SettingsTroubleshootTitle      string
	LogsTitle                      string
	CleanupRestoreTitle            string
	ManagedListTitle               string
	ManagedListHint                string
	ManagedListEmpty               string
	ManagedListEmptyHint           string
	ManagedColumnName              string
	ManagedColumnRule              string
	BrowseProgram                  string
	BrowseProgramHint              string
	ManagedAppArgs                 string
	ManagedArgsHint                string
	ManagedArgsPlaceholder         string
	ManagedEnabled                 string
	ManagedEnabledLabel            string
	ManagedEnabledHint             string
	ManagedModeLabel               string
	ManagedLaunchOnly              string
	ManagedLaunchOnlyHint          string
	ManagedAutoHide                string
	ManagedAutoHideHint            string
	ManagedAutoHideTip             string
	ManagedAutoHideDelayed         string
	ManagedCloseDelay              string
	ManagedCloseDelayHint          string
	ManagedCloseDelayInvalid       string
	ManagedLaunchHidden            string
	ManagedLaunchHiddenHint        string
	ManagedLaunchNow               string
	ManagedLaunchNowHint           string
	ManagedLaunchNowBusy           string
	LaunchNowDoneBody              string
	AddProgram                     string
	AddProgramHint                 string
	RemoveSelected                 string
	RemoveSelectedHint             string
	OpenLogs                       string
	OpenLogsHint                   string
	CleanupRestore                 string
	CleanupRestoreHint             string
	CheckUpdate                    string
	CheckUpdateBusy                string
	GitHubLink                     string
	VersionLabel                   string
	UpdateTitle                    string
	UpdateAvailableBody            string
	UpdateLatestBody               string
	UpdateFailedBody               string
	ExitApp                        string
	RunSilently                    string
	RunSilentlyHint                string
	TrayOpenSettings               string
	TrayExit                       string
	TrayToolTip                    string
	TrayBoxHomeTitle               string
	TrayBoxHomeHint                string
	TrayBoxFailedTitle             string
	HostedShowWindow               string
	HostedHideWindow               string
	HostedQuitProgram              string
	HostedReleaseWindow            string
	SelectManagedExe               string
	SelectReplacementExe           string
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
	RemoveLogonTaskTitle           string
	RemoveLogonTaskHint            string
	RemoveLogonTask                string
	RemoveLogonTaskConfirmBody     string
	RemoveLogonTaskDoneBody        string
	RemoveLogonTaskFailedBody      string
	LanguageZhLabel                string
	LanguageEnLabel                string
}

var zhCN = Messages{
	WindowTitle:                    "WinTray",
	OpenSettings:                   "更多功能",
	BackToPrograms:                 "← 返回",
	SettingsTitle:                  "设置",
	LogonOffNotice:                 "WinTray 没有设置开机自启动，下面的程序不会在开机时自动启动",
	LogonOffEnable:                 "开启",
	SettingsStartupTitle:           "开机启动",
	RunAtLogon:                     "开机时自动运行 WinTray",
	RunAtLogonHint:                 "关闭后，开机启动项里的程序也不会自动启动",
	StartHidden:                    "开机时不弹出 WinTray 窗口",
	ExitOnDone:                     "开机任务完成后自动退出 WinTray",
	ExitOnDoneHint:                 "如果还有命令行程序在用 WinTray 提供的托盘图标，会等它们都退出后再退出",
	SettingsTimingTitle:            "启动节奏",
	RetrySeconds:                   "最长等待程序窗口",
	RetrySecondsHint:               "程序启动较慢、窗口没被收进托盘时，可以调大；可填 0–120",
	RetrySecondsInvalid:            "等待时间必须是 0 到 120 之间的整数",
	StartupInterval:                "程序之间的启动间隔",
	StartupIntervalInvalid:         "启动间隔必须是 0 到 120 之间的整数",
	StartupIntervalHint:            "错开启动，减轻开机卡顿；可填 0–120，0 表示不等待",
	SecondsUnit:                    "秒",
	LanguageLabel:                  "语言 / Language",
	ManagedListTitle:               "开机启动项",
	ManagedListHint:                "勾选开启任务，未选则暂停",
	ManagedListEmpty:               "还没有添加程序",
	ManagedListEmptyHint:           "添加开机时想自动启动的程序，WinTray 会依次启动它们，并按你的设置收进托盘或在后台运行",
	ManagedColumnName:              "程序",
	ManagedColumnRule:              "启动方式",
	BrowseProgram:                  "更换…",
	BrowseProgramHint:              "换成另一个程序文件",
	ManagedAppArgs:                 "启动参数（可选）",
	ManagedArgsHint:                "适用于 .exe 程序及 .bat、.cmd、.ps1、.py 等脚本",
	ManagedArgsPlaceholder:         "例如 --minimized",
	ManagedEnabled:                 "开机后自动启动此程序",
	ManagedEnabledHint:             "取消勾选后，开机时会跳过它；仍可随时点“立即启动”",
	ManagedModeLabel:               "启动方式",
	ManagedLaunchOnly:              "正常启动",
	ManagedLaunchOnlyHint:          "只负责启动，窗口保持原样",
	ManagedAutoHide:                "收进托盘",
	ManagedAutoHideHint:            "启动后自动关闭主窗口，程序继续在托盘运行；适合 QQ、微信等带托盘图标的程序；命令行程序会由 WinTray 提供托盘图标",
	ManagedAutoHideTip:             "WinTray 会向主窗口发送关闭消息（WM_CLOSE）；没有托盘图标的普通程序可能会因此退出",
	ManagedAutoHideDelayed:         "收进托盘（等 %d 秒）",
	ManagedCloseDelay:              "收起前等待",
	ManagedCloseDelayHint:          "先弹出登录窗口的程序（如 QQ）建议设为 10–15 秒，0 表示立即收起",
	ManagedCloseDelayInvalid:       "收起前等待时间必须是 0 到 600 之间的整数",
	ManagedLaunchHidden:            "后台启动",
	ManagedLaunchHiddenHint:        "不弹出任何窗口，直接在后台运行；适合 .bat、.ps1、.py 等脚本和命令行工具",
	ManagedLaunchNow:               "立即启动",
	ManagedLaunchNowHint:           "按当前设置马上启动一次，可以用来试试效果",
	ManagedLaunchNowBusy:           "启动中…",
	LaunchNowDoneBody:              "已启动: %s",
	AddProgram:                     "添加程序",
	AddProgramHint:                 "选择要开机启动的程序或脚本，可以一次选多个",
	RemoveSelected:                 "移除此程序",
	RemoveSelectedHint:             "只从列表中移除，不会卸载或删除程序本身",
	OpenLogs:                       "打开日志",
	OpenLogsHint:                   "查看 WinTray 的运行记录，排查问题时使用",
	CleanupRestore:                 "重置…",
	CleanupRestoreHint:             "清除所有设置和日志，恢复初始状态（会先确认）",
	CheckUpdate:                    "检查更新",
	CheckUpdateBusy:                "检查中…",
	GitHubLink:                     "GitHub 项目主页",
	VersionLabel:                   "版本 %s",
	UpdateTitle:                    "检查更新",
	UpdateAvailableBody:            "发现新版本 %s（当前 %s）\r\n\r\n是否前往下载页面？",
	UpdateLatestBody:               "当前已是最新版本 %s",
	UpdateFailedBody:               "检查更新失败: %s",
	ExitApp:                        "退出 WinTray",
	RunSilently:                    "后台静默运行",
	RunSilentlyHint:                "隐藏此窗口；有程序勾选托盘图标收纳时保留 WinTray 菜单入口，否则隐藏自身图标；保留命令行程序的托盘图标，再次打开 WinTray 可恢复",
	TrayOpenSettings:               "打开 WinTray",
	TrayExit:                       "退出 WinTray",
	TrayToolTip:                    "WinTray",
	TrayBoxHomeTitle:               "托盘图标收纳",
	TrayBoxHomeHint:                "将此程序的托盘图标收进 WinTray 菜单；点击菜单中的程序图标可打开程序",
	TrayBoxFailedTitle:             "托盘图标收纳",
	HostedShowWindow:               "显示窗口",
	HostedHideWindow:               "隐藏窗口",
	HostedQuitProgram:              "退出 %s",
	HostedReleaseWindow:            "结束托管并显示窗口",
	SelectManagedExe:               "选择要添加的程序（可多选）",
	SelectReplacementExe:           "选择新的程序文件",
	ExeFilter:                      "程序和脚本 (*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw)|*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw",
	AllFilesFilter:                 "所有文件 (*.*)|*.*",
	NewAppName:                     "新程序",
	ManagedListItemTemplate:        "%s | %s | 启动后关闭窗口=%t",
	ManagedListHiddenTemplate:      "%s | %s | 后台静默启动=%t",
	ManagedListParamTemplate:       "关闭窗口=%t",
	ManagedListParamHiddenTemplate: "静默启动=%t",
	ManagedListParamPausedTemplate: "已暂停",
	RunSummaryNone:                 "没有可执行的受管任务",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray 启动失败",
	FatalStartupBodyTemplate:       "%s\n\n日志: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray 已在运行",
	StatusLaunchFailTemplate:       "启动失败: %s (%s)",
	StatusRetryExhausted:           "等待超时，未检测到程序窗口",
	StatusPermissionHint:           "可能是权限限制 (UIPI): 请尝试以管理员身份运行 WinTray",
	StatusOpenLogsFailed:           "打开日志失败",
	CleanupConfirmTitle:            "重置并清理数据",
	CleanupConfirmBody:             "将清除 WinTray 的本地配置与日志，并恢复默认设置\r\n\r\n是否继续？",
	CleanupDoneTitle:               "已计划清理",
	CleanupDoneBody:                "已恢复默认设置，WinTray 将在退出后清理本地数据",
	CleanupFailedTitle:             "清理失败",
	CleanupFailedBody:              "重置并清理数据失败: %s",
	RemoveLogonTaskTitle:           "删除登录计划任务",
	RemoveLogonTaskHint:            "删除开机时运行 WinTray 的计划任务和自启项，并关闭开机自动运行（会先确认）",
	RemoveLogonTask:                "删除…",
	RemoveLogonTaskConfirmBody:     "将删除任务计划程序中登录时运行 WinTray 的任务和注册表自启项，并关闭“开机时自动运行 WinTray”\r\n\r\n之后可随时重新打开该开关恢复开机自启\r\n\r\n是否继续？",
	RemoveLogonTaskDoneBody:        "已删除登录计划任务，开机时不会再自动运行 WinTray",
	RemoveLogonTaskFailedBody:      "删除登录计划任务失败: %s",
	LanguageZhLabel:                "中文",
	StartHiddenHint:                "开机后只在托盘显示图标，需要时再打开窗口",
	SettingsTroubleshootTitle:      "故障排查",
	LogsTitle:                      "运行日志",
	CleanupRestoreTitle:            "重置所有数据",
	ManagedEnabledLabel:            "开机启动",
	LanguageEnLabel:                "English",
}

var enUS = Messages{
	WindowTitle:                    "WinTray",
	OpenSettings:                   "More Features",
	BackToPrograms:                 "← Back",
	SettingsTitle:                  "Settings",
	LogonOffNotice:                 "WinTray isn't set to run at sign-in, so the programs below won't start automatically",
	LogonOffEnable:                 "Turn On",
	SettingsStartupTitle:           "Sign-in",
	RunAtLogon:                     "Run WinTray at sign-in",
	RunAtLogonHint:                 "When off, your startup programs won't start automatically either",
	StartHidden:                    "Don't show the WinTray window at sign-in",
	ExitOnDone:                     "Exit WinTray when sign-in tasks finish",
	ExitOnDoneHint:                 "If console programs still use tray icons from WinTray, it waits until they all exit",
	SettingsTimingTitle:            "Timing",
	RetrySeconds:                   "Wait for a window up to",
	RetrySecondsHint:               "Increase it if slow programs aren't closed to the tray; 0–120",
	RetrySecondsInvalid:            "The wait must be a whole number from 0 to 120",
	StartupInterval:                "Delay between programs",
	StartupIntervalInvalid:         "The delay must be a whole number from 0 to 120",
	StartupIntervalHint:            "Staggers launches to ease sign-in load; 0–120; 0 means no wait",
	SecondsUnit:                    "seconds",
	LanguageLabel:                  "Language / 语言",
	ManagedListTitle:               "Startup programs",
	ManagedListHint:                "Check to enable, uncheck to pause",
	ManagedListEmpty:               "No programs yet",
	ManagedListEmptyHint:           "Add the programs you want to start at sign-in; WinTray starts them one by one and can close them to the tray or run them in the background",
	ManagedColumnName:              "Program",
	ManagedColumnRule:              "How it starts",
	BrowseProgram:                  "Change…",
	BrowseProgramHint:              "Use a different program file",
	ManagedAppArgs:                 "Arguments (optional)",
	ManagedArgsHint:                "For .exe programs and scripts such as .bat, .cmd, .ps1 and .py",
	ManagedArgsPlaceholder:         "Example: --minimized",
	ManagedEnabled:                 "Start this program at sign-in",
	ManagedEnabledHint:             "When unchecked, it's skipped at sign-in; you can still use Launch Now",
	ManagedModeLabel:               "How it starts",
	ManagedLaunchOnly:              "Start normally",
	ManagedLaunchOnlyHint:          "Just starts the program and leaves its window as is",
	ManagedAutoHide:                "Close to tray",
	ManagedAutoHideHint:            "Closes the main window after launch; the program keeps running in the tray; best for apps with a tray icon, such as QQ or WeChat; WinTray adds tray icons for console programs",
	ManagedAutoHideTip:             "WinTray sends WM_CLOSE to the main window; apps without a tray icon may exit",
	ManagedAutoHideDelayed:         "Close to tray (after %d s)",
	ManagedCloseDelay:              "Wait before closing",
	ManagedCloseDelayHint:          "Programs that show a sign-in window first (such as QQ) need about 10–15 seconds; 0 closes right away",
	ManagedCloseDelayInvalid:       "The wait must be a whole number from 0 to 600",
	ManagedLaunchHidden:            "Run in background",
	ManagedLaunchHiddenHint:        "Runs in the background without showing any window; best for scripts (.bat, .ps1, .py) and command-line tools",
	ManagedLaunchNow:               "Launch Now",
	ManagedLaunchNowHint:           "Starts it now with these settings, so you can try them out",
	ManagedLaunchNowBusy:           "Starting…",
	LaunchNowDoneBody:              "Started: %s",
	AddProgram:                     "Add Program",
	AddProgramHint:                 "Pick programs or scripts to start at sign-in; you can select several at once",
	RemoveSelected:                 "Remove",
	RemoveSelectedHint:             "Only removes it from this list; the program itself isn't uninstalled or deleted",
	OpenLogs:                       "Open Logs",
	OpenLogsHint:                   "WinTray's activity record, for troubleshooting",
	CleanupRestore:                 "Reset…",
	CleanupRestoreHint:             "Clears all settings and logs and restores defaults (asks first)",
	CheckUpdate:                    "Check for Updates",
	CheckUpdateBusy:                "Checking…",
	GitHubLink:                     "GitHub project page",
	VersionLabel:                   "Version %s",
	UpdateTitle:                    "Check for Updates",
	UpdateAvailableBody:            "Version %s is available (current %s)\r\n\r\nOpen the download page?",
	UpdateLatestBody:               "You are on the latest version %s",
	UpdateFailedBody:               "Update check failed: %s",
	ExitApp:                        "Exit WinTray",
	RunSilently:                    "Run silently",
	RunSilentlyHint:                "Hides this window; keeps WinTray's menu icon if a program collects its tray icon, otherwise hides it; hosted console icons remain",
	TrayOpenSettings:               "Open WinTray",
	TrayExit:                       "Exit WinTray",
	TrayToolTip:                    "WinTray",
	TrayBoxHomeTitle:               "Collect tray icon",
	TrayBoxHomeHint:                "Collect this program's tray icon in WinTray's menu; select its icon there to open the program",
	TrayBoxFailedTitle:             "Collected tray icons",
	HostedShowWindow:               "Show Window",
	HostedHideWindow:               "Hide Window",
	HostedQuitProgram:              "Quit %s",
	HostedReleaseWindow:            "Stop Hosting and Show Window",
	SelectManagedExe:               "Select programs to add (you can pick several)",
	SelectReplacementExe:           "Select the new program file",
	ExeFilter:                      "Programs and scripts (*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw)|*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw",
	AllFilesFilter:                 "All Files (*.*)|*.*",
	NewAppName:                     "New App",
	ManagedListItemTemplate:        "%s | %s | CloseAfterLaunch=%t",
	ManagedListHiddenTemplate:      "%s | %s | LaunchHidden=%t",
	ManagedListParamTemplate:       "CloseAfterLaunch=%t",
	ManagedListParamHiddenTemplate: "LaunchHidden=%t",
	ManagedListParamPausedTemplate: "Paused",
	RunSummaryNone:                 "No managed tasks to run",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray startup failed",
	FatalStartupBodyTemplate:       "%s\n\nLog: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray is already running",
	StatusLaunchFailTemplate:       "Launch failed: %s (%s)",
	StatusRetryExhausted:           "Retry exhausted, no manageable window found",
	StatusPermissionHint:           "Possible UIPI permission limitation: try running WinTray as administrator",
	StatusOpenLogsFailed:           "Failed to open logs",
	CleanupConfirmTitle:            "Cleanup && Restore Defaults",
	CleanupConfirmBody:             "This will clear WinTray local settings and logs, then restore defaults\r\n\r\nContinue?",
	CleanupDoneTitle:               "Cleanup Scheduled",
	CleanupDoneBody:                "Default settings restored; WinTray data will be cleaned after exit",
	CleanupFailedTitle:             "Cleanup Failed",
	CleanupFailedBody:              "Cleanup and restore failed: %s",
	RemoveLogonTaskTitle:           "Remove sign-in task",
	RemoveLogonTaskHint:            "Deletes the scheduled task and startup entry that run WinTray at sign-in, and turns running at sign-in off (asks first)",
	RemoveLogonTask:                "Remove…",
	RemoveLogonTaskConfirmBody:     "This deletes the Task Scheduler task and registry startup entry that run WinTray at sign-in, and turns off \"Run WinTray at sign-in\"\r\n\r\nTurn the switch back on at any time to restore it\r\n\r\nContinue?",
	RemoveLogonTaskDoneBody:        "Sign-in task removed; WinTray will no longer run at sign-in",
	RemoveLogonTaskFailedBody:      "Removing the sign-in task failed: %s",
	LanguageZhLabel:                "中文",
	StartHiddenHint:                "Only the tray icon appears at sign-in; open the window when you need it",
	SettingsTroubleshootTitle:      "Troubleshooting",
	LogsTitle:                      "Logs",
	CleanupRestoreTitle:            "Reset all data",
	ManagedEnabledLabel:            "At sign-in",
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
		if delay := app.TrayBehavior.CloseDelaySeconds; delay > 0 {
			return fmt.Sprintf(msg.ManagedAutoHideDelayed, delay)
		}
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
		"startup_check_failed":       "startup check failed",
		"external_startup_timeout":   "external startup timeout",
		"cancelled":                  "cancelled",
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
	case "startup check failed":
		if Resolve(language) == LangEnUS {
			return "could not check Windows startup; skipped launch to avoid a duplicate"
		}
		return "无法检查系统自启项，为避免重复启动已跳过"
	case "external startup timeout":
		if Resolve(language) == LangEnUS {
			return "timed out waiting for Windows startup; no duplicate launch attempted"
		}
		return "等待程序自启动超时，未重复拉起程序"
	case "cancelled":
		if Resolve(language) == LangEnUS {
			return "cancelled before launch"
		}
		return "已取消启动"
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
