//go:build windows

package startup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"debug/pe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wintray/internal/config"
)

// Helper flags mark the one-shot elevated instance that registers or removes a
// single program task after the user has confirmed an elevation prompt. The
// instance does nothing else and exits at once.
const (
	AppTaskHelperRegister = "--register-app-task"
	AppTaskHelperDelete   = "--delete-app-task"
)

// errElevationNeeded reports a task whose registration needs the elevated
// helper while elevation was not allowed, e.g. during the background
// reconciliation at startup. The next save made from the UI asks for it.
var errElevationNeeded = errors.New("program task needs one elevated confirmation; it is requested when settings are saved")

// AppTasks reuses an application's existing interactive logon task, or creates
// a current-user replacement for its original Run/shortcut trigger. Original
// startup data remains owned by the application. Only newly created elevated
// tasks need a one-time administrator confirmation.
type AppTasks struct {
	mu            sync.Mutex
	userSID       string
	selfExe       string
	namePrefix    string
	statePath     string
	run           func(args ...string) ([]byte, error)
	elevateSelf   func(args ...string) error
	findEntries   func(string) ([]startupEntry, error)
	writeApproval func(string, string, approvalValue, approvalValue) error
	// Log is an optional diagnostic callback; set it before calling Sync.
	Log func(string)
}

func NewAppTasks() (*AppTasks, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current user: %w", err)
	}
	sid := user.User.Sid.String()
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("executable: %w", err)
	}
	dir, err := config.AppDirWithError()
	if err != nil {
		return nil, err
	}
	// The SID in the name keeps each Windows user's tasks apart; task names are
	// machine wide. The prefix is also what orphan cleanup matches against.
	return &AppTasks{
		userSID:       sid,
		selfExe:       self,
		namePrefix:    "WinTrayApp-" + sid + "-",
		statePath:     filepath.Join(dir, "startup-migrations.json"),
		run:           runSchtasks,
		findEntries:   findStartupEntries,
		writeApproval: writeUserApproval,
		elevateSelf:   func(args ...string) error { return runSelfElevated(self, args...) },
	}, nil
}

// appTaskSpec is one wanted task, derived from the settings.
type appTaskSpec struct {
	name       string // full schtasks name
	appName    string
	exePath    string
	args       string
	workingDir string
	delay      int // seconds after sign-in
	highest    bool
}

// Sync replaces only the login trigger. Existing native tasks remain owned by
// their applications; Run/shortcut migrations keep recoverable enable-state
// backups. Turning the feature off releases our task and restores those states.
// Background reconciliation never asks for elevation.
func (t *AppTasks) Sync(settings config.Settings, allowElevated bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	inventory, err := readTaskInventory(t.run)
	if err != nil {
		return err // absence cannot be inferred from an unreadable inventory
	}
	state, err := loadMigrationState(t.statePath)
	if err != nil {
		return err
	}
	wanted := make(map[string]bool)
	pathCounts := make(map[string]int)
	idCounts := make(map[string]int)
	var apps []config.LogonTaskApp
	if settings.RunAtLogon {
		apps = config.LogonTaskApps(settings)
		for _, app := range apps {
			wanted[t.namePrefix+app.Entry.ID] = true
			pathCounts[strings.ToLower(filepath.Clean(app.Entry.ExePath))]++
			idCounts[strings.ToLower(app.Entry.ID)]++
		}
	}
	managed := make(map[string]bool)
	for _, task := range inventory {
		name := strings.TrimPrefix(task.Registration.URI, `\`)
		if strings.HasPrefix(name, t.namePrefix) {
			managed[name] = true
		}
	}
	for name := range state.Apps {
		if !strings.HasPrefix(name, t.namePrefix) {
			return fmt.Errorf("startup backup belongs to another task owner: %s", name)
		}
		managed[name] = true
	}
	var obsolete []string
	for name := range managed {
		if !wanted[name] {
			obsolete = append(obsolete, name)
		}
	}
	sort.Strings(obsolete)
	var errs []error
	for _, name := range obsolete {
		if err := t.releaseMigration(name, state, allowElevated); err != nil {
			errs = append(errs, fmt.Errorf("restore original startup for %s: %w", name, err))
		}
	}
	for _, app := range apps {
		if app.Entry.ID == "" || strings.ContainsAny(app.Entry.ID, "\\/\x00") {
			errs = append(errs, fmt.Errorf("%s: invalid program task identifier", app.Entry.Name))
			continue
		}
		if pathCounts[strings.ToLower(filepath.Clean(app.Entry.ExePath))] > 1 || idCounts[strings.ToLower(app.Entry.ID)] > 1 {
			err := t.releaseMigration(t.namePrefix+app.Entry.ID, state, allowElevated)
			errs = append(errs, errors.Join(fmt.Errorf("%s: multiple WinTray entries share an executable or task identifier", app.Entry.Name), err))
			continue
		}
		if err := t.syncPreservingStartup(app, inventory, state, allowElevated); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", app.Entry.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (t *AppTasks) report(message string) {
	if t.Log != nil {
		t.Log(message)
	}
}

func (t *AppTasks) spec(app config.LogonTaskApp) appTaskSpec {
	return appTaskSpec{
		name:       t.namePrefix + app.Entry.ID,
		appName:    app.Entry.Name,
		exePath:    app.Entry.ExePath,
		args:       app.Entry.Args,
		workingDir: filepath.Dir(app.Entry.ExePath),
		delay:      app.DelaySeconds,
		highest:    requiresElevationForLaunch(app.Entry.ExePath),
	}
}

// requiresElevationForLaunch is what spec uses; a variable so tests can
// exercise the elevated branch without an executable whose manifest demands
// elevation.
var requiresElevationForLaunch = RequiresElevationForLaunch

// syncOne registers the task unless one with the same definition is already
// in place. The fingerprint check keeps an unchanged task untouched, so an
// elevated program is asked for confirmation only when its task changed.
func (t *AppTasks) syncOne(spec appTaskSpec, allowElevated bool) error {
	definition, fingerprint := appTaskXML(t.userSID, spec)
	needsElevation := spec.highest
	if out, err := t.run("/Query", "/TN", spec.name, "/XML"); err == nil {
		if appTaskUpToDate(out, t.userSID, spec, fingerprint) {
			return nil
		}
		var previous taskDefinition
		if decodeTaskXML(out, &previous) == nil {
			for _, principal := range previous.Principals {
				needsElevation = needsElevation || principal.RunLevel == "HighestAvailable"
			}
		}
	}
	path, cleanup, err := writeTaskDefinition(definition)
	if err != nil {
		return err
	}
	defer cleanup()
	if !needsElevation || IsProcessElevated() {
		_, err = t.run("/Create", "/TN", spec.name, "/XML", path, "/F")
		return err
	}
	if !allowElevated {
		return errElevationNeeded
	}
	if t.elevateSelf == nil {
		return errors.New("elevation helper unavailable")
	}
	return t.elevateSelf(AppTaskHelperRegister, spec.name, path)
}

// remove deletes one task; a task that does not exist counts as removed. A
// task registered by the elevated helper may refuse deletion from this
// filtered token, so that one removal goes through the helper as well.
func (t *AppTasks) remove(name string, allowElevated bool) error {
	_, err := t.run("/Delete", "/TN", name, "/F")
	if err == nil {
		return nil
	}
	// A failed per-task query may mean access denied, not 'missing'. Only
	// an intact inventory can prove deletion unnecessary.
	inventory, queryErr := readTaskInventory(t.run)
	if queryErr != nil {
		return errors.Join(err, queryErr)
	}
	found := false
	for _, task := range inventory {
		if strings.EqualFold(strings.TrimPrefix(task.Registration.URI, `\`), strings.TrimPrefix(name, `\`)) {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	if IsProcessElevated() || !allowElevated || t.elevateSelf == nil {
		return err
	}
	return t.elevateSelf(AppTaskHelperDelete, name)
}

// writeTaskDefinition stores a definition in the encoding schtasks accepts
// and returns the path plus a cleanup func.
func writeTaskDefinition(definition string) (string, func(), error) {
	file, err := os.CreateTemp("", "wintray-app-task-*.xml")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	_, writeErr := file.Write(utf16LEWithBOM(definition))
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		cleanup()
		return "", nil, writeErr
	}
	return path, cleanup, nil
}

// appTaskXML returns the task definition and a fingerprint of the intended
// launch. Reconciliation also checks exported action/principal/trigger fields.
func appTaskXML(userSID string, spec appTaskSpec) (string, string) {
	runLevel := "LeastPrivilege"
	if spec.highest {
		runLevel = "HighestAvailable"
	}
	// Launch at boot starts at sign-in; a delay exists only when scheduled.
	delay := ""
	if spec.delay > 0 {
		delay = fmt.Sprintf("\n      <Delay>PT%dS</Delay>", spec.delay)
	}
	workingDirectory := ""
	if spec.workingDir != "" {
		workingDirectory = "<WorkingDirectory>" + xmlText(spec.workingDir) + "</WorkingDirectory>"
	}
	body := fmt.Sprintf(`  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%[1]s</UserId>%[2]s
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%[1]s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>%[3]s</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>false</StartWhenAvailable>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>4</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%[4]s</Command>
      <Arguments>%[5]s</Arguments>
      %[6]s
    </Exec>
  </Actions>
`, xmlText(userSID), delay, runLevel, xmlText(spec.exePath), xmlText(spec.args), workingDirectory)
	sum := sha256.Sum256([]byte(body))
	fingerprint := "wintray-app-" + hex.EncodeToString(sum[:8])
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>WinTray</Author>
    <Description>Starts ` + xmlText(spec.appName) + ` at logon. ` + fingerprint + `</Description>
  </RegistrationInfo>
` + body + `</Task>
`, fingerprint
}

// RegisterAppTaskHeadless creates or overwrites one program task with the
// definition in xmlPath. It runs inside the elevated helper instance after
// the user confirmed the elevation prompt.
func RegisterAppTaskHeadless(name, xmlPath string) error {
	_, err := runSchtasks("/Create", "/TN", name, "/XML", xmlPath, "/F")
	return err
}

// DeleteAppTaskHeadless removes one program task from the elevated helper.
func DeleteAppTaskHeadless(name string) error {
	_, err := runSchtasks("/Delete", "/TN", name, "/F")
	return err
}

// RequiresElevationForLaunch reports whether launching exePath shows a UAC
// prompt: the program's manifest demands requireAdministrator or
// highestAvailable, or the user has set the "Run as administrator"
// compatibility flag on it. Such a program needs a task with RunLevel
// HighestAvailable to start elevated at sign-in without a prompt.
func RequiresElevationForLaunch(exePath string) bool {
	if runAsAdminCompatFlag(exePath) {
		return true
	}
	return manifestDemandsElevation(exePath)
}

// compatLayersRegistryPath holds the "Run as administrator" compatibility
// flags; tests point it at a key of their own.
var compatLayersRegistryPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Layers`

func runAsAdminCompatFlag(exePath string) bool {
	if exePath == "" {
		return false
	}
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, compatLayersRegistryPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		data, _, err := key.GetStringValue(exePath)
		key.Close()
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToUpper(data), "RUNASADMIN") {
			return true
		}
	}
	return false
}

// elevationManifestMarkers are the manifest levels that make Windows ask for
// elevation whenever the program starts.
var elevationManifestMarkers = []string{"requireAdministrator", "highestAvailable"}

func manifestDemandsElevation(path string) bool {
	f, err := pe.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	for _, section := range f.Sections {
		if section.Name != ".rsrc" {
			continue
		}
		data, err := section.Data()
		if err != nil {
			continue
		}
		if resourceDataDemandsElevation(data) {
			return true
		}
	}
	return false
}

// resourceDataDemandsElevation scans raw .rsrc bytes for the manifest's
// requested execution level, both in the UTF-8 the resource compiler emits
// and in UTF-16 for the rare unicode manifest.
func resourceDataDemandsElevation(data []byte) bool {
	for _, marker := range elevationManifestMarkers {
		if bytes.Contains(data, []byte(marker)) {
			return true
		}
		if bytes.Contains(data, utf16LE(marker)) {
			return true
		}
	}
	return false
}

// IsProcessElevated reports whether this process runs with administrator
// rights, in which case tasks register directly and no helper is needed.
func IsProcessElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

var (
	shell32            = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteEx = shell32.NewProc("ShellExecuteExW")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	elevatedHelperTimeout = 3 * time.Minute
)

// shellExecuteInfoW is SHELLEXECUTEINFOW, laid out for ShellExecuteExW.
type shellExecuteInfoW struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hMonitor     windows.Handle
	hProcess     windows.Handle
}

// runSelfElevated starts one more instance of this executable with
// administrator rights, waits for it and fails when it fails. The user sees
// a single elevation prompt per task change; a declined prompt reports
// ERROR_CANCELLED like any other failure.
func runSelfElevated(selfExe string, args ...string) error {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(selfExe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return err
	}
	info := &shellExecuteInfoW{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		nShow:        0, // SW_HIDE; the helper shows nothing of its own
	}
	r1, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(info)))
	if r1 == 0 {
		return fmt.Errorf("start elevated helper: %w", callErr)
	}
	if info.hProcess == 0 {
		return errors.New("elevated helper returned no process handle")
	}
	defer windows.CloseHandle(info.hProcess)
	event, waitErr := windows.WaitForSingleObject(info.hProcess, uint32(elevatedHelperTimeout/time.Millisecond))
	if waitErr != nil {
		return fmt.Errorf("wait for elevated helper: %w", waitErr)
	}
	if event == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("elevated helper timed out")
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected elevated helper wait result: %d", event)
	}
	var code uint32
	if err = windows.GetExitCodeProcess(info.hProcess, &code); err != nil || code != 0 {
		return fmt.Errorf("elevated helper failed: exit=%d err=%v", code, err)
	}
	return nil
}
