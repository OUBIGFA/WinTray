//go:build windows

package traybox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	brokerWindowClass  = "WinTray.TrayIconBroker.v1"
	bridgeExecutable   = "WinTray-Bridge.exe"
	bridgeDLL          = "WinTray-ShellBridge.dll"
	bridgeForwardData  = 0x57544246
	bridgeSelectData   = 0x57544250
	bridgePacketData   = 0x57544251
	bridgeResumeData   = 0x57544252
	bridgeObservedData = 0x5754424f
)

// bridgeArgs is the fixed-width ABI in native/traybridge/bridge.h. Both native
// helpers validate the desktop Shell PID, creation time and taskbar thread.
type bridgeArgs struct {
	Size, Protocol, HostPID, TargetPID uint32
	HostCreated, TargetCreated, Broker uint64
	Taskbar                            uint64
	StopEvent                          [128]uint16
	ReadyEvent                         [128]uint16
}

type bridgeTarget struct {
	pid     uint32
	created uint64
	path    string
}

type shellEndpoint struct {
	bridgeTarget
	hwnd win.HWND
}

type bridgeLease struct {
	activate    func() error
	cancel      func() error
	selectPaths func([]string) error
	alive       func() bool
}

type nativeBridge struct {
	broker win.HWND
	host   bridgeTarget
	dir    string
	cached map[string]string
	owners *sync.Map
}

func newNativeBridge(selfPath string, broker win.HWND) (*nativeBridge, error) {
	created, err := processCreation(uint32(os.Getpid()))
	if err != nil {
		return nil, err
	}
	n := &nativeBridge{broker: broker, host: bridgeTarget{pid: uint32(os.Getpid()), created: created, path: processPath(uint32(os.Getpid()))},
		dir: filepath.Join(filepath.Dir(selfPath), "native"), cached: make(map[string]string)}
	// Validate the portable layout even if selected applications have not
	// started yet; never accept a selection that cannot later be collected.
	for _, arch := range []string{"x64", "x86"} {
		for _, name := range []string{bridgeExecutable, bridgeDLL} {
			path := filepath.Join(n.dir, arch, name)
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("tray bridge component unavailable: %s (extract the complete WinTray package): %w", path, errors.Join(err, os.ErrNotExist))
			}
		}
	}
	return n, nil
}

// cachedDLL keeps loaded modules out of the portable directory. A subclass
// can still be on the taskbar thread's reentrant stack after cancellation, so
// its module stays pinned until the shell exits. Content-addressed copies let
// users replace WinTray's package without overwriting a loaded DLL.
func (n *nativeBridge) cachedDLL(arch string) (string, error) {
	if path := n.cached[arch]; path != "" {
		return path, nil
	}
	data, err := os.ReadFile(filepath.Join(n.dir, arch, bridgeDLL))
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "WinTray", "TrayBridge", fmt.Sprintf("%x", sha256.Sum256(data)), arch)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, bridgeDLL)
	if existing, err := os.ReadFile(path); err == nil {
		if sha256.Sum256(existing) != sha256.Sum256(data) {
			return "", fmt.Errorf("tray bridge cache does not match the package: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		_, writeErr := file.Write(data)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			_ = os.Remove(path) // discard only our own incomplete cache entry
			return "", err
		}
	}
	n.cached[arch] = path
	return path, nil
}

func bridgeArchitecture(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var machine, native uint16
	if err := windows.IsWow64Process2(h, &machine, &native); err != nil {
		return "", err
	}
	if machine == pe.IMAGE_FILE_MACHINE_UNKNOWN {
		machine = native
	}
	switch machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "x64", nil
	case pe.IMAGE_FILE_MACHINE_I386:
		return "x86", nil
	default:
		return "", fmt.Errorf("tray collection does not support process machine 0x%X", machine)
	}
}

func (n *nativeBridge) attach(target shellEndpoint) (*bridgeLease, error) {
	arch, err := bridgeArchitecture(target.pid)
	if err != nil {
		return nil, err
	}
	dll, err := n.cachedDLL(arch)
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	name := fmt.Sprintf(`Local\WinTray.TrayBridge.%d.%x`, n.host.pid, nonce)
	wide, err := windows.UTF16FromString(name)
	if err != nil {
		return nil, err
	}
	event, err := windows.CreateEvent(nil, 1, 0, &wide[0])
	if err != nil {
		return nil, err
	}
	readyName := name + ".ready"
	readyWide, err := windows.UTF16FromString(readyName)
	if err != nil {
		windows.CloseHandle(event)
		return nil, err
	}
	ready, err := windows.CreateEvent(nil, 1, 0, &readyWide[0])
	if err != nil {
		windows.CloseHandle(event)
		return nil, err
	}
	lease := &bridgeLease{
		selectPaths: func(paths []string) error { return n.selectPaths(target, paths) },
		alive: func() bool {
			status, err := windows.WaitForSingleObject(event, 0)
			return err == nil && status == uint32(windows.WAIT_TIMEOUT)
		},
		activate: func() error { return windows.SetEvent(ready) },
		cancel: func() error {
			// Signal before closing: late/timed-out remote attachments must
			// stay dormant even if they finish after we abandon the attempt.
			return errors.Join(windows.SetEvent(event), windows.CloseHandle(event), windows.CloseHandle(ready))
		},
	}
	args := bridgeArgs{Protocol: 4, HostPID: n.host.pid, TargetPID: target.pid,
		HostCreated: n.host.created, TargetCreated: target.created, Broker: uint64(n.broker)}
	args.Taskbar = uint64(target.hwnd)
	args.Size = uint32(unsafe.Sizeof(args))
	copy(args.StopEvent[:], wide)
	copy(args.ReadyEvent[:], readyWide)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	decimal := func(v uint64) string { return strconv.FormatUint(v, 10) }
	cmd := exec.CommandContext(ctx, filepath.Join(n.dir, arch, bridgeExecutable), "--attach",
		decimal(uint64(target.pid)), decimal(target.created), decimal(uint64(n.host.pid)),
		decimal(n.host.created), decimal(uint64(n.broker)), decimal(uint64(target.hwnd)), name, readyName, dll)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, commandErr := cmd.CombinedOutput()
	if commandErr != nil {
		err = fmt.Errorf("%w: %s", commandErr, strings.TrimSpace(string(output)))
	}
	if err != nil {
		return nil, errors.Join(err, lease.cancel())
	}
	return lease, nil
}

// currentShellEndpoint never enumerates or opens selected applications for
// injection. Both elevated and sandboxed owners already send to this receiver.
func currentShellEndpoint() (shellEndpoint, error) {
	hwnd := explorerTray(0)
	if hwnd == 0 {
		return shellEndpoint{}, errors.New("Windows taskbar is unavailable")
	}
	var pid uint32
	win.GetWindowThreadProcessId(hwnd, &pid)
	created, err := processCreation(pid)
	if err != nil {
		return shellEndpoint{}, err
	}
	return shellEndpoint{bridgeTarget{pid, created, processPath(pid)}, hwnd}, nil
}
