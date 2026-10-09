//go:build windows

package traybox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"syscall"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// RecoveryExecutable is shipped beside WinTray. A different image name keeps
// Task Manager's termination of WinTray.exe from also killing its recovery.
const RecoveryExecutable = "WinTray-Recovery.exe"

const recoveryTimeout = 5 * time.Second

type recoveryIcon struct {
	Request []byte
	PID     uint32
	Created uint64
}

type recoveryFrame struct {
	Icons []recoveryIcon
	Stop  bool
}

// recoveryClient acknowledges each snapshot before the listener may hide an
// icon. The inherited pipe closes on TerminateProcess, without needing defer,
// a timer in the dying process, or files containing stale recovery records.
type recoveryClient struct {
	input   io.WriteCloser
	output  io.ReadCloser
	encoder *json.Encoder
	done    chan struct{}
	err     error // published by closing done
	process *os.Process
	last    []recoveryIcon
}

func startRecovery(cmd *exec.Cmd) (*recoveryClient, error) {
	childIn, in, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer childIn.Close()
	out, childOut, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	defer childOut.Close()
	// Own these ends ourselves: Cmd.Wait must not close the acknowledgement
	// reader before the final byte has been consumed during normal shutdown.
	cmd.Stdin, cmd.Stdout = childIn, childOut
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	c := &recoveryClient{input: in, output: out, encoder: json.NewEncoder(in), done: make(chan struct{}), process: cmd.Process}
	go func() { c.err = cmd.Wait(); close(c.done) }()
	if err := c.exchange(nil); err != nil {
		c.abort()
		return nil, fmt.Errorf("start tray recovery: %w", err)
	}
	return c, nil
}

// Pipe IO runs off the listener thread with a deadline. A stalled helper must
// not indefinitely block another application's Shell_NotifyIcon call.
func (c *recoveryClient) exchange(frame *recoveryFrame) error {
	result := make(chan error, 1)
	go func() {
		if frame != nil {
			if err := c.encoder.Encode(frame); err != nil {
				result <- err
				return
			}
		}
		var ack [1]byte
		_, err := io.ReadFull(c.output, ack[:])
		if err == nil && ack[0] != 1 {
			err = errors.New("invalid tray recovery acknowledgement")
		}
		result <- err
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(recoveryTimeout):
		c.close()
		return errors.New("tray recovery did not respond")
	}
}

func (c *recoveryClient) close() { _ = c.input.Close(); _ = c.output.Close() }

// Once the live listener takes over restoration, a stalled helper must not
// wake up later and apply an obsolete snapshot over newer native changes.
func (c *recoveryClient) abort() { _ = c.process.Kill(); c.close() }

// stop is used only after Explorer has accepted every normal restoration.
func (c *recoveryClient) stop() error {
	err := c.exchange(&recoveryFrame{Stop: true})
	c.close()
	select {
	case <-c.done:
		return errors.Join(err, c.err)
	case <-time.After(recoveryTimeout):
		c.abort()
		return errors.Join(err, errors.New("tray recovery did not exit"))
	}
}

// RunRecovery serves the private inherited pipes of WinTray-Recovery.exe.
// A session lock makes a new collector wait until the previous recovery has
// finished, so a quick restart cannot have its newly hidden icons unhidden.
func RunRecovery(input io.Reader, output io.Writer) error {
	return runRecovery(input, output, explorerTray, `Local\WinTray_TrayRecovery`)
}

func runRecovery(input io.Reader, output io.Writer, target func(win.HWND) win.HWND, lockName string) error {
	runtime.LockOSThread() // Windows mutex ownership belongs to this OS thread.
	defer runtime.UnlockOSThread()
	name, err := windows.UTF16PtrFromString(lockName)
	if err != nil {
		return err
	}
	lock, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	defer windows.CloseHandle(lock)
	status, err := windows.WaitForSingleObject(lock, uint32(recoveryTimeout.Milliseconds()))
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED {
		return errors.New("previous tray recovery is still running")
	}
	defer windows.ReleaseMutex(lock)
	if _, err := output.Write([]byte{1}); err != nil {
		return err
	}
	decoder := json.NewDecoder(input)
	var icons []recoveryIcon
	var readErr error
	for {
		var frame recoveryFrame
		if err := decoder.Decode(&frame); err != nil {
			readErr = err
			break
		}
		if frame.Stop {
			_, err := output.Write([]byte{1})
			return err
		}
		icons = frame.Icons
		if _, err := output.Write([]byte{1}); err != nil {
			readErr = err
			break
		}
	}
	// Let an already dispatched shell call complete before undoing its hidden
	// state. No new collector can start while we hold the recovery mutex.
	if len(icons) != 0 {
		time.Sleep(100 * time.Millisecond)
	}
	restoreErr := restoreRecoveryIcons(icons, target)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	return errors.Join(readErr, restoreErr)
}

func processCreation(pid uint32) (uint64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), nil
}

func recoveryOwnerAlive(icon recoveryIcon) bool {
	d, ok := parseTrayData(icon.Request)
	if !ok || d.HWnd == 0 || d.Message != nimModify || d.Flags & ^uint32(nifState|nifGUID) != 0 {
		return false
	}
	var pid uint32
	win.GetWindowThreadProcessId(win.HWND(d.HWnd), &pid)
	if pid == 0 || pid != icon.PID {
		return false
	}
	created, err := processCreation(pid)
	return err == nil && created == icon.Created
}

// Restore only the visibility of still-existing registrations. Never replay
// ADD, a balloon notification, or callbacks; PID + creation time prevents a
// recycled window/process ID from receiving an old program's recovery.
func restoreRecoveryIcons(icons []recoveryIcon, target func(win.HWND) win.HWND) error {
	l := &listener{cfg: listenerConfig{target: target}}
	deadline := time.Now().Add(15 * time.Second)
	for len(icons) > 0 {
		pending := icons[:0]
		for _, icon := range icons {
			if !recoveryOwnerAlive(icon) {
				continue
			}
			if l.sendRequest(0, icon.Request) == 0 {
				pending = append(pending, icon)
			}
		}
		icons = pending
		if len(icons) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("could not restore %d tray icons", len(icons))
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}

// syncRecovery runs on the listener thread, before any native hide request.
// The program's own hidden/deleted icons are omitted from the next snapshot.
func (l *listener) syncRecovery() error {
	if l.recovery == nil {
		return nil
	}
	if l.recoveryFailed {
		return errors.New("tray recovery is unavailable")
	}
	select {
	case <-l.recovery.done:
		return errors.New("tray recovery process exited")
	default:
	}
	l.mu.Lock()
	var icons []recoveryIcon
	for _, t := range l.icons {
		if t.hidden || t.nativeRestored || !isWindow(win.HWND(t.owner)) {
			continue
		}
		var pid uint32
		win.GetWindowThreadProcessId(win.HWND(t.owner), &pid)
		// Hosted icons die with WinTray; there is no native owner to restore.
		if pid == uint32(os.Getpid()) {
			continue
		}
		if t.recoveryPID != pid || t.recoveryCreated == 0 {
			created, err := processCreation(pid)
			if err != nil {
				l.mu.Unlock()
				return fmt.Errorf("identify tray icon owner: %w", err)
			}
			t.recoveryPID, t.recoveryCreated = pid, created
		}
		request := stateRequest(t.template, false)
		if len(request) == 0 {
			l.mu.Unlock()
			return errors.New("invalid tray recovery request")
		}
		// GUID-only updates need not repeat the window, but recovery must still
		// verify the original owner before changing Explorer's registration.
		put(request, offHWnd, t.owner)
		put(request, offUID, t.uid)
		icons = append(icons, recoveryIcon{request, t.recoveryPID, t.recoveryCreated})
	}
	l.mu.Unlock()
	// Tooltip/icon animation updates do not change recovery state. Avoid IPC
	// for those updates and idle maintenance ticks, regardless of map order.
	slices.SortFunc(icons, func(a, b recoveryIcon) int { return bytes.Compare(a.Request, b.Request) })
	if slices.EqualFunc(icons, l.recovery.last, func(a, b recoveryIcon) bool {
		return a.PID == b.PID && a.Created == b.Created && bytes.Equal(a.Request, b.Request)
	}) {
		return nil
	}
	if err := l.recovery.exchange(&recoveryFrame{Icons: icons}); err != nil {
		return err
	}
	l.recovery.last = icons
	return nil
}

func (l *listener) failRecovery(err error) {
	if l.recoveryFailed {
		return
	}
	l.recoveryFailed = true
	l.mu.Lock()
	l.closing = true // pass through future requests and stop healing them hidden
	l.mu.Unlock()
	l.recovery.abort()
	l.logger.Warn(fmt.Sprintf("tray box: recovery unavailable; collection stopped: %v", err))
	if err := l.restoreIcons(true); err != nil {
		l.logger.Warn(fmt.Sprintf("tray box: %v", err))
	}
}
