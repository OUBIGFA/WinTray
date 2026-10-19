//go:build windows

package tray

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	menuUser32              = windows.NewLazySystemDLL("user32.dll")
	procEndMenu             = menuUser32.NewProc("EndMenu")
	procSetWindowsHookEx    = menuUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = menuUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = menuUser32.NewProc("CallNextHookEx")
	procEnumWindows         = menuUser32.NewProc("EnumWindows")
	procGetMenuItemRect     = menuUser32.NewProc("GetMenuItemRect")
	procGetAsyncKeyState    = menuUser32.NewProc("GetAsyncKeyState")
	menuControls            = windows.NewLazySystemDLL("comctl32.dll")
	procSetMenuSubclass     = menuControls.NewProc("SetWindowSubclass")
	procRemoveMenuSubclass  = menuControls.NewProc("RemoveWindowSubclass")
	procDefMenuSubclass     = menuControls.NewProc("DefSubclassProc")
)

const (
	msgfMenu           = 2
	boxRefreshTimer    = 0x5742
	boxHoverDelay      = 120 * time.Millisecond
	boxPopupLeaveDelay = 150 * time.Millisecond
)

type boxMenuChoice struct {
	id     uint32
	expand bool
}

type boxMenuTracking struct {
	owner           win.HWND
	menu            win.HMENU
	refresh         func()
	choice          boxMenuChoice
	pressed         boxMenuChoice
	selected        int32
	hoverID         uint32
	hoverSince      time.Time
	hoverDelay      time.Duration
	rightSuppressed uint32
	expand          func(uint32) (boxMenuExpansion, error)
	expanding       bool
	hasExpanded     bool
	popup           *programMenu
	expandedID      uint32
	pendingID       uint32
	outsideSince    time.Time
	err             error
}

// Only the UI thread tracks menus. WH_MSGFILTER is scoped to that thread and
// TrackPopupMenuEx's modal loop; it never hooks another process's input.
var activeBoxMenu *boxMenuTracking

// For submenu rows WM_MENUSELECT reports the position, not a command ID.
// Use the native selection notification for keyboard input; menu highlight
// bits are painting state and are not a reliable keyboard cursor.
var boxMenuOwnerProc = syscall.NewCallback(func(hwnd win.HWND, message uint32, wParam, lParam, subclassID, refData uintptr) uintptr {
	if s := activeBoxMenu; s != nil && hwnd == s.owner {
		if message == win.WM_CANCELMODE && s.retainParent() {
			// A foreign menu's activation can cancel this thread's native
			// menu too. That handoff is not a user dismissal of WinTray.
			return 0
		}
		if message == win.WM_MENUSELECT {
			s.selected = -1
			if win.HMENU(lParam) == s.menu && uint32(wParam>>16)&win.MF_POPUP != 0 {
				position := uint32(wParam & 0xffff)
				if collectedMenuCommand(s.menu, position) != 0 {
					s.selected = int32(position)
				}
			}
		}
	}
	r, _, _ := procDefMenuSubclass.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
})

var boxMenuFilterProc = syscall.NewCallback(func(code int32, wParam uintptr, m *win.MSG) uintptr {
	if code == msgfMenu && activeBoxMenu != nil && m.Message == win.WM_CANCELMODE && activeBoxMenu.retainParent() {
		// Menu cancellation can be posted to the popup rather than its
		// owner. This hook is still confined to WinTray's menu thread.
		return 1
	}
	if code == msgfMenu && activeBoxMenu != nil && m.HWnd == activeBoxMenu.owner {
		s := activeBoxMenu
		// The parent can receive the tail of a click as the foreign menu
		// releases capture. Do not interpret that as a desktop dismissal.
		if s.popup != nil && isMenuButtonMessage(m.Message) {
			for _, popup := range s.popup.windows {
				if pointInMenuRect(popup.rect, m.Pt) {
					s.pressed = boxMenuChoice{}
					return 1
				}
			}
		}
		switch m.Message {
		case win.WM_TIMER:
			if m.WParam == boxRefreshTimer {
				if s.refresh != nil {
					s.refresh()
				}
				s.pollExpand()
				return 1
			}
		case win.WM_RBUTTONDOWN, win.WM_RBUTTONUP, win.WM_RBUTTONDBLCLK:
			if id, _ := collectedMenuHit(s.owner, s.menu, m.Pt); id != 0 {
				// A right-click on the arrow must not turn into a delayed hover
				// expansion either. Leaving that row rearms hover.
				s.rightSuppressed, s.hoverID = id, 0
				return 1
			}
		case win.WM_LBUTTONDOWN, win.WM_LBUTTONDBLCLK:
			if id, rect := collectedMenuHit(s.owner, s.menu, m.Pt); id != 0 {
				s.pressed = boxMenuChoice{id: id, expand: inMenuExpandRegion(rect, m.Pt)}
				s.hoverID, s.rightSuppressed = 0, 0
				return 1
			}
		case win.WM_LBUTTONUP:
			if pressed := s.pressed; pressed.id != 0 {
				s.pressed = boxMenuChoice{}
				id, rect := collectedMenuHit(s.owner, s.menu, m.Pt)
				if id == pressed.id && pressed.expand == inMenuExpandRegion(rect, m.Pt) {
					s.choose(pressed)
				}
				return 1
			}
		case win.WM_KEYDOWN:
			if m.WParam == win.VK_RETURN || m.WParam == win.VK_SPACE || m.WParam == win.VK_RIGHT {
				if s.selected >= 0 {
					if id := collectedMenuCommand(s.menu, uint32(s.selected)); id != 0 {
						s.choose(boxMenuChoice{id: id, expand: m.WParam == win.VK_RIGHT})
						return 1
					}
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, uintptr(unsafe.Pointer(m)))
	return r
})

// Commands, Escape and outside mouse clicks still end the normal native
// loop. Only the implicit cancellation during a program-menu handoff is held.
func (s *boxMenuTracking) retainParent() bool {
	return s.hasExpanded && s.choice.id == 0 && s.err == nil
}

func isMenuButtonMessage(message uint32) bool {
	switch message {
	case win.WM_LBUTTONDOWN, win.WM_LBUTTONUP, win.WM_LBUTTONDBLCLK, win.WM_RBUTTONDOWN, win.WM_RBUTTONUP, win.WM_RBUTTONDBLCLK:
		return true
	}
	return false
}

func (s *boxMenuTracking) choose(choice boxMenuChoice) {
	if s.choice.id != 0 || s.expanding {
		return
	}
	if choice.expand && s.expand != nil {
		if s.popup != nil {
			if s.expandedID == choice.id && !s.popup.closing {
				return
			}
			// An explicit click/key selection waits for the previous menu's
			// real dismissal. Hover requests are evaluated from the cursor.
			s.pendingID = choice.id
			s.popup.close(time.Now())
			return
		}
		s.openExpansion(choice.id)
		return
	}
	s.choice = choice
	// The original program owns the menu, including custom/owner-drawn
	// items. Release our capture before delivering its callback, otherwise
	// the next click can dismiss a menu instead of invoking its command.
	procEndMenu.Call()
}

func (s *boxMenuTracking) openExpansion(id uint32) {
	s.expanding = true
	defer func() { s.expanding = false }()
	target, err := s.expand(id)
	if err == nil {
		s.popup, err = newProgramMenu(target, s.owner, time.Now())
	}
	if err == nil {
		s.hasExpanded = true
		s.expandedID, s.pendingID = id, 0
		s.selected, s.hoverID, s.rightSuppressed = -1, 0, id
		s.outsideSince = time.Time{}
		err = target.open()
	}
	if err != nil {
		s.err = errors.Join(s.err, err)
		procEndMenu.Call()
	}
}

func (s *boxMenuTracking) pollExpand() {
	if s.choice.id != 0 || s.expanding {
		return
	}
	now := time.Now()
	if s.popup != nil {
		s.popup.observe()
		if s.popup.closing {
			s.popup.close(now)
		}
		if s.popup.done(now) {
			if s.popup.closing {
				// Restore keyboard input only after our automatic dismissal.
				// A program command may instead have opened its own window.
				win.SetForegroundWindow(s.owner)
			}
			s.popup, s.expandedID = nil, 0
		}
	}
	if s.popup == nil && s.pendingID != 0 {
		s.openExpansion(s.pendingID)
		return
	}
	// Pair GetCursorPos with current physical button state. A queued MSG
	// point does not follow the mouse once another process has capture.
	var point win.POINT
	if !win.GetCursorPos(&point) || s.pressed.id != 0 {
		return
	}
	for _, button := range []uintptr{win.VK_LBUTTON, win.VK_RBUTTON} {
		state, _, _ := procGetAsyncKeyState.Call(button)
		if int16(state) < 0 {
			return
		}
	}
	id, rect := collectedMenuHit(s.owner, s.menu, point)
	insidePopup := s.popup != nil && s.popup.contains(point)
	if insidePopup {
		id = 0 // the popup may cover another row's expansion arrow
	}
	arrow := id != 0 && inMenuExpandRegion(rect, point)
	if s.popup != nil && !s.popup.closing {
		switch {
		case insidePopup || arrow && id == s.expandedID:
			s.outsideSince = time.Time{}
		case arrow && id != s.expandedID:
			s.popup.close(now)
		default:
			if s.outsideSince.IsZero() {
				s.outsideSince = now
			}
			// A short transit interval lets the pointer cross the border
			// into the menu, without leaving abandoned popups on screen.
			if now.Sub(s.outsideSince) >= boxPopupLeaveDelay {
				s.popup.close(now)
			}
		}
	}
	if id != s.rightSuppressed && !insidePopup {
		s.rightSuppressed = 0
	}
	if !arrow || id == s.rightSuppressed {
		s.hoverID = 0
		return
	}
	if s.hoverID != id {
		s.hoverID, s.hoverSince = id, now
	}
	if s.popup == nil && now.Sub(s.hoverSince) >= s.hoverDelay {
		s.choose(boxMenuChoice{id: id, expand: true})
	}
}

// A square at the row's trailing edge includes the native arrow and gives it
// a usable hit target at the menu's current DPI. The label remains a separate
// normal-click target, and hovering it never opens a program's menu.
func inMenuExpandRegion(rect win.RECT, point win.POINT) bool {
	width := max(rect.Bottom-rect.Top, win.GetSystemMetrics(win.SM_CXMENUCHECK))
	return point.X >= max(rect.Left, rect.Right-width) && point.X < rect.Right && point.Y >= rect.Top && point.Y < rect.Bottom
}

func collectedMenuHit(owner win.HWND, menu win.HMENU, point win.POINT) (uint32, win.RECT) {
	for position, count := int32(0), win.GetMenuItemCount(menu); position < count; position++ {
		var rect win.RECT
		ok, _, _ := procGetMenuItemRect.Call(uintptr(owner), uintptr(menu), uintptr(position), uintptr(unsafe.Pointer(&rect)))
		if ok != 0 && point.X >= rect.Left && point.X < rect.Right && point.Y >= rect.Top && point.Y < rect.Bottom {
			return collectedMenuCommand(menu, uint32(position)), rect
		}
	}
	return 0, win.RECT{}
}

func collectedMenuCommand(menu win.HMENU, position uint32) uint32 {
	item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID | win.MIIM_STATE}
	if win.GetMenuItemInfo(menu, position, 1, &item) &&
		item.WID >= trayBoxedBase && item.WID < trayIDLimit && item.FState&win.MFS_DISABLED == 0 {
		return item.WID
	}
	return 0
}

// trackBoxMenu splits a collected row into a normal click and an expansion
// arrow. An expansion callback keeps this menu alive while the program's own
// native menu is opened; ordinary choices still end the current menu.
func trackBoxMenu(hwnd win.HWND, menu win.HMENU, refresh func(), track func(), expandAction ...func(uint32) (boxMenuExpansion, error)) (choice boxMenuChoice, err error) {
	if activeBoxMenu != nil {
		return boxMenuChoice{}, errors.New("tray menu is already active")
	}
	if hwnd == 0 || menu == 0 {
		return boxMenuChoice{}, errors.New("tray menu or owner is unavailable")
	}
	var expand func(uint32) (boxMenuExpansion, error)
	if len(expandAction) > 0 {
		expand = expandAction[0]
	}
	session := &boxMenuTracking{owner: hwnd, menu: menu, refresh: refresh, expand: expand, selected: -1, hoverDelay: boxHoverDelay}
	// WH_MSGFILTER = -1; a thread-local hook does not need a DLL module.
	hook, _, err := procSetWindowsHookEx.Call(^uintptr(0), boxMenuFilterProc, 0, uintptr(win.GetCurrentThreadId()))
	if hook == 0 {
		return boxMenuChoice{}, fmt.Errorf("install tray menu input handler: %w", err)
	}
	activeBoxMenu = session
	session.popup = takeRetiringProgramMenu()
	if session.popup != nil {
		session.popup.parent = hwnd
	}
	defer func() {
		if session.popup != nil {
			retireProgramMenu(hwnd, session.popup)
			if !session.popup.done(time.Now()) {
				session.err = errors.Join(session.err, session.popup.err)
			}
		}
		err = errors.Join(err, session.err)
		activeBoxMenu = nil
		procUnhookWindowsHookEx.Call(hook)
	}()
	if ok, _, err := procSetMenuSubclass.Call(uintptr(hwnd), boxMenuOwnerProc, boxRefreshTimer, 0); ok == 0 {
		return boxMenuChoice{}, fmt.Errorf("observe tray menu selection: %w", err)
	}
	defer procRemoveMenuSubclass.Call(uintptr(hwnd), boxMenuOwnerProc, boxRefreshTimer)
	if win.SetTimer(hwnd, boxRefreshTimer, 30, 0) == 0 {
		return boxMenuChoice{}, errors.New("start tray menu refresh timer")
	}
	defer win.KillTimer(hwnd, boxRefreshTimer)
	track()
	return session.choice, nil
}
