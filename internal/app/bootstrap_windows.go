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
	"wintray/internal/traybox"
	"wintray/internal/ui"
	"wintray/internal/update"
	"wintray/internal/version"
)

const (
	appName            = "WinTray"
	singleInstanceName = "WinTray_SingleInstance"
	activationEvent    = "WinTray_ShowMainWindow"
	// readyMarker exists while a WinTray session is up and collecting tray
	// icons. Program logon tasks wait for it before launching.
	readyMarker = "WinTray_Ready"
)

// readyWait bounds that wait: a WinTray that failed to start must not keep
// the programs from starting at all.
var readyWait = 2 * time.Minute

func Run(args []string) int {
	if code := runAppTaskHelper(args); code >= 0 {
		return code
	}
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

	registrar := startup.NewRegistrar(appName)
	logon := newLatestOnly(func(s config.Settings) { ensureRunAtLogon(registrar, s, logger) })
	appTasks, appTasksErr := startup.NewAppTasks()
	if appTasksErr != nil {
		logger.Warn(fmt.Sprintf("program logon tasks unavailable: %v", appTasksErr))
		appTasks = nil
	}
	if appTasks != nil {
		appTasks.Log = logger.Info
	}
	appTaskWorker := newLatestOnly(func(r appTaskSyncRequest) { ensureAppTasks(appTasks, r, logger) })
	// Let a registration change requested just before exit reach the system.
	defer func() {
		logon.Wait()
		appTaskWorker.Wait()
	}()
	return runMainSession(args, settings, sessionServices{
		store: store, logger: logger, activationName: activationEvent,
		setRunAtLogon: logon.Request,
		syncAppTasks: func(r appTaskSyncRequest) {
			appTaskWorker.Request(r.snapshot())
		},
		restoreAppStartup: func() error {
			appTaskWorker.Wait()
			if appTasks == nil {
				return appTasksErr
			}
			return appTasks.Sync(config.Settings{}, true)
		},
		removeLogon: func() error {
			// Queued saves run first so none of them re-creates the task.
			logon.Wait()
			return registrar.Remove()
		},
	})
}

// appTaskSyncRequest carries what one program-task reconciliation needs: the
// settings to apply and whether it may ask for one elevated confirmation.
type appTaskSyncRequest struct {
	settings      config.Settings
	allowElevated bool
	onError       func(error)
}

func (r appTaskSyncRequest) snapshot() appTaskSyncRequest {
	// The UI edits this slice in place while reconciliation runs in the
	// background; a shallow Settings copy is not a stable startup request.
	r.settings.ManagedApps = append([]config.ManagedAppEntry(nil), r.settings.ManagedApps...)
	return r
}

type sessionServices struct {
	store             *config.Store
	logger            *logging.Logger
	activationName    string
	setRunAtLogon     func(config.Settings)
	syncAppTasks      func(appTaskSyncRequest)
	restoreAppStartup func() error
	removeLogon       func() error
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
	selfPath, _ := os.Executable()
	box := traybox.NewBox(selfPath, logger)
	state := residencyState{autorun: isAutorunLaunch(args), startupPending: isAutorunLaunch(args)}
	state.settingsOpen = shouldShowMainWindowForSettings(args, settings)
	quitting := false
	defer func() {
		cancelLaunches()
		launchWG.Wait()
		host.RestoreAll()
		trayController.Dispose()
		if err := box.Close(); err != nil {
			logger.Warn(fmt.Sprintf("restore collected tray icons on shutdown failed: %v", err))
		}
	}()

	// Keep pumping UI messages while outstanding work is cancelled and hidden
	// windows are recovered. Closing the message loop first loses hand-offs.
	requestExit := func() {
		if quitting {
			return
		}
		if err := box.Close(); err != nil {
			logger.Warn(fmt.Sprintf("restore collected tray icons before exit failed: %v", err))
			mainWindow.ShowError(i18n.For(safeLanguage(mainWindow)).TrayBoxFailedTitle, err.Error())
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
		current := mainWindow.Settings()
		state.boxActive = len(config.CollectedTrayIconPaths(current)) > 0
		exitAfter := exitsAfterStartup(current)
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
	// refreshRunLimits ends programs past their scheduled run time and keeps
	// WinTray while others still have to be ended. It returns when to look
	// again.
	refreshRunLimits := func() time.Duration {
		wait := runLimitPoll
		if quitting || mainWindow == nil {
			return wait
		}
		check := orch.EndOverduePrograms(mainWindow.Settings().ManagedApps, time.Now())
		state.runLimitsPending = check.Running > 0
		if !check.Next.IsZero() {
			wait = min(wait, max(time.Until(check.Next), time.Second))
		}
		applyResidency()
		return wait
	}
	openSettings := func() {
		if quitting {
			return
		}
		// Reopening settings ends background mode; hiding the window again
		// then keeps WinTray's own icon unless the logon exit option applies.
		state.silent = false
		state.settingsOpen = true
		applyResidency()
		mainWindow.ShowMainWindow()
	}
	// runSilently hides settings and WinTray's own icon but keeps hosted
	// program icons. Unlike exit, hidden windows stay hidden; the process ends
	// once nothing is hosted or still being launched.
	runSilently := func() {
		if quitting {
			return
		}
		logger.Info(fmt.Sprintf("silent background mode requested: hosted=%d", host.Count()))
		state.silent = true
		state.settingsOpen = false
		mainWindow.HideMainWindow()
		applyResidency()
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

		// Restore migrated startup states before any backup/settings data can
		// be erased. A failed restore must leave the recovery journal intact.
		if services.restoreAppStartup != nil {
			if restoreErr := services.restoreAppStartup(); restoreErr != nil {
				mainWindow.ShowError(m.CleanupFailedTitle, fmt.Sprintf(m.CleanupFailedBody, restoreErr))
				return
			}
		}
		if resetErr := resetSettings(store, func() error { return box.SetPaths(nil) }); resetErr != nil {
			logger.Warn(fmt.Sprintf("reset settings failed: %v", resetErr))
			mainWindow.ShowError(m.CleanupFailedTitle, fmt.Sprintf(m.CleanupFailedBody, resetErr))
			return
		}

		services.setRunAtLogon(config.Settings{RunAtLogon: false})
		services.syncAppTasks(appTaskSyncRequest{settings: config.Settings{}, allowElevated: true})
		if scheduleErr := scheduleAppDataCleanupOnExit(); scheduleErr != nil {
			mainWindow.ShowError(m.CleanupFailedTitle, fmt.Sprintf(m.CleanupFailedBody, scheduleErr))
			return
		}

		mainWindow.ShowInfo(m.CleanupDoneTitle, m.CleanupDoneBody)
		requestExit()
	}

	removeLogon := func() {
		if quitting || services.removeLogon == nil {
			return
		}
		m := i18n.For(safeLanguage(mainWindow))
		if walk.MsgBox(mainWindow.Native(), m.RemoveLogonTaskTitle, m.RemoveLogonTaskConfirmBody, walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
			return
		}
		// Turning the switch off first keeps the next start from registering
		// the task again.
		mainWindow.TurnOffRunAtLogon()
		launchWG.Add(1)
		go func() {
			defer launchWG.Done()
			if removeErr := services.removeLogon(); removeErr != nil {
				logger.Warn(fmt.Sprintf("remove logon task failed: %v", removeErr))
				mainWindow.ShowError(m.RemoveLogonTaskTitle, fmt.Sprintf(m.RemoveLogonTaskFailedBody, removeErr))
				return
			}
			logger.Info("logon task and Run entry removed on request")
			mainWindow.ShowInfo(m.RemoveLogonTaskTitle, m.RemoveLogonTaskDoneBody)
		}()
	}

	var err error
	mainWindow, err = ui.NewMainWindow(settings, ui.Callbacks{
		OnSave: func(s config.Settings) {
			if saveErr := store.Save(s); saveErr != nil {
				logger.Warn(fmt.Sprintf("save settings failed: %v", saveErr))
				mainWindow.ShowError(i18n.For(s.Language).WindowTitle, saveErr.Error())
				return // do not migrate system startup from unpersisted settings
			}
			services.setRunAtLogon(s)
			// A task change made in the UI may ask for one elevated
			// confirmation; the background sync at startup never does.
			services.syncAppTasks(appTaskSyncRequest{
				settings: s, allowElevated: true,
				onError: func(err error) {
					m := i18n.For(s.Language)
					mainWindow.ShowError(m.ManagedTaskFailedTitle, fmt.Sprintf(m.ManagedTaskFailedBody, formatAppTaskError(s.Language, err)))
				},
			})
			if trayController != nil {
				trayController.SetLanguage(s.Language)
			}
			host.SetLanguage(s.Language)
			applyResidency()
		},
		OnToggleTrayBox: func(id string, on bool) error {
			before := mainWindow.Settings()
			next, err := traySelectionChange(before, id, on)
			if err != nil {
				return err
			}
			// Collect or release the icons first; a failed save hands the
			// previous selection back to the box.
			if err := box.SetPaths(config.CollectedTrayIconPaths(next)); err != nil {
				return err
			}
			if err := store.Save(next); err != nil {
				if restoreErr := box.SetPaths(config.CollectedTrayIconPaths(before)); restoreErr != nil {
					logger.Warn(fmt.Sprintf("restore tray box selection failed: %v", restoreErr))
				}
				return err
			}
			mainWindow.SetCollectedTrayIcon(id, on)
			applyResidency()
			return nil
		},
		OnOpenLogs: func() {
			if openErr := openLogLocation(); openErr != nil {
				lang := safeLanguage(mainWindow)
				m := i18n.For(lang)
				mainWindow.ShowError(m.WindowTitle, fmt.Sprintf("%s: %v", m.StatusOpenLogsFailed, openErr))
			}
		},
		OnCleanupRestore: cleanupAndRestore,
		OnRemoveLogon:    removeLogon,
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
		OnExit:        requestExit,
		OnRunSilently: runSilently,
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
	// Heal program tasks without prompting: a task missing an elevated
	// confirmation waits for the next save, which is allowed to ask for one.
	services.syncAppTasks(appTaskSyncRequest{settings: settings, allowElevated: false})

	// Collect icons before programs are launched, so their icons never
	// appear on the taskbar.
	if boxErr := box.SetPaths(config.CollectedTrayIconPaths(settings)); boxErr != nil {
		logger.Warn(fmt.Sprintf("collect tray icons failed: %v", boxErr))
	}
	// Program logon tasks are held back until here.
	if release, readyErr := ipc.MarkReady(readyMarker); readyErr != nil {
		logger.Warn(fmt.Sprintf("publish ready state failed: %v", readyErr))
	} else {
		defer release()
	}

	createTray := func() error {
		current := mainWindow.Settings()
		c, trayErr := tray.New(
			mainWindow.Native(),
			openSettings,
			runSilently,
			requestExit,
			current.Language,
			!state.hideMainIcon(exitsAfterStartup(current)),
			box,
			logger,
		)
		if trayErr == nil {
			trayController = c
		}
		return trayErr
	}
	if err = createTray(); err != nil {
		if !state.autorun {
			logger.Error(fmt.Sprintf("create tray failed: %v", err))
			emitFatalWithLog(settings.Language, "failed to create system tray", err)
			return 1
		}
		// The logon task can start WinTray before Explorer's taskbar exists.
		// Managed apps are handled meanwhile; the icon follows once it can.
		logger.Warn(fmt.Sprintf("create tray failed at logon, retrying: %v", err))
		launchWG.Add(1)
		go func() {
			defer launchWG.Done()
			retryMainTray(launchCtx, mainWindow, logger, func() bool {
				return quitting || trayController != nil
			}, createTray)
		}()
	}

	if state.settingsOpen {
		mainWindow.ShowMainWindow()
	} else {
		mainWindow.HideMainWindow()
	}

	if state.autorun {
		// The message loop must exist before tasks can hand it hidden windows.
		mainWindow.SetLaunchNowBusy(true)
		mainWindow.Native().Starting().Attach(func() {
			logger.Info(fmt.Sprintf("autorun mode: run managed apps (exitAfterCompleted=%t, collectedTrayIcons=%d)", exitsAfterStartup(settings), len(config.CollectedTrayIconPaths(settings))))
			launchWG.Add(1)
			go func() {
				defer launchWG.Done()
				runManagedApps(launchCtx, orch, settings, logger, handOffToHost)
				mainWindow.Synchronize(func() {
					state.startupPending = false
					mainWindow.SetLaunchNowBusy(false)
					// Programs just started may have a run time to wait for.
					refreshRunLimits()
				})
			}()
		})
	}

	launchWG.Add(1)
	go func() {
		defer launchWG.Done()
		wait := time.Duration(0)
		for waitContext(launchCtx, wait) {
			next := make(chan time.Duration, 1)
			mainWindow.Synchronize(func() { next <- refreshRunLimits() })
			select {
			case wait = <-next:
			case <-launchCtx.Done():
				return
			}
		}
	}()

	return mainWindow.Run()
}

// runLimitPoll is how often running programs are checked against their
// scheduled run time when none is due sooner.
const runLimitPoll = 30 * time.Second

func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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

// retryMainTray keeps trying to create WinTray's own icon on the UI thread
// until done reports it exists or is no longer wanted.
func retryMainTray(ctx context.Context, mainWindow *ui.MainWindow, logger *logging.Logger, done func() bool, create func() error) {
	for attempt := 2; attempt <= hostAddAttempts; attempt++ {
		timer := time.NewTimer(hostAddDelay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
		result := make(chan bool, 1)
		mainWindow.Synchronize(func() {
			if done() {
				result <- true
				return
			}
			err := create()
			if err == nil {
				logger.Info(fmt.Sprintf("tray created on attempt %d", attempt))
			} else if attempt == hostAddAttempts {
				logger.Error(fmt.Sprintf("create tray failed after %d attempts: %v", attempt, err))
			}
			result <- err == nil
		})
		select {
		case ok := <-result:
			if ok {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func ensureRunAtLogon(registrar *startup.Registrar, settings config.Settings, logger *logging.Logger) {
	exePath, err := os.Executable()
	if err != nil || exePath == "" {
		logger.Warn("unable to resolve executable path for run-at-logon")
		return
	}
	args := "--autorun"
	if settings.StartMinimizedToTray {
		args = "--background --autorun"
	}
	taskErr, err := registrar.Apply(exePath, args, settings.RunAtLogon)
	if taskErr != nil && err == nil {
		logger.Warn(fmt.Sprintf("logon task unavailable, using the Run registry entry instead (Windows starts it later): %v", taskErr))
	}
	if err != nil {
		logger.Warn(fmt.Sprintf("set run-at-logon failed: %v", err))
	}
}

// ensureAppTasks reconciles the per-program logon tasks with the settings of
// one request. Failures land in the log; a declined elevation keeps the task
// missing until the next save asks again.
func ensureAppTasks(tasks *startup.AppTasks, request appTaskSyncRequest, logger *logging.Logger) {
	var err error
	if tasks == nil {
		err = errors.New("program logon tasks are unavailable")
	} else {
		err = tasks.Sync(request.settings, request.allowElevated)
	}
	if err != nil {
		logger.Warn(fmt.Sprintf("program logon tasks sync: %v", err))
		if request.onError != nil {
			request.onError(err)
		}
	}
}

// runAppTaskHelper runs the elevated one-shot instances that register or
// remove one program task after the user has confirmed an elevation prompt.
// It reports -1 when args belong to none of them.
func runAppTaskHelper(args []string) int {
	switch {
	case len(args) == 3 && args[0] == startup.AppTaskHelperRegister:
		if err := startup.RegisterAppTaskHeadless(args[1], args[2]); err != nil {
			return 1
		}
		return 0
	case len(args) == 3 && (args[0] == startup.AppTaskHelperRun || args[0] == startup.AppTaskHelperShortcut || args[0] == startup.AppTaskHelperConfigured):
		// The logon trigger fires for WinTray and its program tasks alike;
		// no program may come up before WinTray does.
		if !ipc.WaitReady(readyMarker, readyWait, 200*time.Millisecond) {
			logStartupHelperFailure(fmt.Errorf("WinTray was not running after %s; starting %s anyway", readyWait, args[2]))
		}
		launch := startup.LaunchStartupRun
		switch args[0] {
		case startup.AppTaskHelperShortcut:
			launch = startup.LaunchStartupShortcut
		case startup.AppTaskHelperConfigured:
			launch = startup.LaunchConfigured
		}
		if err := launch(args[1], args[2]); err != nil {
			logStartupHelperFailure(err)
			return 1
		}
		return 0
	case len(args) == 2 && args[0] == startup.AppTaskHelperDelete:
		if err := startup.DeleteAppTaskHeadless(args[1]); err != nil {
			return 1
		}
		return 0
	}
	return -1
}

func logStartupHelperFailure(cause error) {
	dir, err := config.AppDirWithError()
	if err != nil {
		return
	}
	logger, err := logging.New(dir)
	if err != nil {
		return
	}
	defer logger.Close()
	logger.Error(fmt.Sprintf("original startup helper failed: %v", cause))
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

	return cleanupAppDataAfterRestore(appDir, func() error {
		tasks, err := startup.NewAppTasks()
		if err != nil {
			return err
		}
		// The UI already requested any needed elevation before spawning us.
		// A direct headless cleanup must fail safely rather than lose backups.
		if err := tasks.Sync(config.Settings{}, false); err != nil {
			return err
		}
		return startup.NewRegistrar(appName).Remove()
	})
}

func cleanupAppDataAfterRestore(appDir string, restore func() error) error {
	if err := restore(); err != nil {
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
