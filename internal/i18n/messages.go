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
	ProgramNameRequired            string
	ProgramsTab                    string
	SettingsTab                    string
	CommonSettings                 string
	MoreSettings                   string
	ResizePanels                   string
	LaunchBehavior                 string
	ScheduleTitle                  string
	ProgramName                    string
	ReplaceColumn                  string
	TrayColumn                     string
	FrequencyDaysLabel             string
	FrequencyRunsLabel             string
	ScheduleRequired               string
	WindowTitle                    string
	OpenSettings                   string
	OpenSettingsFailed             string
	BackToPrograms                 string
	SettingsTitle                  string
	LogonOffNotice                 string
	LogonOffEnable                 string
	SettingsStartupTitle           string
	RunAtLogon                     string
	RunAtLogonHint                 string
	StartHidden                    string
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
	ManagedProgramCount            string
	ManagedDragHandle              string
	ManagedDragHint                string
	ManagedSelectPrompt            string
	ManagedSelectHint              string
	ManagedScheduleOffHint         string
	ManagedScheduleRangeHint       string
	ManagedListHint                string
	ManagedDropRejected            string
	ManagedListEmpty               string
	ManagedListEmptyHint           string
	ManagedExampleName             string
	ManagedColumnName              string
	ManagedColumnOrder             string
	ManagedColumnRule              string
	ManagedColumnDelete            string
	BrowseProgram                  string
	BrowseProgramHint              string
	ManagedAppArgs                 string
	ManagedArgsHint                string
	ManagedArgsPlaceholder         string
	ManagedWorkingDirectory        string
	ManagedWorkingDirectoryHint    string
	BrowseWorkingDirectory         string
	ManagedTaskDirectoryHint       string
	ManagedEnabled                 string
	ManagedEnabledHint             string
	ManagedModeLabel               string
	ManagedLaunchOnly              string
	ManagedLaunchOnlyHint          string
	ManagedTaskLaunch              string
	ManagedTaskLaunchHint          string
	ManagedTaskLaunchTip           string
	ManagedTaskArgsHint            string
	ManagedTaskLaunchNowHint       string
	ManagedTaskFailedTitle         string
	ManagedTaskFailedBody          string
	StartupCommandUnquoted         string
	StartupCommandIndirect         string
	ManagedAutoHide                string
	ManagedAutoHideHint            string
	ManagedAutoHideTip             string
	ManagedAutoHideDelayed         string
	ManagedCloseDelay              string
	ManagedCloseDelayHint          string
	ManagedCloseDelayInvalid       string
	ManagedWeChatAutoLogin         string
	ManagedWeChatAutoLoginHint     string
	ManagedQQWaitForLogin          string
	ManagedQQWaitForLoginHint      string
	ManagedSchedule                string
	ManagedScheduleEnabled         string
	ManagedScheduleStart           string
	ManagedScheduleStartUnit       string
	ManagedScheduleExit            string
	ManagedScheduleExitUnit        string
	ManagedScheduleHint            string
	ManagedFrequency               string
	ManagedFrequencyDays           string
	ManagedFrequencyRuns           string
	ManagedFrequencyHint           string
	ManagedFrequencyInvalid        string
	ManagedScheduleInvalid         string
	ManagedScheduleTag             string
	ManagedLaunchHidden            string
	ManagedLaunchHiddenHint        string
	ManagedLaunchNow               string
	ManagedLaunchNowHint           string
	ManagedLaunchNowBusy           string
	ManagedStopProgram             string
	ManagedStopProgramBusy         string
	ManagedStopProgramHint         string
	ManagedStopProgramFailed       string
	ManagedWindowBusy              string
	ManagedWindowHint              string
	ManagedWindowNotRunning        string
	ManagedWindowUnavailable       string
	ManagedWindowFailed            string
	ManagedWindowActivation        string
	AddProgram                     string
	AddProgramHint                 string
	RemoveConfirmPrompt            string
	RemoveConfirmLabel             string
	OpenLogs                       string
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
	TrayExit                       string
	TrayToolTip                    string
	TrayMenuFailedBody             string
	TrayBoxHomeTitle               string
	TrayBoxHomeHint                string
	TrayBoxFailedTitle             string
	TrayBoxFailedBody              string
	TrayBoxIconGoneBody            string
	TrayBoxNotClickableBody        string
	TrayBoxClickFailedBody         string
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
	SwitchLanguageHint             string
}

var zhCN = Messages{
	ProgramNameRequired:            "请输入程序名称",
	ProgramsTab:                    "程序列表",
	SettingsTab:                    "设置",
	CommonSettings:                 "启动与计划",
	MoreSettings:                   "程序信息",
	ResizePanels:                   "调整列表与设置宽度",
	LaunchBehavior:                 "启动行为",
	ScheduleTitle:                  "时间计划",
	ProgramName:                    "名称",
	ReplaceColumn:                  "更换",
	TrayColumn:                     "收纳",
	FrequencyDaysLabel:             "最近天数",
	FrequencyRunsLabel:             "最多运行次数",
	ScheduleRequired:               "开启“自定义时间”后，可限制自动运行次数",
	WindowTitle:                    "WinTray",
	OpenSettings:                   "设置",
	OpenSettingsFailed:             "无法打开设置：%s\n请左键单击 WinTray 托盘图标重试",
	BackToPrograms:                 "← 返回",
	SettingsTitle:                  "设置",
	LogonOffNotice:                 "开机启动已关闭，WinTray 的启动计划已暂停",
	LogonOffEnable:                 "开启",
	SettingsStartupTitle:           "启动",
	RunAtLogon:                     "开机启动",
	RunAtLogonHint:                 "关闭后暂停计划，恢复原自启",
	StartHidden:                    "启动时隐藏窗口",
	ExitOnDone:                     "任务完成后退出",
	ExitOnDoneHint:                 "有托盘、后台窗口或定时任务时不退出",
	SettingsTimingTitle:            "启动节奏",
	RetrySeconds:                   "窗口等待上限（秒）",
	RetrySecondsHint:               "0–120 秒；窗口出现较慢时可调大",
	RetrySecondsInvalid:            "等待时间必须是 0 到 120 之间的整数",
	StartupInterval:                "启动间隔（秒）",
	StartupIntervalInvalid:         "启动间隔必须是 0 到 120 之间的整数",
	StartupIntervalHint:            "0–120 秒，0 表示不等待；仅限 WinTray 启动的程序",
	SecondsUnit:                    "秒",
	LanguageLabel:                  "语言 / Language",
	SwitchLanguageHint:             "Switch to English",
	ManagedListTitle:               "程序管理",
	ManagedProgramCount:            "%d 个程序",
	ManagedDragHandle:              "拖动排序",
	ManagedDragHint:                "拖动把手调整启动顺序，也可选择序号",
	ManagedSelectPrompt:            "选择一个程序",
	ManagedSelectHint:              "选择左侧程序，设置启动方式和时间",
	ManagedScheduleOffHint:         "设置启动延迟、自动退出和运行次数",
	ManagedScheduleRangeHint:       "0–1440 分钟；自动退出填 0 表示不退出",
	ManagedListHint:                "拖入文件添加 · 拖动把手排序",
	ManagedDropRejected:            "以下项目未添加：\r\n%s\r\n\r\n请确认文件存在，并选择 .exe、.bat、.cmd、.ps1、.py、.pyw 或 .ahk 文件",
	ManagedListEmpty:               "还没有添加程序",
	ManagedExampleName:             "示例程序",
	ManagedListEmptyHint:           "拖入程序或脚本，或选择“添加程序”；支持多选",
	ManagedColumnName:              "程序名称",
	ManagedColumnOrder:             "序号",
	ManagedColumnRule:              "启动动作",
	ManagedColumnDelete:            "删除",
	BrowseProgram:                  "更换…",
	BrowseProgramHint:              "换成另一个程序文件",
	ManagedAppArgs:                 "启动参数（可选）",
	ManagedArgsHint:                "含空格的参数请用双引号括起",
	ManagedArgsPlaceholder:         "例如 --minimized",
	ManagedWorkingDirectory:        "工作目录（可选）",
	ManagedWorkingDirectoryHint:    "留空使用程序所在目录；相对路径以程序所在目录为起点",
	BrowseWorkingDirectory:         "选择文件夹…",
	ManagedTaskDirectoryHint:       "沿用原自启入口时，使用该入口的工作目录",
	ManagedEnabled:                 "开机启动此程序",
	ManagedEnabledHint:             "关闭后停止普通开机启动；“自定义时间”和程序原有自启仍可生效",
	ManagedModeLabel:               "启动方式",
	ManagedLaunchOnly:              "正常启动",
	ManagedLaunchOnlyHint:          "优先使用程序原有自启；没有原有自启时，由 WinTray 启动",
	ManagedTaskLaunch:              "开机即启动",
	ManagedTaskLaunchHint:          "登录 Windows 时启动；保留原有自启的参数和权限，没有原有自启时使用本页设置",
	ManagedTaskLaunchTip:           "仅支持 .exe，不受启动间隔限制；首次创建管理员任务需要确认，原任务的延迟保持不变",
	ManagedTaskArgsHint:            "有原有自启时使用原参数；否则使用此处参数",
	ManagedTaskLaunchNowHint:       "设置保存后，运行登录任务；程序已运行时跳过",
	ManagedTaskFailedTitle:         "开机即启动未完成",
	ManagedTaskFailedBody:          "%s\r\n\r\n请处理问题后重新保存设置\r\n原自启备份已保留，恢复前请勿删除 WinTray 数据目录",
	StartupCommandUnquoted:         "%s：无法识别含空格的原自启路径；请给程序路径加上引号",
	StartupCommandIndirect:         "%s：原自启通过其他程序启动；请选择其他启动方式",
	ManagedAutoHide:                "收进托盘",
	ManagedAutoHideHint:            "启动后收起程序窗口；命令行程序使用 WinTray 提供的托盘图标",
	ManagedAutoHideTip:             "部分程序仍可能短暂闪现界面；没有托盘图标的普通程序可能退出",
	ManagedAutoHideDelayed:         "收进托盘（等 %d 秒）",
	ManagedCloseDelay:              "收起前等待（秒）",
	ManagedCloseDelayHint:          "范围 0–600 秒，从程序启动时计时",
	ManagedCloseDelayInvalid:       "收起前等待时间必须是 0 到 600 之间的整数",
	ManagedWeChatAutoLogin:         "微信自动登录",
	ManagedWeChatAutoLoginHint:     "自动点击登录按钮；需要扫码或手机确认时，请手动完成",
	ManagedQQWaitForLogin:          "登录成功后收起",
	ManagedQQWaitForLoginHint:      "等待 QQ 登录成功后收进托盘；不自动点击登录",
	ManagedSchedule:                "自定义时间",
	ManagedScheduleEnabled:         "启用",
	ManagedScheduleStart:           "启动延迟（分钟）",
	ManagedScheduleStartUnit:       "分钟启动",
	ManagedScheduleExit:            "自动退出（分钟）",
	ManagedScheduleExitUnit:        "分钟后退出",
	ManagedScheduleHint:            "范围 0–1440 分钟，自动退出填 0 表示不退出；关闭“开机启动此程序”后，此计划仍生效",
	ManagedFrequency:               "限制自动运行次数",
	ManagedFrequencyDays:           "天内",
	ManagedFrequencyRuns:           "次",
	ManagedFrequencyHint:           "按自然日计数，包含原有开机自启；手动启动不计数",
	ManagedFrequencyInvalid:        "请输入整数：天数 1–365，次数 1–1000",
	ManagedScheduleInvalid:         "时间必须是 0 到 1440 之间的整数（分钟）",
	ManagedScheduleTag:             "%s · 定时",
	ManagedLaunchHidden:            "后台启动",
	ManagedLaunchHiddenHint:        "隐藏控制台启动；选择“显示窗口”可查看输出和输入命令",
	ManagedLaunchNow:               "立即启动",
	ManagedLaunchNowHint:           "启动一次；关闭“开机启动此程序”时，使用正常启动方式",
	ManagedLaunchNowBusy:           "启动中…",
	ManagedStopProgram:             "停止程序",
	ManagedStopProgramBusy:         "停止中…",
	ManagedStopProgramHint:         "关闭或退出正在运行的程序",
	ManagedStopProgramFailed:       "无法停止程序，请在程序窗口中退出",
	ManagedWindowBusy:              "切换中…",
	ManagedWindowHint:              "显示或隐藏程序窗口",
	ManagedWindowNotRunning:        "程序未运行，请先选择“立即启动”",
	ManagedWindowUnavailable:       "程序没有可显示的窗口；若由旧版 WinTray 后台启动，请退出程序后重新启动",
	ManagedWindowFailed:            "无法切换窗口，请确认程序仍在响应后重试",
	ManagedWindowActivation:        "窗口已显示，请在任务栏选中它",
	AddProgram:                     "添加程序",
	AddProgramHint:                 "选择要开机启动的程序或脚本，可以一次选多个",
	RemoveConfirmPrompt:            "从列表删除“%s”？",
	RemoveConfirmLabel:             "删除",
	OpenLogs:                       "打开",
	CleanupRestore:                 "重置…",
	CleanupRestoreHint:             "删除设置和日志，不可撤销",
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
	RunSilentlyHint:                "隐藏设置窗口；托盘收纳或后台窗口需要时保留 WinTray 图标；重新打开 WinTray 可显示设置",
	TrayExit:                       "退出 WinTray",
	TrayToolTip:                    "WinTray",
	TrayBoxHomeTitle:               "托盘收纳",
	TrayBoxHomeHint:                "将托盘图标收进 WinTray 菜单；单击打开窗口，右键单击打开程序菜单",
	TrayMenuFailedBody:             "无法打开托盘菜单\n请重启 WinTray 后重试",
	TrayBoxFailedTitle:             "托盘图标收纳",
	TrayBoxFailedBody:              "托盘收纳未能完成\n请重新启用收纳；若仍失败，请打开日志",
	TrayBoxIconGoneBody:            "%s 的托盘图标已不存在，程序可能已经退出",
	TrayBoxNotClickableBody:        "%s 的托盘图标不响应点击",
	TrayBoxClickFailedBody:         "无法打开 %s 的托盘图标：%v",
	HostedShowWindow:               "显示窗口",
	HostedHideWindow:               "隐藏窗口",
	HostedQuitProgram:              "退出 %s",
	HostedReleaseWindow:            "结束托管并显示窗口",
	SelectManagedExe:               "选择要添加的程序（可多选）",
	SelectReplacementExe:           "选择新的程序文件",
	ExeFilter:                      "程序和脚本 (*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw;*.ahk)|*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw;*.ahk",
	AllFilesFilter:                 "所有文件 (*.*)|*.*",
	NewAppName:                     "新程序",
	ManagedListItemTemplate:        "%s | %s | 启动后关闭窗口=%t",
	ManagedListHiddenTemplate:      "%s | %s | 后台静默启动=%t",
	ManagedListParamTemplate:       "关闭窗口=%t",
	ManagedListParamHiddenTemplate: "静默启动=%t",
	ManagedListParamPausedTemplate: "原生行为",
	RunSummaryNone:                 "没有可执行的受管任务",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray 启动失败",
	FatalStartupBodyTemplate:       "%s\n\n日志: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray 已在运行",
	StatusLaunchFailTemplate:       "启动失败: %s (%s)",
	StatusRetryExhausted:           "等待超时，未检测到程序窗口",
	StatusPermissionHint:           "可能权限不足，请尝试以管理员身份运行 WinTray",
	StatusOpenLogsFailed:           "无法打开日志，请打开 WinTray 数据目录查看",
	CleanupConfirmTitle:            "重置所有数据",
	CleanupConfirmBody:             "将删除所有程序设置和日志，并恢复默认设置\r\n此操作无法撤销\r\n\r\n是否继续？",
	CleanupDoneTitle:               "已计划清理",
	CleanupDoneBody:                "已恢复默认设置，WinTray 将在退出后清理本地数据",
	CleanupFailedTitle:             "清理失败",
	CleanupFailedBody:              "重置并清理数据失败: %s",
	RemoveLogonTaskTitle:           "删除自启项",
	RemoveLogonTaskHint:            "同时关闭开机启动",
	RemoveLogonTask:                "删除…",
	RemoveLogonTaskConfirmBody:     "删除 WinTray 自启项并关闭“开机启动”？\r\n重新开启可恢复自启项",
	RemoveLogonTaskDoneBody:        "已删除自启项，开机启动已关闭",
	RemoveLogonTaskFailedBody:      "删除自启项失败: %s",
	LanguageZhLabel:                "中文",
	SettingsTroubleshootTitle:      "故障排查",
	LogsTitle:                      "日志",
	CleanupRestoreTitle:            "重置数据",
	LanguageEnLabel:                "English",
}

var enUS = Messages{
	ProgramNameRequired:            "Enter a program name",
	ProgramsTab:                    "Programs",
	SettingsTab:                    "Settings",
	CommonSettings:                 "Startup & schedule",
	MoreSettings:                   "Program details",
	ResizePanels:                   "Resize program list and settings",
	LaunchBehavior:                 "Launch behavior",
	ScheduleTitle:                  "Timing",
	ProgramName:                    "Name",
	ReplaceColumn:                  "Change",
	TrayColumn:                     "Tray",
	FrequencyDaysLabel:             "Lookback period (days)",
	FrequencyRunsLabel:             "Maximum launches",
	ScheduleRequired:               "Turn on Custom timing to limit automatic runs",
	WindowTitle:                    "WinTray",
	OpenSettings:                   "Settings",
	OpenSettingsFailed:             "Could not open settings: %s\nLeft-click the WinTray tray icon to try again",
	BackToPrograms:                 "← Back",
	SettingsTitle:                  "Settings",
	LogonOffNotice:                 "WinTray startup is off; its start schedules are paused",
	LogonOffEnable:                 "Turn On",
	SettingsStartupTitle:           "Sign-in",
	RunAtLogon:                     "Start at sign-in",
	RunAtLogonHint:                 "When off, pauses schedules and restores original startup",
	StartHidden:                    "Start with window hidden",
	ExitOnDone:                     "Exit after tasks finish",
	ExitOnDoneHint:                 "Stays open for tray icons, background windows or scheduled tasks",
	SettingsTimingTitle:            "Timing",
	RetrySeconds:                   "Window wait limit (seconds)",
	RetrySecondsHint:               "0–120 seconds; increase for slow windows",
	RetrySecondsInvalid:            "The wait must be a whole number from 0 to 120",
	StartupInterval:                "Launch interval (seconds)",
	StartupIntervalInvalid:         "The interval must be a whole number from 0 to 120",
	StartupIntervalHint:            "0–120 seconds; 0 means no wait; only for programs WinTray starts",
	SecondsUnit:                    "seconds",
	LanguageLabel:                  "Language / 语言",
	SwitchLanguageHint:             "切换为中文",
	ManagedListTitle:               "Program management",
	ManagedProgramCount:            "Programs: %d",
	ManagedDragHandle:              "Reorder",
	ManagedDragHint:                "Drag the grip to reorder, or choose an order number",
	ManagedSelectPrompt:            "Select a program",
	ManagedSelectHint:              "Select a program on the left to set how and when it starts",
	ManagedScheduleOffHint:         "Set a launch delay, automatic exit and launch limits",
	ManagedScheduleRangeHint:       "0–1440 minutes; set automatic exit to 0 to keep running",
	ManagedListHint:                "Drop files to add · Drag grips to reorder",
	ManagedDropRejected:            "These items were not added:\r\n%s\r\n\r\nMake sure the files exist and select .exe, .bat, .cmd, .ps1, .py, .pyw or .ahk files",
	ManagedListEmpty:               "No programs yet",
	ManagedExampleName:             "Example program",
	ManagedListEmptyHint:           "Drag programs or scripts here, or select Add Program; you can select more than one file",
	ManagedColumnName:              "Program name",
	ManagedColumnOrder:             "Order",
	ManagedColumnRule:              "Start action",
	ManagedColumnDelete:            "Delete",
	BrowseProgram:                  "Change…",
	BrowseProgramHint:              "Use a different program file",
	ManagedAppArgs:                 "Arguments (optional)",
	ManagedArgsHint:                "Put arguments that contain spaces in double quotes",
	ManagedArgsPlaceholder:         "Example: --minimized",
	ManagedWorkingDirectory:        "Working directory (optional)",
	ManagedWorkingDirectoryHint:    "Leave blank to use the program folder; relative paths start from that folder",
	BrowseWorkingDirectory:         "Choose folder…",
	ManagedTaskDirectoryHint:       "An original startup entry supplies its own working directory",
	ManagedEnabled:                 "Start this program at sign-in",
	ManagedEnabledHint:             "When off, stops normal sign-in starts; Custom timing and the program's original startup can still run",
	ManagedModeLabel:               "How it starts",
	ManagedLaunchOnly:              "Start normally",
	ManagedLaunchOnlyHint:          "Uses the program's original startup; if there is none, WinTray starts the program",
	ManagedTaskLaunch:              "Launch at boot",
	ManagedTaskLaunchHint:          "Starts at Windows sign-in with the original startup arguments and permissions; uses these settings if no original startup exists",
	ManagedTaskLaunchTip:           "Requires an .exe file; ignores the launch interval; new administrator tasks need confirmation; existing task delays stay unchanged",
	ManagedTaskArgsHint:            "Uses the original startup arguments when available; otherwise uses these arguments",
	ManagedTaskLaunchNowHint:       "Runs the sign-in task after settings are saved; skips programs that are already running",
	ManagedTaskFailedTitle:         "Launch at boot not applied",
	ManagedTaskFailedBody:          "%s\r\n\r\nCorrect the problem and save the settings again\r\nThe original startup backup remains; do not delete the WinTray data folder before restoration",
	StartupCommandUnquoted:         "%s: cannot identify the original startup path with spaces; put the program path in double quotes",
	StartupCommandIndirect:         "%s: the original startup uses another program; select a different start mode",
	ManagedAutoHide:                "Close to tray",
	ManagedAutoHideHint:            "Closes program windows to the tray after startup; WinTray supplies tray icons for console programs",
	ManagedAutoHideTip:             "Some programs may briefly show a window; GUI apps without a tray icon may exit",
	ManagedAutoHideDelayed:         "Close to tray (after %d s)",
	ManagedCloseDelay:              "Wait before closing (sec)",
	ManagedCloseDelayHint:          "Range: 0–600 seconds from program start",
	ManagedCloseDelayInvalid:       "The wait must be a whole number from 0 to 600",
	ManagedWeChatAutoLogin:         "WeChat auto login",
	ManagedWeChatAutoLoginHint:     "Clicks the login button automatically; scan the QR code or confirm on your phone when required",
	ManagedQQWaitForLogin:          "Close after login",
	ManagedQQWaitForLoginHint:      "Waits for QQ to log in, then closes to tray; does not click Log in",
	ManagedSchedule:                "Custom timing",
	ManagedScheduleEnabled:         "On",
	ManagedScheduleStart:           "Launch delay (min)",
	ManagedScheduleStartUnit:       "min after sign-in",
	ManagedScheduleExit:            "Automatic exit (min)",
	ManagedScheduleExitUnit:        "min",
	ManagedScheduleHint:            "Range: 0–1440 minutes; automatic exit set to 0 keeps the program running; this schedule still runs when sign-in startup is off",
	ManagedFrequency:               "Limit automatic launches",
	ManagedFrequencyDays:           "days:",
	ManagedFrequencyRuns:           "runs",
	ManagedFrequencyHint:           "Counts by calendar day, including original sign-in starts; manual starts do not count",
	ManagedFrequencyInvalid:        "Enter whole numbers: 1–365 days, 1–1000 launches",
	ManagedScheduleInvalid:         "Times must be whole minutes from 0 to 1440",
	ManagedScheduleTag:             "%s · timed",
	ManagedLaunchHidden:            "Run in background",
	ManagedLaunchHiddenHint:        "Starts with the console hidden; select Show Window to read output and enter commands",
	ManagedLaunchNow:               "Launch Now",
	ManagedLaunchNowHint:           "Starts once; uses normal mode when Start this program at sign-in is off",
	ManagedLaunchNowBusy:           "Starting…",
	ManagedStopProgram:             "Stop Program",
	ManagedStopProgramBusy:         "Stopping…",
	ManagedStopProgramHint:         "Close or quit the running program",
	ManagedStopProgramFailed:       "Could not stop the program; exit through its window",
	ManagedWindowBusy:              "Switching…",
	ManagedWindowHint:              "Show or hide the program window",
	ManagedWindowNotRunning:        "The program is not running; select Launch Now first",
	ManagedWindowUnavailable:       "The program has no window to show; if an older WinTray started it in the background, exit and restart it",
	ManagedWindowFailed:            "Could not change window visibility; check that the program is responding and try again",
	ManagedWindowActivation:        "The window is visible; select it on the taskbar",
	AddProgram:                     "Add Program",
	AddProgramHint:                 "Pick programs or scripts to start at sign-in; you can select several at once",
	RemoveConfirmPrompt:            "Remove \"%s\" from the list?",
	RemoveConfirmLabel:             "Delete",
	OpenLogs:                       "Open",
	CleanupRestore:                 "Reset…",
	CleanupRestoreHint:             "Deletes settings and logs; cannot be undone",
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
	RunSilentlyHint:                "Hides settings; keeps the WinTray icon when collected icons or background windows need it; open WinTray again to show settings",
	TrayExit:                       "Exit WinTray",
	TrayToolTip:                    "WinTray",
	TrayBoxHomeTitle:               "Tray icon",
	TrayBoxHomeHint:                "Move the tray icon into the WinTray menu; click to open the window, or right-click to open the program menu",
	TrayMenuFailedBody:             "Could not open the tray menu\nRestart WinTray and try again",
	TrayBoxFailedTitle:             "Collected tray icons",
	TrayBoxFailedBody:              "Tray collection could not complete\nEnable it again; if it fails again, open the log",
	TrayBoxIconGoneBody:            "The tray icon of %s is gone; the program may have exited",
	TrayBoxNotClickableBody:        "The tray icon of %s does not respond to clicks",
	TrayBoxClickFailedBody:         "Could not open the tray icon for %s: %v",
	HostedShowWindow:               "Show Window",
	HostedHideWindow:               "Hide Window",
	HostedQuitProgram:              "Quit %s",
	HostedReleaseWindow:            "Stop Hosting and Show Window",
	SelectManagedExe:               "Select programs to add (you can pick several)",
	SelectReplacementExe:           "Select the new program file",
	ExeFilter:                      "Programs and scripts (*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw;*.ahk)|*.exe;*.bat;*.cmd;*.ps1;*.py;*.pyw;*.ahk",
	AllFilesFilter:                 "All Files (*.*)|*.*",
	NewAppName:                     "New App",
	ManagedListItemTemplate:        "%s | %s | CloseAfterLaunch=%t",
	ManagedListHiddenTemplate:      "%s | %s | LaunchHidden=%t",
	ManagedListParamTemplate:       "CloseAfterLaunch=%t",
	ManagedListParamHiddenTemplate: "LaunchHidden=%t",
	ManagedListParamPausedTemplate: "Native behavior",
	RunSummaryNone:                 "No managed tasks to run",
	RunSummaryLine:                 "%s: %s",
	FatalStartupTitle:              "WinTray startup failed",
	FatalStartupBodyTemplate:       "%s\n\nLog: %s",
	AlreadyRunningTitle:            "WinTray",
	AlreadyRunningBody:             "WinTray is already running",
	StatusLaunchFailTemplate:       "Launch failed: %s (%s)",
	StatusRetryExhausted:           "The wait ended before a program window was found",
	StatusPermissionHint:           "Permissions may be insufficient; try running WinTray as administrator",
	StatusOpenLogsFailed:           "Could not open the log; open the WinTray data folder to view it",
	CleanupConfirmTitle:            "Reset all data",
	CleanupConfirmBody:             "This deletes all program settings and logs, and restores default settings\r\nYou cannot undo this action\r\n\r\nContinue?",
	CleanupDoneTitle:               "Cleanup Scheduled",
	CleanupDoneBody:                "Default settings restored; WinTray data will be cleaned after exit",
	CleanupFailedTitle:             "Cleanup Failed",
	CleanupFailedBody:              "Cleanup and restore failed: %s",
	RemoveLogonTaskTitle:           "Remove startup entries",
	RemoveLogonTaskHint:            "Also turns off sign-in startup",
	RemoveLogonTask:                "Remove…",
	RemoveLogonTaskConfirmBody:     "Remove WinTray startup entries and turn off Start at sign-in?\r\nTurn it on again to restore startup entries",
	RemoveLogonTaskDoneBody:        "Startup entries removed; sign-in startup is off",
	RemoveLogonTaskFailedBody:      "Could not remove startup entries: %s",
	LanguageZhLabel:                "中文",
	SettingsTroubleshootTitle:      "Troubleshooting",
	LogsTitle:                      "Logs",
	CleanupRestoreTitle:            "Reset data",
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
	param := formatManagedMode(language, app)
	if app.Schedule.Enabled {
		return fmt.Sprintf(For(language).ManagedScheduleTag, param)
	}
	return param
}

func formatManagedMode(language string, app config.ManagedAppEntry) string {
	msg := For(language)
	if !app.RunOnStartup {
		return msg.ManagedListParamPausedTemplate
	}
	if app.LaunchViaLogonTask {
		return msg.ManagedTaskLaunch
	}
	if app.LaunchHiddenInBackground {
		return msg.ManagedLaunchHidden
	}
	if app.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		if delay := app.TrayBehavior.CloseDelaySeconds; delay > 0 && !config.WaitForLoginBeforeTray(app) {
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
		"empty_exe_path":                 "empty exe path",
		"invalid_exe_path":               "invalid exe path",
		"process_start_failed":           "process start failed",
		"script_association_missing":     "script association missing",
		"startup_check_failed":           "startup check failed",
		"startup_frequency_limit":        "automatic startup limit reached",
		"startup_frequency_check_failed": "automatic startup limit check failed",
		"external_startup_timeout":       "external startup timeout",
		"wechat_login_incomplete":        "wechat login incomplete",
		"qq_login_incomplete":            "qq login incomplete",
		"cancelled":                      "cancelled",
		"started_only":                   "started only",
		"started_hidden":                 "started hidden",
		"already_running_skipped":        "already running skipped",
		"already_running_managed":        "already running managed existing",
		"no_window_managed":              "no window managed",
		"invalid_process_name":           "invalid process name",
		"no_existing_window_managed":     "no existing window managed",
		"managed":                        "managed",
		"managed_existing":               "managed existing",
		"hidden_to_tray":                 "hidden to tray",
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
	case "automatic startup limit reached":
		if Resolve(language) == LangEnUS {
			return "automatic launch limit reached; skipped"
		}
		return "已达到自动运行次数上限，本次跳过"
	case "automatic startup limit check failed":
		if Resolve(language) == LangEnUS {
			return "automatic launch control failed; see the log and save settings again"
		}
		return "自动运行次数控制失败，请查看日志后重新保存设置"
	case "empty exe path":
		if Resolve(language) == LangEnUS {
			return "no program selected; select a program file"
		}
		return "尚未选择程序，请选择程序文件"
	case "invalid exe path":
		if Resolve(language) == LangEnUS {
			return "invalid program path; select the program file again"
		}
		return "程序路径无效，请重新选择程序文件"
	case "process start failed":
		if Resolve(language) == LangEnUS {
			return "could not start the program; open the log for details"
		}
		return "无法启动程序，请打开日志查看详情"
	case "script association missing":
		if Resolve(language) == LangEnUS {
			return "no program opens .ahk files; install AutoHotkey, then double-click the script to check it"
		}
		return "未设置 .ahk 文件的打开方式；请安装 AutoHotkey，并确认双击脚本可运行"
	case "startup check failed":
		if Resolve(language) == LangEnUS {
			return "could not check startup entries; open the log and save the settings again"
		}
		return "无法检查系统自启项，请查看日志后重新保存设置"
	case "external startup timeout":
		if Resolve(language) == LangEnUS {
			return "the program did not start in time; check its startup settings"
		}
		return "等待程序自启动超时，请检查程序的自启设置"
	case "cancelled":
		if Resolve(language) == LangEnUS {
			return "cancelled before launch"
		}
		return "已取消启动"
	case "qq login incomplete":
		if Resolve(language) == LangEnUS {
			return "QQ login is incomplete; complete login in the QQ window"
		}
		return "QQ 尚未完成登录，请在 QQ 窗口中完成登录"
	case "wechat login incomplete":
		if Resolve(language) == LangEnUS {
			return "WeChat login is incomplete; complete login in the WeChat window"
		}
		return "微信登录未完成，请在微信窗口中完成登录"
	case "started only":
		if Resolve(language) == LangEnUS {
			return "program started"
		}
		return "程序已启动"
	case "started hidden":
		if Resolve(language) == LangEnUS {
			return "started in the background"
		}
		return "已后台启动"
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
			return "program window closed to tray"
		}
		return "程序窗口已收起"
	case "invalid process name":
		if Resolve(language) == LangEnUS {
			return "invalid process name"
		}
		return "进程名无效"
	default:
		return message
	}
}
