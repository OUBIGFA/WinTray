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
	if isCleanupRestoreLaunch(args) {
		_ = runCleanupRestoreHeadless()
		return
	}

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
	migrationErr := config.TryMigrateFromWinTray(settingsPath)
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
	if migrationErr != nil {
		logger.Warn(fmt.Sprintf("settings migration failed: %v", migrationErr))
	}

	enumerator := orchestrator.NewWin32WindowEnumerator()
	manager := orchestrator.NewWin32WindowManager()
	orch := orchestrator.NewService(enumerator, manager, logger)
	registrar := startup.NewRegistrar()

	var (
		mu     sync.Mutex
		latest = settings
	)

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
		mu.Lock()
		latest = defaults
		mu.Unlock()

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
			mu.Lock()
			latest = s
			mu.Unlock()
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

	managedCtx, managedCancel := context.WithCancel(context.Background())
	defer managedCancel()

	if isAutorunLaunch(args) {
		mu.Lock()
		snapshot := latest
		mu.Unlock()
		go runManagedApps(managedCtx, orch, mainWindow, snapshot, snapshot.ExitAfterManagedAppsCompleted, logger)
	}

	exitCode := mainWindow.Run()
	managedCancel()

	mu.Lock()
	finalSettings := latest
	mu.Unlock()
	if saveErr := store.Save(finalSettings); saveErr != nil {
		logger.Warn(fmt.Sprintf("save settings on exit failed: %v", saveErr))
	}
	os.Exit(exitCode)
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

			detail := i18n.TranslateResultMessage(settings.Language, result.Message)
			if !result.Managed && i18n.IsLikelyPermissionIssue(result.Message) {
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
	lifecycle.ExitIfCompleted(ctx, autoExit, hadTasks, func() {
		mainWindow.RequestExplicitClose()
	})
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
