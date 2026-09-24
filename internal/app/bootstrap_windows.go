//go:build windows

package app

import (
	"context"
	"errors"
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
	if isHostLaunch(args) {
		return runHostMode(args)
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

	registrar := startup.NewRegistrar()
	return runMainSession(args, settings, sessionServices{
		store: store, logger: logger, activationName: activationEvent,
		setRunAtLogon: func(s config.Settings) { ensureRunAtLogon(registrar, s, logger) },
	})
}

type sessionServices struct {
	store          *config.Store
	logger         *logging.Logger
	activationName string
	setRunAtLogon  func(config.Settings)
}

// runMainSession owns UI, startup workers and hosted icons for one instance.
// Registry registration and data paths are supplied by the composition root.
func runMainSession(args []string, settings config.Settings, services sessionServices) int {
	store, logger := services.store, services.logger
	enumerator := orchestrator.NewWin32WindowEnumerator()
	manager := orchestrator.NewWin32WindowManager()
	orch := orchestrator.NewService(enumerator, manager, logger)
	launchCtx, cancelLaunches := context.WithCancel(context.Background())
	var launchWG sync.WaitGroup
	var trayController *tray.Controller
	var mainWindow *ui.MainWindow
	host := tray.NewHost(settings.Language, logger, orchestrator.FindConsoleWindow)
	state := residencyState{autorun: isAutorunLaunch(args), startupPending: isAutorunLaunch(args)}
	state.settingsOpen = shouldShowMainWindowForSettings(args, settings)
	quitting := false
	defer func() {
		cancelLaunches()
		launchWG.Wait()
		host.RestoreAll()
	}()

	// Keep pumping UI messages while outstanding work is cancelled and hidden
	// windows are recovered. Closing the message loop first loses hand-offs.
	requestExit := func() {
		if quitting {
			return
		}
		quitting = true
		logger.Info("shutdown requested: cancelling pending tasks")
		cancelLaunches()
		mainWindow.Native().SetEnabled(false)
		// Dispose the icon before closing the window so an open shell menu is
		// dismissed immediately instead of being left behind while workers stop.
		if trayController != nil {
			trayController.Dispose()
		}
		mainWindow.RequestExplicitClose()
	}
	applyResidency := func() {
		if quitting || mainWindow == nil {
			return
		}
		exitAfter := mainWindow.Settings().ExitAfterManagedAppsCompleted
		if trayController != nil {
			if err := trayController.SetVisible(!state.hideMainIcon(exitAfter)); err != nil {
				logger.Warn(fmt.Sprintf("update main tray visibility failed: %v", err))
			}
		}
		if state.shouldExit(exitAfter, host.Count()) {
			requestExit()
		}
	}
	host.SetOnEmpty(applyResidency)
	openSettings := func() {
		if quitting {
			return
		}
		state.settingsOpen = true
		applyResidency()
		mainWindow.ShowMainWindow()
	}

	// Called by launch workers. Host icons belong to this process and are
	// created on its UI thread, not in additional WinTray processes.
	handOffToHost := func(hw tray.HostedWindow) {
		restore := func() { tray.RestoreWindow(hw, orchestrator.FindConsoleWindow, logger) }
		for attempt := 1; attempt <= hostAddAttempts; attempt++ {
			done := make(chan struct{})
			var handled sync.Once
			var hostErr error
			mainWindow.Synchronize(func() {
				defer close(done)
				handled.Do(func() {
					if hostErr = launchCtx.Err(); hostErr == nil {
						hostErr = host.TryAdd(hw)
					}
				})
			})
			select {
			case <-done:
				if hostErr == nil {
					return
				}
			case <-launchCtx.Done():
				// Prevent a late callback from adopting a restored window.
				handled.Do(func() {})
				restore()
				return
			}
			logger.Warn(fmt.Sprintf("tray hosting attempt %d failed: %s pid=%d %v", attempt, hw.Name, hw.ProcessID, hostErr))
			if errors.Is(hostErr, tray.ErrHostedProcessUnavailable) || attempt == hostAddAttempts {
				break
			}
			// Explorer may not accept icons yet at logon. Retry off the UI
			// thread so other launches, activation and exit remain responsive.
			timer := time.NewTimer(hostAddDelay)
			select {
			case <-timer.C:
			case <-launchCtx.Done():
				timer.Stop()
				restore()
				return
			}
		}
		restore()
	}

	activation, activationErr := ipc.NewActivationListener(services.activationName)
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

		services.setRunAtLogon(defaults)
		if scheduleErr := scheduleAppDataCleanupOnExit(); scheduleErr != nil {
			mainWindow.ShowError(m.CleanupFailedTitle, fmt.Sprintf(m.CleanupFailedBody, scheduleErr))
			return
		}

		mainWindow.ShowInfo(m.CleanupDoneTitle, m.CleanupDoneBody)
		requestExit()
	}

	var err error
	mainWindow, err = ui.NewMainWindow(settings, ui.Callbacks{
		OnSave: func(s config.Settings) {
			if saveErr := store.Save(s); saveErr != nil {
				logger.Warn(fmt.Sprintf("save settings failed: %v", saveErr))
			}
			services.setRunAtLogon(s)
			if trayController != nil {
				trayController.SetLanguage(s.Language)
			}
			host.SetLanguage(s.Language)
			applyResidency()
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
			if quitting || state.startupPending {
				return
			}
			state.manualLaunches++
			current := mainWindow.Settings()
			retrySeconds := current.CloseWindowRetrySeconds
			language := current.Language
			launchWG.Add(1)
			go func() {
				defer launchWG.Done()
				result := orch.StartNow(launchCtx, entry, retrySeconds)
				if hw, ok := hostedFromResult(entry, result); ok {
					handOffToHost(hw)
				}
				mainWindow.Synchronize(func() {
					state.manualLaunches--
					applyResidency()
				})
				if launchCtx.Err() != nil {
					return
				}
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
			if quitting {
				return
			}
			language := safeLanguage(mainWindow)
			launchWG.Add(1)
			go func() {
				defer launchWG.Done()
				result, checkErr := update.Check(launchCtx, version.Number)
				if launchCtx.Err() != nil {
					return
				}
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
				if mainWindow.ConfirmContext(launchCtx, m.UpdateTitle, fmt.Sprintf(m.UpdateAvailableBody, result.Latest, result.Current)) {
					openRepository(result.PageURL, logger)
				}
			}()
		},
		OnOpenRepository: func() {
			openRepository(version.RepositoryURL, logger)
		},
		OnExit: requestExit,
		OnHideToTray: func() {
			state.settingsOpen = false
			applyResidency()
		},
	})
	if err != nil {
		logger.Error(fmt.Sprintf("create main window failed: %v", err))
		emitFatalWithLog(settings.Language, "failed to create main window", err)
		return 1
	}
	if activation != nil {
		activation.Start(func() {
			mainWindow.Synchronize(openSettings)
		})
	}

	services.setRunAtLogon(settings)

	trayController, err = tray.New(
		mainWindow.Native(),
		openSettings,
		requestExit,
		settings.Language,
		!state.hideMainIcon(settings.ExitAfterManagedAppsCompleted),
	)
	if err != nil {
		logger.Error(fmt.Sprintf("create tray failed: %v", err))
		emitFatalWithLog(settings.Language, "failed to create system tray", err)
		return 1
	}
	defer trayController.Dispose()

	if state.settingsOpen {
		mainWindow.ShowMainWindow()
	} else {
		mainWindow.HideMainWindow()
	}

	if state.autorun {
		// The message loop must exist before tasks can hand it hidden windows.
		mainWindow.SetLaunchNowBusy(true)
		mainWindow.Native().Starting().Attach(func() {
			logger.Info(fmt.Sprintf("autorun mode: run managed apps (exitAfterCompleted=%t)", settings.ExitAfterManagedAppsCompleted))
			launchWG.Add(1)
			go func() {
				defer launchWG.Done()
				runManagedApps(launchCtx, orch, settings, logger, handOffToHost)
				mainWindow.Synchronize(func() {
					state.startupPending = false
					mainWindow.SetLaunchNowBusy(false)
					applyResidency()
				})
			}()
		})
	}

	return mainWindow.Run()
}

// runManagedApps hands hidden windows to their hosts as soon as each task
// finishes. Waiting for the whole batch would leave early programs unreachable
// behind long close delays or a missing external startup later in the list.
func runManagedApps(ctx context.Context, orch *orchestrator.Service, settings config.Settings, logger *logging.Logger, onHosted func(tray.HostedWindow)) {
	msg := i18n.For(settings.Language)
	results := orch.StartManagedApps(ctx, settings, func(entry config.ManagedAppEntry, result orchestrator.Result) {
		if !result.Managed {
			logger.Warn(fmt.Sprintf("managed startup app failed: %s %s", result.AppName, result.Message))
		}
		if hw, ok := hostedFromResult(entry, result); ok {
			onHosted(hw)
		}
	})
	if len(results) == 0 {
		logger.Info(fmt.Sprintf("managed summary: %s", msg.RunSummaryNone))
	}
	for _, result := range results {
		detail := i18n.TranslateResultCode(settings.Language, string(result.Code))
		if detail == "" {
			detail = i18n.TranslateResultMessage(settings.Language, result.Message)
		}
		if !result.Managed && (i18n.IsLikelyPermissionCode(string(result.Code)) || i18n.IsLikelyPermissionIssue(result.Message)) {
			detail += " " + msg.StatusPermissionHint
		}
		logger.Info(fmt.Sprintf("managed summary: "+msg.RunSummaryLine, result.AppName, detail))
	}
}

// hostedFromResult converts a result whose window WinTray hid itself into a
// tray host entry.
func hostedFromResult(entry config.ManagedAppEntry, result orchestrator.Result) (tray.HostedWindow, bool) {
	if result.Hidden == nil {
		return tray.HostedWindow{}, false
	}
	return tray.HostedWindow{
		Name:      entry.Name,
		ExePath:   entry.ExePath,
		Handle:    result.Hidden.Handle,
		ProcessID: result.Hidden.ProcessID,
	}, true
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
