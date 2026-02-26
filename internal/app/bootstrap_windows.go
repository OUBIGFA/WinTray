//go:build windows

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/lxn/walk"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/ipc"
	"wintray/internal/lifecycle"
	"wintray/internal/logging"
	"wintray/internal/orchestrator"
	"wintray/internal/startup"
	"wintray/internal/tray"
	"wintray/internal/ui"
)

const (
	appName            = "WinTray"
	singleInstanceName = "WinTray_SingleInstance"
	activationEvent    = "WinTray_ShowMainWindow"
)

func Run(args []string) {
	instance, alreadyRunning, err := ipc.Acquire(singleInstanceName)
	if err != nil {
		emitFatalBeforeUI("failed to acquire single-instance lock", err)
		return
	}
	defer instance.Close()

	if alreadyRunning {
		if shouldSignalRunningInstance(args) {
			if ipc.TrySignalActivation(activationEvent) {
				return
			}
			msg := i18n.For("zh-CN")
			showMessage(msg.AlreadyRunningTitle, msg.AlreadyRunningBody, walk.MsgBoxIconInformation)
		}
		return
	}

	settingsPath, settingsPathErr := config.SettingsPathWithError()
	if settingsPathErr != nil {
		emitFatalBeforeUI("failed to resolve settings path", settingsPathErr)
		return
	}
	_ = config.TryMigrateFromWinTray(settingsPath)
	store := config.NewStore(settingsPath)
	settings := store.Load()

	appDir, appDirErr := config.AppDirWithError()
	if appDirErr != nil {
		emitFatalBeforeUI("failed to resolve app data directory", appDirErr)
		return
	}

	logger, err := logging.New(appDir)
	if err != nil {
		emitFatalBeforeUI("failed to initialize logger", err)
		return
	}
	defer logger.Close()

	enumerator := orchestrator.NewWin32WindowEnumerator()
	manager := orchestrator.NewWin32WindowManager()
	orch := orchestrator.NewService(enumerator, manager, logger)
	registrar := startup.NewRegistrar()

	var (
		mu     sync.Mutex
		latest = settings
	)

	var trayController *tray.Controller
	activation, activationErr := ipc.NewActivationListener(activationEvent)
	if activationErr != nil {
		logger.Warn(fmt.Sprintf("activation listener unavailable: %v", activationErr))
	}
	if activation != nil {
		defer activation.Close()
	}

	var mainWindow *ui.MainWindow
	mainWindow, err = ui.NewMainWindow(settings, ui.Callbacks{
		OnSave: func(s config.Settings) {
			mu.Lock()
			latest = s
			mu.Unlock()
			_ = store.Save(s)
			ensureRunAtLogon(registrar, s, logger)
			if trayController != nil {
				trayController.SetLanguage(s.Language)
			}
		},
		OnOpenLogs: func() {
			if openErr := openLogLocation(); openErr != nil {
				lang := safeLanguage(mainWindow)
				m := i18n.For(lang)
				mainWindow.ShowError(m.WindowTitle, fmt.Sprintf("%s: %v", m.StatusOpenLogsFailed, openErr))
			}
		},
		OnExit: func() {
			mainWindow.RequestExplicitClose()
		},
	})
	if err != nil {
		logger.Error(fmt.Sprintf("create main window failed: %v", err))
		emitFatalWithLog(settings.Language, "failed to create main window", err)
		return
	}
	if activation != nil {
		activation.Start(func() {
			mainWindow.ShowMainWindow()
		})
	}

	ensureRunAtLogon(registrar, settings, logger)

	trayController, err = tray.New(
		mainWindow.Native(),
		mainWindow.ShowMainWindow,
		func() { mainWindow.RequestExplicitClose() },
		config.LogPathWithError,
		func(detail string) {
			m := i18n.For(safeLanguage(mainWindow))
			mainWindow.ShowError(m.WindowTitle, fmt.Sprintf("%s: %s", m.StatusOpenLogsFailed, detail))
		},
		settings.Language,
	)
	if err != nil {
		logger.Error(fmt.Sprintf("create tray failed: %v", err))
		emitFatalWithLog(settings.Language, "failed to create system tray", err)
		return
	}
	defer trayController.Dispose()

	if shouldShowMainWindow(args) {
		mainWindow.ShowMainWindow()
	} else {
		mainWindow.HideMainWindow()
	}

	if isAutorunLaunch(args) {
		go runManagedApps(context.Background(), orch, mainWindow, settings, settings.ExitAfterManagedAppsCompleted, logger)
	}

	exitCode := mainWindow.Run()

	mu.Lock()
	finalSettings := latest
	mu.Unlock()
	_ = store.Save(finalSettings)
	os.Exit(exitCode)
}

func runManagedApps(ctx context.Context, orch *orchestrator.Service, mainWindow *ui.MainWindow, settings config.Settings, autoExit bool, logger *logging.Logger) {
	msg := i18n.For(settings.Language)
	startupEntries := make([]config.ManagedAppEntry, 0)
	hideOnlyEntries := make([]config.ManagedAppEntry, 0)
	for _, entry := range settings.ManagedApps {
		if entry.RunOnStartup {
			startupEntries = append(startupEntries, entry)
			continue
		}
		if entry.TrayBehavior.AutoMinimizeAndHideOnLaunch {
			hideOnlyEntries = append(hideOnlyEntries, entry)
		}
	}

	summaries := make([]string, 0, len(startupEntries)+len(hideOnlyEntries))

	for _, entry := range startupEntries {
		result := orch.StartAndManage(ctx, entry, settings.CloseWindowRetrySeconds)
		if !result.Managed {
			logger.Warn(fmt.Sprintf("managed startup app failed: %s %s", result.AppName, result.Message))
			hint := ""
			if i18n.IsLikelyPermissionIssue(result.Message) {
				hint = " " + msg.StatusPermissionHint
			}
			detail := i18n.TranslateResultMessage(settings.Language, result.Message) + hint
			summaries = append(summaries, fmt.Sprintf(msg.RunSummaryLine, result.AppName, detail))
			continue
		}
		detail := i18n.TranslateResultMessage(settings.Language, result.Message)
		summaries = append(summaries, fmt.Sprintf(msg.RunSummaryLine, result.AppName, detail))
	}

	for _, entry := range hideOnlyEntries {
		result := orch.HideExisting(ctx, entry, settings.CloseWindowRetrySeconds)
		if !result.Managed {
			logger.Warn(fmt.Sprintf("managed existing app failed: %s %s", result.AppName, result.Message))
			hint := ""
			if i18n.IsLikelyPermissionIssue(result.Message) {
				hint = " " + msg.StatusPermissionHint
			}
			detail := i18n.TranslateResultMessage(settings.Language, result.Message) + hint
			summaries = append(summaries, fmt.Sprintf(msg.RunSummaryLine, result.AppName, detail))
			continue
		}
		detail := i18n.TranslateResultMessage(settings.Language, result.Message)
		summaries = append(summaries, fmt.Sprintf(msg.RunSummaryLine, result.AppName, detail))
	}

	if len(summaries) == 0 {
		summaries = append(summaries, msg.RunSummaryNone)
	}
	for _, line := range summaries {
		logger.Info(fmt.Sprintf("managed summary: %s", line))
	}

	hadTasks := len(startupEntries) > 0 || len(hideOnlyEntries) > 0
	lifecycle.ExitIfCompleted(ctx, autoExit, hadTasks, func() {
		mainWindow.RequestExplicitClose()
	})
}

func ensureRunAtLogon(registrar *startup.Registrar, settings config.Settings, logger *logging.Logger) {
	exePath, err := os.Executable()
	if err != nil || exePath == "" {
		logger.Warn("unable to resolve executable path for run-at-logon")
		return
	}
	command := fmt.Sprintf("\"%s\" --autorun", exePath)
	if settings.StartMinimizedToTray {
		command = fmt.Sprintf("\"%s\" --background --autorun", exePath)
	}
	if err = registrar.SetEnabled(appName, command, settings.RunAtLogon); err != nil {
		logger.Warn(fmt.Sprintf("set run-at-logon failed: %v", err))
	}
}

func emitFatalBeforeUI(message string, err error) {
	emitFatalWithLog("zh-CN", message, err)
}

func emitFatalWithLog(language, message string, err error) {
	msg := i18n.For(language)
	logPath := config.LogPath()
	body := fmt.Sprintf(msg.FatalStartupBodyTemplate, fmt.Sprintf("%s: %v", message, err), logPath)
	showMessage(msg.FatalStartupTitle, body, walk.MsgBoxIconError)
}

func showMessage(title, body string, style walk.MsgBoxStyle) {
	_ = walk.MsgBox(nil, title, body, style)
}

func safeLanguage(mainWindow *ui.MainWindow) string {
	if mainWindow == nil {
		return string(i18n.LangZhCN)
	}
	return mainWindow.Settings().Language
}

func openLogLocation() error {
	logPath, err := config.LogPathWithError()
	if err != nil {
		return err
	}
	if err = exec.Command("explorer", "/select,", logPath).Start(); err != nil {
		return err
	}
	return nil
}
