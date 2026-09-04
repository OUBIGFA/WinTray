//go:build windows

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lxn/walk"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/ipc"
	"wintray/internal/logging"
	"wintray/internal/orchestrator"
	"wintray/internal/startup"
	"wintray/internal/tray"
	"wintray/internal/ui"
	"wintray/internal/update"
	"wintray/internal/version"
)

const (
	appName            = "WinTray"
	singleInstanceName = "WinTray_SingleInstance"
	activationEvent    = "WinTray_ShowMainWindow"
)

func Run(args []string) int {
	if isCleanupRestoreLaunch(args) {
		if err := runCleanupRestoreHeadless(); err != nil {
			return 1
		}
		return 0
	}

	instance, alreadyRunning, err := ipc.Acquire(singleInstanceName)
	if err != nil {
		emitFatalBeforeUI("failed to acquire single-instance lock", err)
		return 1
	}
	defer instance.Close()

	if alreadyRunning {
		if shouldSignalRunningInstance(args) {
			if ipc.TrySignalActivation(activationEvent) {
				return 0
			}
			msg := i18n.For("zh-CN")
			showMessage(msg.AlreadyRunningTitle, msg.AlreadyRunningBody, walk.MsgBoxIconInformation)
		}
		return 0
	}

	settingsPath, settingsPathErr := config.SettingsPathWithError()
	if settingsPathErr != nil {
		emitFatalBeforeUI("failed to resolve settings path", settingsPathErr)
		return 1
	}
	store := config.NewStore(settingsPath)
	settings, settingsErr := store.LoadWithError()

	appDir, appDirErr := config.AppDirWithError()
	if appDirErr != nil {
		emitFatalBeforeUI("failed to resolve app data directory", appDirErr)
		return 1
	}

	logger, err := logging.New(appDir)
	if err != nil {
		emitFatalBeforeUI("failed to initialize logger", err)
		return 1
	}
	defer logger.Close()
	if settingsErr != nil {
		logger.Warn(fmt.Sprintf("load settings failed; defaults kept in memory: %v", settingsErr))
	}

	enumerator := orchestrator.NewWin32WindowEnumerator()
	manager := orchestrator.NewWin32WindowManager()
	orch := orchestrator.NewService(enumerator, manager, logger)
	registrar := startup.NewRegistrar()

	if isAutorunLaunch(args) {
		logger.Info(fmt.Sprintf("autorun mode: run managed apps (exitAfterCompleted=%t)", settings.ExitAfterManagedAppsCompleted))
		if runAutorunMode(context.Background(), orch, settings, logger) {
			return 0
		}
	}

	var trayController *tray.Controller
	var mainWindow *ui.MainWindow
	activation, activationErr := ipc.NewActivationListener(activationEvent)
	if activationErr != nil {
		logger.Warn(fmt.Sprintf("activation listener unavailable: %v", activationErr))
	}
	if activation != nil {
		defer activation.Close()
	}

	cleanupAndRestore := func() {
		if mainWindow == nil || mainWindow.Native() == nil {
			logger.Warn("cleanup requested but main window is unavailable")
			return
		}
		lang := safeLanguage(mainWindow)
		m := i18n.For(lang)
		if walk.MsgBox(mainWindow.Native(), m.CleanupConfirmTitle, m.CleanupConfirmBody, walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
			return
		}

		defaults := config.DefaultSettings()

		if saveErr := store.Save(defaults); saveErr != nil {
			mainWindow.ShowError(m.CleanupFailedTitle, fmt.Sprintf(m.CleanupFailedBody, saveErr))
			return
		}

		ensureRunAtLogon(registrar, defaults, logger)
		if scheduleErr := scheduleAppDataCleanupOnExit(); scheduleErr != nil {
			mainWindow.ShowError(m.CleanupFailedTitle, fmt.Sprintf(m.CleanupFailedBody, scheduleErr))
			return
		}

		mainWindow.ShowInfo(m.CleanupDoneTitle, m.CleanupDoneBody)
		mainWindow.RequestExplicitClose()
	}

	mainWindow, err = ui.NewMainWindow(settings, ui.Callbacks{
		OnSave: func(s config.Settings) {
			if saveErr := store.Save(s); saveErr != nil {
				logger.Warn(fmt.Sprintf("save settings failed: %v", saveErr))
			}
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
		OnCleanupRestore: cleanupAndRestore,
		OnLaunchNow: func(entry config.ManagedAppEntry) {
			current := mainWindow.Settings()
			retrySeconds := current.CloseWindowRetrySeconds
			language := current.Language
			go func() {
				result := orch.StartNow(context.Background(), entry, retrySeconds)
				m := i18n.For(language)
				mainWindow.SetLaunchNowBusy(false)
				if result.Managed {
					mainWindow.ShowInfo(m.WindowTitle, fmt.Sprintf(m.LaunchNowDoneBody, result.AppName))
					return
				}
				detail := i18n.TranslateResultCode(language, string(result.Code))
				if detail == "" {
					detail = i18n.TranslateResultMessage(language, result.Message)
				}
				if i18n.IsLikelyPermissionCode(string(result.Code)) || i18n.IsLikelyPermissionIssue(result.Message) {
					detail += " " + m.StatusPermissionHint
				}
				logger.Warn(fmt.Sprintf("launch now failed: %s %s", result.AppName, result.Message))
				mainWindow.ShowError(m.WindowTitle, fmt.Sprintf(m.StatusLaunchFailTemplate, result.AppName, detail))
			}()
		},
		OnCheckUpdate: func() {
			language := safeLanguage(mainWindow)
			go func() {
				result, checkErr := update.Check(context.Background(), version.Number)
				m := i18n.For(language)
				mainWindow.SetCheckUpdateBusy(false)
				if checkErr != nil {
					logger.Warn(fmt.Sprintf("update check failed: %v", checkErr))
					mainWindow.ShowError(m.UpdateTitle, fmt.Sprintf(m.UpdateFailedBody, checkErr))
					return
				}
				logger.Info(fmt.Sprintf("update check: current=%s latest=%s newer=%t", result.Current, result.Latest, result.HasUpdate))
				if !result.HasUpdate {
					mainWindow.ShowInfo(m.UpdateTitle, fmt.Sprintf(m.UpdateLatestBody, result.Latest))
					return
				}
				if mainWindow.Confirm(m.UpdateTitle, fmt.Sprintf(m.UpdateAvailableBody, result.Latest, result.Current)) {
					openRepository(result.PageURL, logger)
				}
			}()
		},
		OnOpenRepository: func() {
			openRepository(version.RepositoryURL, logger)
		},
		OnExit: func() {
			mainWindow.RequestExplicitClose()
		},
	})
	if err != nil {
		logger.Error(fmt.Sprintf("create main window failed: %v", err))
		emitFatalWithLog(settings.Language, "failed to create main window", err)
		return 1
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
		func() {
			if openErr := openLogLocation(); openErr != nil {
				lang := safeLanguage(mainWindow)
				m := i18n.For(lang)
				mainWindow.ShowError(m.WindowTitle, fmt.Sprintf("%s: %v", m.StatusOpenLogsFailed, openErr))
			}
		},
		cleanupAndRestore,
		func() { mainWindow.RequestExplicitClose() },
		settings.Language,
	)
	if err != nil {
		logger.Error(fmt.Sprintf("create tray failed: %v", err))
		emitFatalWithLog(settings.Language, "failed to create system tray", err)
		return 1
	}
	defer trayController.Dispose()

	showMainWindow := shouldShowMainWindowForSettings(args, settings)
	if showMainWindow {
		mainWindow.ShowMainWindow()
	} else {
		mainWindow.HideMainWindow()
	}

	exitCode := mainWindow.Run()

	return exitCode
}

// runAutorunMode runs the logon task set and reports whether the process should
// exit immediately. When the setting is disabled, Run continues into the
// normal tray/UI path after the tasks complete.
func runAutorunMode(ctx context.Context, orch *orchestrator.Service, settings config.Settings, logger *logging.Logger) bool {
	runManagedApps(ctx, orch, nil, settings, false, logger)
	return settings.ExitAfterManagedAppsCompleted
}

func runManagedApps(ctx context.Context, orch *orchestrator.Service, mainWindow *ui.MainWindow, settings config.Settings, autoExit bool, logger *logging.Logger) {
	msg := i18n.For(settings.Language)
	managedEntries := make([]config.ManagedAppEntry, 0, len(settings.ManagedApps))
	for _, entry := range settings.ManagedApps {
		if config.ShouldLaunchViaWinTray(entry) {
			managedEntries = append(managedEntries, entry)
		}
	}

	summaries := make([]string, len(managedEntries))
	var wg sync.WaitGroup
	for i, entry := range managedEntries {
		i := i
		entry := entry
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := processManagedEntry(ctx, orch, settings, entry, logger)

			detail := i18n.TranslateResultCode(settings.Language, string(result.Code))
			if detail == "" {
				detail = i18n.TranslateResultMessage(settings.Language, result.Message)
			}
			if !result.Managed && (i18n.IsLikelyPermissionCode(string(result.Code)) || i18n.IsLikelyPermissionIssue(result.Message)) {
				detail += " " + msg.StatusPermissionHint
			}
			summaries[i] = fmt.Sprintf(msg.RunSummaryLine, result.AppName, detail)
		}()
	}
	wg.Wait()

	if len(managedEntries) == 0 {
		summaries = append(summaries, msg.RunSummaryNone)
	}
	for _, line := range summaries {
		logger.Info(fmt.Sprintf("managed summary: %s", line))
	}

	hadTasks := len(managedEntries) > 0
	if ctx.Err() == nil && autoExit && hadTasks && mainWindow != nil {
		mainWindow.RequestExplicitClose()
	}
}

func processManagedEntry(ctx context.Context, orch *orchestrator.Service, settings config.Settings, entry config.ManagedAppEntry, logger *logging.Logger) orchestrator.Result {
	if entry.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		existing := orch.HideExisting(ctx, entry, settings.CloseWindowRetrySeconds)
		if existing.Managed {
			return existing
		}
	}

	result := orch.StartAndManage(ctx, entry, settings.CloseWindowRetrySeconds)
	if !result.Managed {
		logger.Warn(fmt.Sprintf("managed startup app failed: %s %s", result.AppName, result.Message))
	}
	return result
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

func scheduleAppDataCleanupOnExit() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.TrimSpace(exePath) == "" {
		return fmt.Errorf("empty executable path")
	}

	cmd := exec.Command(exePath, "--cleanup-restore")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func runCleanupRestoreHeadless() error {
	appDir, err := config.AppDirWithError()
	if err != nil {
		return err
	}

	for attempt := 0; attempt < 30; attempt++ {
		removeErr := os.RemoveAll(appDir)
		if removeErr == nil {
			return nil
		}
		if os.IsNotExist(removeErr) {
			return nil
		}
		message := strings.ToLower(removeErr.Error())
		if strings.Contains(message, "cannot find") || strings.Contains(message, "not found") {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return os.RemoveAll(appDir)
}

func emitFatalBeforeUI(message string, err error) {
	emitFatalWithLog("zh-CN", message, err)
}

func emitFatalWithLog(language, message string, err error) {
	msg := i18n.For(language)
	logPath, pathErr := config.LogPathWithError()
	if pathErr != nil {
		logPath = "unavailable"
	}
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

// openRepository hands a project URL to the default browser.
func openRepository(url string, logger *logging.Logger) {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		logger.Warn(fmt.Sprintf("open repository failed: %v", err))
	}
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
