//go:build windows

package tray

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	fixtureMenuOpen  = win.WM_APP + 71
	fixtureMenuStats = win.WM_APP + 72
	fixtureMenuStop  = win.WM_APP + 73
	fixtureMenuClose = win.WM_APP + 74
)

// This is a disposable application, not a mocked HWND. Native menus run in
// another process's real TrackPopupMenu loop. Its callback and menu owner are
// different windows, as in several tray frameworks.
func TestProgramMenuProcessHelper(t *testing.T) {
	if os.Getenv("WINTRAY_MENU_PROCESS_HELPER") != "1" {
		t.Skip("subprocess fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	kind := os.Getenv("WINTRAY_MENU_PROCESS_KIND")
	var callback, owner, popup win.HWND
	var opens, closes, commands uintptr
	var tracking bool
	var delay uint32
	show := func() {}
	closePopup := func() {
		if kind == "native" {
			win.SendMessage(owner, win.WM_CANCELMODE, 0, 0)
		} else if tracking {
			tracking = false
			closes++
			win.ReleaseCapture()
			win.ShowWindow(popup, win.SW_HIDE)
		}
	}
	proc := syscall.NewCallback(func(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
		switch msg {
		case fixtureMenuOpen:
			delay = uint32(wp)
			if delay != 0 {
				win.SetTimer(callback, 1, delay, 0)
			} else {
				show()
			}
			return 0
		case win.WM_TIMER:
			win.KillTimer(callback, 1)
			show()
			return 0
		case fixtureMenuStats:
			switch wp {
			case 0:
				return opens
			case 1:
				return closes
			case 2:
				return commands
			case 3:
				return uintptr(owner)
			case 4:
				return uintptr(popup)
			}
			return 0
		case fixtureMenuClose:
			closePopup()
			return 0
		case fixtureMenuStop:
			win.KillTimer(callback, 1)
			closePopup()
			win.PostQuitMessage(0)
			return 0
		case win.WM_ACTIVATE:
			if hwnd == popup && kind == "focus" && wp&0xffff == win.WA_INACTIVE {
				closePopup()
			}
		case win.WM_KEYDOWN:
			if hwnd == popup && wp == win.VK_ESCAPE && kind == "focus" {
				commands++ // focus-only surfaces must never receive Escape
				return 0
			}
			if hwnd == popup && wp == win.VK_ESCAPE && kind != "resistant" {
				closePopup()
				return 0
			}
		}
		return win.DefWindowProc(hwnd, msg, wp, lp)
	})
	className := syscall.StringToUTF16Ptr("WinTray.MenuProcessFixture")
	wc := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: proc, HInstance: win.GetModuleHandle(nil), LpszClassName: className}
	if win.RegisterClassEx(&wc) == 0 {
		t.Fatal("register fixture class")
	}
	create := func(style uint32, title string, x, y, width, height int32) win.HWND {
		hwnd := win.CreateWindowEx(win.WS_EX_TOOLWINDOW, className, syscall.StringToUTF16Ptr(title), style,
			x, y, width, height, 0, 0, wc.HInstance, nil)
		if hwnd == 0 {
			t.Fatal("create fixture window")
		}
		return hwnd
	}
	callback = create(win.WS_OVERLAPPEDWINDOW, "Fixture callback", 0, 0, 100, 100)
	defer win.DestroyWindow(callback)
	owner = create(win.WS_OVERLAPPEDWINDOW, "Fixture menu owner", 0, 0, 100, 100)
	defer win.DestroyWindow(owner)
	if kind == "focus" {
		popup = create(win.WS_CAPTION|win.WS_MINIMIZEBOX, "Application panel", 450, 150, 210, 120)
	} else {
		popup = create(win.WS_POPUP|win.WS_BORDER, "Fixture captured popup", 450, 150, 210, 120)
	}
	if kind == "owned" {
		win.SetWindowLongPtr(popup, win.GWLP_HWNDPARENT, uintptr(callback))
	}
	defer win.DestroyWindow(popup)
	show = func() {
		if tracking {
			return
		}
		opens++
		tracking = true
		if kind != "native" {
			win.ShowWindow(popup, win.SW_SHOWNOACTIVATE)
			if kind != "owned" {
				win.SetForegroundWindow(popup)
				if kind != "focus" {
					win.SetCapture(popup)
				}
			}
			return
		}
		menu := win.CreatePopupMenu()
		sub := win.CreatePopupMenu()
		appendTrayMenuItem(sub, menuItem{id: 3, text: "Nested fixture action"})
		appendTrayMenuItem(menu, menuItem{id: 1, text: "Fixture action"})
		appendTrayMenuItem(menu, menuItem{text: "More actions", submenu: sub})
		win.SetForegroundWindow(owner)
		command := win.TrackPopupMenuEx(menu, win.TPM_NOANIMATION|win.TPM_RETURNCMD, 450, 150, owner, nil)
		if command != 0 {
			commands++
		}
		tracking = false
		closes++
		win.DestroyMenu(menu)
	}
	fmt.Printf("%d %d\n", callback, os.Getpid())
	var msg win.MSG
	var pinned runtime.Pinner
	pinned.Pin(&msg)
	defer pinned.Unpin()
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
}

type menuProcessClient struct {
	hwnd win.HWND
	pid  uint32
	cmd  *exec.Cmd
	done chan error
}

func startMenuProcess(t *testing.T, kind string) *menuProcessClient {
	t.Helper()
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	client := &menuProcessClient{done: make(chan error, 1)}
	client.cmd = exec.Command(os.Args[0], "-test.run=^TestProgramMenuProcessHelper$")
	client.cmd.Env = append(os.Environ(), "WINTRAY_MENU_PROCESS_HELPER=1", "WINTRAY_MENU_PROCESS_KIND="+kind)
	client.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := client.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	client.cmd.Stderr = os.Stderr
	if err := client.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { client.done <- client.cmd.Wait() }()
	t.Cleanup(func() {
		win.PostMessage(client.hwnd, fixtureMenuStop, 0, 0)
		select {
		case <-client.done:
		case <-time.After(3 * time.Second):
			_ = client.cmd.Process.Kill()
			<-client.done
		}
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if _, err := fmt.Sscan(line, &client.hwnd, &client.pid); err != nil || client.hwnd == 0 {
			t.Fatalf("fixture startup: %q: %v", line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture startup timeout")
	}
	return client
}

func (c *menuProcessClient) stat(t *testing.T, which uintptr) uintptr {
	t.Helper()
	var result uintptr
	var pinned runtime.Pinner
	pinned.Pin(&result)
	defer pinned.Unpin()
	if ok, _, err := menuUser32.NewProc("SendMessageTimeoutW").Call(uintptr(c.hwnd), fixtureMenuStats, which, 0, 2, 1000, uintptr(unsafe.Pointer(&result))); ok == 0 {
		t.Fatalf("read fixture state: %v", err)
	}
	return result
}

func (c *menuProcessClient) expansion(delay time.Duration, report func(error)) boxMenuExpansion {
	return boxMenuExpansion{owner: c.hwnd, report: report, open: func() error {
		return postProgramMenuMessage(c.hwnd, fixtureMenuOpen, uintptr(delay.Milliseconds()), 0)
	}}
}

func waitMenuCondition(t *testing.T, label string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		var msg win.MSG
		for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) {
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", label)
}

func TestProgramMenuClosesActualOwnerAndCapturedPopup(t *testing.T) {
	for _, kind := range []string{"native", "custom", "owned"} {
		t.Run(kind, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			client := startMenuProcess(t, kind)
			p, err := newProgramMenu(client.expansion(0, nil), 0, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := p.target.open(); err != nil {
				t.Fatal(err)
			}
			waitMenuCondition(t, "real popup", func() bool { p.observe(); return len(p.windows) != 0 })
			popup := p.windows[0].hwnd
			if kind == "native" && (win.GetWindow(popup, win.GW_OWNER) != 0 || p.windows[0].gui.menuOwner == client.hwnd) {
				t.Fatal("fixture must expose the missing/different GW_OWNER failure")
			}
			p.close(time.Now())
			waitMenuCondition(t, "popup hidden and its menu loop returned", func() bool {
				p.observe()
				return !win.IsWindowVisible(popup) && client.stat(t, 1) == 1 && p.done(time.Now())
			})
			if p.err != nil || client.stat(t, 2) != 0 {
				t.Fatalf("close error=%v, unexpected menu command", p.err)
			}
		})
	}
}

// These scenarios exercise hover through WinTray's real modal loop, not a
// choice callback. The fixture reports when TrackPopupMenu actually returns.
func TestBoxMenuPopupLifecycle(t *testing.T) {
	for _, scenario := range []string{"leave", "switch", "cycle", "custom", "owned", "focus", "nested", "command", "late", "late-slow", "resistant", "parent-click", "parent-escape", "parent-outside"} {
		t.Run(scenario, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			kind := "native"
			if scenario == "custom" || scenario == "resistant" || scenario == "owned" || scenario == "focus" {
				kind = scenario
			}
			a := startMenuProcess(t, kind)
			b := startMenuProcess(t, "native")
			hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
			sub := win.CreatePopupMenu()
			if sub == 0 || !appendTrayMenuItem(menu, menuItem{id: trayBoxedBase + 1, text: "Second program", submenu: sub}) {
				t.Fatal("append second program")
			}
			var cursor win.POINT
			win.GetCursorPos(&cursor)
			defer win.SetCursorPos(cursor.X, cursor.Y)
			win.SetForegroundWindow(hwnd)
			var first, second win.RECT
			stage, ticks, switches := 0, 0, 0
			var stageAt time.Time
			var firstPopup win.HWND
			reported := 0
			var requestAt time.Time
			var choice boxMenuChoice
			var err error
			move := func(rect win.RECT) { win.SetCursorPos(rect.Right-12, (rect.Top+rect.Bottom)/2) }
			advance := func(next int) { stage, stageAt = next, time.Now() }
			finish := func() { advance(99); procEndMenu.Call() }
			choice, err = trackBoxMenu(hwnd, menu, func() {
				ticks++
				if ticks > 180 {
					t.Errorf("scenario %s stalled at stage %d", scenario, stage)
					if p := activeBoxMenu.popup; p != nil {
						t.Logf("closing=%t foreground=%#x windows=%+v", p.closing, win.GetForegroundWindow(), p.windows)
					}
					procEndMenu.Call()
					return
				}
				if a.stat(t, 0) > a.stat(t, 1) && b.stat(t, 0) > b.stat(t, 1) {
					t.Error("two program menus were active at once")
					procEndMenu.Call()
					return
				}
				switch stage {
				case 0:
					for index, rect := range []*win.RECT{&first, &second} {
						if ok, _, _ := procGetMenuItemRect.Call(uintptr(hwnd), uintptr(menu), uintptr(3+index), uintptr(unsafe.Pointer(rect))); ok == 0 {
							t.Error("parent row rect")
							procEndMenu.Call()
							return
						}
					}
					move(first)
					advance(1)
				case 1:
					if (scenario == "late" || scenario == "late-slow") && !requestAt.IsZero() {
						win.SetCursorPos(20, 450)
						advance(5)
						return
					}
					if a.stat(t, 0) != 1 || activeBoxMenu.popup == nil || len(activeBoxMenu.popup.windows) == 0 {
						return
					}
					firstPopup = activeBoxMenu.popup.windows[0].hwnd
					r := activeBoxMenu.popup.windows[0].rect
					if scenario == "nested" {
						// Hover the submenu row instead of racing a posted key
						// against the preceding physical mouse move.
						win.SetCursorPos(r.Right-10, r.Top+(r.Bottom-r.Top)*3/4)
					} else {
						win.SetCursorPos((r.Left+r.Right)/2, r.Top+10)
					}
					advance(2)
				case 2:
					if scenario == "nested" {
						if activeBoxMenu.popup == nil || len(activeBoxMenu.popup.windows) < 2 {
							return
						}
						for _, w := range activeBoxMenu.popup.windows {
							if w.hwnd != firstPopup {
								win.SetCursorPos((w.rect.Left+w.rect.Right)/2, (w.rect.Top+w.rect.Bottom)/2)
							}
						}
						advance(3)
						return
					}
					if time.Since(stageAt) < 300*time.Millisecond {
						return
					}
					if a.stat(t, 1) != 0 || !win.IsWindowVisible(firstPopup) {
						t.Error("popup closed while pointer was inside it")
						procEndMenu.Call()
						return
					}
					if scenario == "command" {
						mouse := menuUser32.NewProc("mouse_event")
						mouse.Call(win.MOUSEEVENTF_LEFTDOWN, 0, 0, 0, 0)
						mouse.Call(win.MOUSEEVENTF_LEFTUP, 0, 0, 0, 0)
						advance(5)
					} else if scenario == "switch" || scenario == "cycle" || scenario == "resistant" {
						move(second)
						advance(4)
					} else {
						win.SetCursorPos(20, 450)
						advance(5)
					}
				case 3:
					if time.Since(stageAt) < 300*time.Millisecond {
						return
					}
					if a.stat(t, 1) != 0 {
						t.Error("moving into native submenu dismissed its root")
						procEndMenu.Call()
						return
					}
					win.SetCursorPos(20, 450)
					advance(5)
				case 4:
					if scenario == "resistant" {
						if time.Since(stageAt) < 1300*time.Millisecond {
							return
						}
						if b.stat(t, 0) != 0 || reported == 0 || !win.IsWindowVisible(firstPopup) {
							t.Error("unacknowledged close was treated as success")
							procEndMenu.Call()
							return
						}
						win.PostMessage(a.hwnd, fixtureMenuClose, 0, 0)
						advance(6)
					} else if b.stat(t, 0) == 1 && a.stat(t, 1) == 1 {
						if win.IsWindowVisible(firstPopup) {
							t.Error("old popup remains visible")
						}
						if scenario == "cycle" {
							move(first)
							advance(7)
						} else {
							finish()
						}
					}
				case 5:
					if a.stat(t, 1) != 1 {
						return
					}
					if scenario == "command" && a.stat(t, 2) != 1 {
						t.Error("native menu command was not delivered")
					}
					if firstPopup != 0 && win.IsWindowVisible(firstPopup) {
						t.Error("popup still visible after close")
					}
					switch scenario {
					case "parent-click":
						win.SetCursorPos((first.Left+first.Right)/2, (first.Top+first.Bottom)/2)
						mouse := menuUser32.NewProc("mouse_event")
						mouse.Call(win.MOUSEEVENTF_LEFTDOWN, 0, 0, 0, 0)
						mouse.Call(win.MOUSEEVENTF_LEFTUP, 0, 0, 0, 0)
						advance(98)
					case "parent-escape":
						win.PostMessage(hwnd, win.WM_KEYDOWN, win.VK_ESCAPE, 0)
						advance(98)
					case "parent-outside":
						// Address our owner directly: no desktop application
						// receives the synthetic outside click in this test.
						win.PostMessage(hwnd, win.WM_LBUTTONDOWN, 1, 20|(450<<16))
						win.PostMessage(hwnd, win.WM_LBUTTONUP, 0, 20|(450<<16))
						advance(98)
					default:
						finish()
					}
				case 6:
					if b.stat(t, 0) == 1 && a.stat(t, 1) == 1 {
						finish()
					}
				case 7:
					if a.stat(t, 0) != a.stat(t, 1)+1 || b.stat(t, 0) != b.stat(t, 1) {
						return
					}
					switches++
					if switches == 3 {
						finish()
						return
					}
					move(second)
					advance(8)
				case 8:
					if b.stat(t, 0) != b.stat(t, 1)+1 || a.stat(t, 0) != a.stat(t, 1) {
						return
					}
					move(first)
					advance(7)
				}
			}, func() { win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil) }, func(id uint32) (boxMenuExpansion, error) {
				if id == trayBoxedBase+1 {
					return b.expansion(0, nil), nil
				}
				requestAt = time.Now()
				delay := time.Duration(0)
				if scenario == "late" {
					delay = 350 * time.Millisecond
				} else if scenario == "late-slow" {
					delay = 1200 * time.Millisecond
				}
				return a.expansion(delay, func(error) { reported++ }), nil
			})
			wantStage, wantChoice := 99, boxMenuChoice{}
			if scenario == "parent-click" || scenario == "parent-escape" || scenario == "parent-outside" {
				wantStage = 98
			}
			if scenario == "parent-click" {
				wantChoice.id = trayBoxedBase
			}
			if stage != wantStage || choice != wantChoice || err != nil && scenario != "resistant" {
				for _, input := range recordedMenuInput {
					t.Logf("parent input: msg=%#x key=%#x point=%+v", input.Message, input.WParam, input.Pt)
				}
				for _, input := range recordedMenuOwnerMessages {
					t.Logf("parent notification: msg=%#x wp=%#x lp=%#x", input.Message, input.WParam, input.LParam)
				}
				t.Fatalf("parent menu ended early: stage=%d choice=%+v err=%v", stage, choice, err)
			}
			if scenario != "resistant" && reported != 0 {
				t.Errorf("unexpected close failures: %d", reported)
			}
			if scenario == "focus" && a.stat(t, 2) != 0 {
				t.Error("Escape was sent to a focus-only window")
			}
			waitMenuCondition(t, "parent cleanup", func() bool { return retiringProgramMenu == nil })
		})
	}
}

func TestBoxMenuParentExitCancelsLatePopup(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	client := startMenuProcess(t, "native")
	hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
	var requested bool
	_, err := trackBoxMenu(hwnd, menu, func() {
		if !requested {
			activeBoxMenu.choose(boxMenuChoice{id: trayBoxedBase, expand: true})
			requested = true
		}
		procEndMenu.Call()
	}, func() { win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil) }, func(uint32) (boxMenuExpansion, error) {
		return client.expansion(300*time.Millisecond, nil), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitMenuCondition(t, "late popup cancelled after parent exit", func() bool {
		return client.stat(t, 0) == 1 && client.stat(t, 1) == 1 && retiringProgramMenu == nil
	})
}

func TestProgramMenuNeverAdoptsExistingPopups(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	client := startMenuProcess(t, "custom")
	if err := client.expansion(0, nil).open(); err != nil {
		t.Fatal(err)
	}
	popup := win.HWND(client.stat(t, 4))
	waitMenuCondition(t, "existing popup", func() bool { return win.IsWindowVisible(popup) })
	p, err := newProgramMenu(client.expansion(0, nil), 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p.observe()
	p.close(time.Now())
	if len(p.windows) != 0 || !win.IsWindowVisible(popup) || client.stat(t, 1) != 0 {
		t.Fatal("pre-existing menu was adopted or dismissed")
	}
}

func TestMenuRectSupportsNegativeMonitorCoordinates(t *testing.T) {
	for _, tc := range []struct {
		point win.POINT
		want  bool
	}{
		{win.POINT{X: -1500, Y: -500}, true}, {win.POINT{X: -1000, Y: -500}, false},
		{win.POINT{X: -1601, Y: -500}, false}, {win.POINT{X: -1500, Y: 0}, false},
	} {
		t.Run(strconv.Itoa(int(tc.point.X))+","+strconv.Itoa(int(tc.point.Y)), func(t *testing.T) {
			if got := pointInMenuRect(win.RECT{Left: -1600, Top: -600, Right: -1000, Bottom: 0}, tc.point); got != tc.want {
				t.Fatalf("hit=%t want=%t", got, tc.want)
			}
		})
	}
}
