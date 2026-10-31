//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

// Slots and GUIDs follow the Windows SDK UIAutomationClient.h interfaces.
// This small binding uses only the system UIA client, with no Python runtime.
const (
	uiaRelease               = 2
	uiaElementFromHandle     = 6
	uiaCreateTrueCondition   = 21
	uiaPutAutoSetFocus       = 59
	uiaPutConnectionTimeout  = 61
	uiaPutTransactionTimeout = 63
	uiaFindAll               = 6
	uiaGetCurrentPattern     = 16
	uiaCurrentProcessID      = 20
	uiaCurrentControlType    = 21
	uiaCurrentName           = 23
	uiaCurrentIsEnabled      = 28
	uiaCurrentClassName      = 30
	uiaCurrentIsOffscreen    = 38
	uiaCurrentBoundingRect   = 43
	uiaArrayLength           = 3
	uiaArrayElement          = 4
	uiaInvoke                = 3
	uiaInvokePatternID       = 10000
	uiaButtonControlTypeID   = 50000
	uiaTreeScopeDescendants  = 4
)

type weChatAutomation struct{ object, condition *weChatUIAObject }
type weChatUIAObject struct{ vtable *uintptr }

func (o *weChatUIAObject) call(slot int, args ...uintptr) error {
	method := *(*uintptr)(unsafe.Add(unsafe.Pointer(o.vtable), uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	runtime.KeepAlive(o)
	if int32(hr) < 0 {
		return fmt.Errorf("UIA method %d: HRESULT 0x%08X", slot, uint32(hr))
	}
	return nil
}

func (o *weChatUIAObject) release() {
	if o != nil {
		_ = o.call(uiaRelease)
	} // IUnknown::Release returns a reference count.
}

// Pin out parameters before building the syscall argument slice: a Go stack
// may move while a provider is servicing a call on another thread.
func weChatUIARead[T any](o *weChatUIAObject, slot int, args ...uintptr) (T, error) {
	var value T
	var pin runtime.Pinner
	pin.Pin(&value)
	defer pin.Unpin()
	err := o.call(slot, append(args, uintptr(unsafe.Pointer(&value)))...)
	return value, err
}

func (o *weChatUIAObject) text(slot int) (string, error) {
	value, err := weChatUIARead[*uint16](o, slot)
	if value != nil {
		defer win.SysFreeString(value)
	}
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	return win.BSTRToString(value), nil
}

// The caller must lock its OS thread until close. CUIAutomation8 exposes
// provider timeouts on Windows 8+, avoiding unbounded waits on a frozen client.
func newWeChatAutomation() (*weChatAutomation, error) {
	if hr := win.CoInitializeEx(nil, win.COINIT_MULTITHREADED); hr < 0 {
		return nil, fmt.Errorf("initialize UIA COM: HRESULT 0x%08X", uint32(hr))
	}
	clsid := win.CLSID{Data1: 0xe22ad333, Data2: 0xb25f, Data3: 0x460c, Data4: [8]byte{0x83, 0xd0, 0x05, 0x81, 0x10, 0x73, 0x95, 0xc9}}
	iid := win.IID{Data1: 0x34723aff, Data2: 0x0c9d, Data3: 0x49d0, Data4: [8]byte{0x98, 0x96, 0x7a, 0xb5, 0x2d, 0xf8, 0xcd, 0x8a}}
	var object unsafe.Pointer
	var pin runtime.Pinner
	pin.Pin(&object)
	hr := win.CoCreateInstance(&clsid, nil, win.CLSCTX_INPROC_SERVER, &iid, &object)
	pin.Unpin()
	if hr < 0 || object == nil {
		win.CoUninitialize()
		return nil, fmt.Errorf("create UIA client: HRESULT 0x%08X", uint32(hr))
	}
	a := &weChatAutomation{object: (*weChatUIAObject)(object)}
	for _, setting := range []struct {
		slot  int
		value uintptr
	}{{uiaPutConnectionTimeout, 2000}, {uiaPutTransactionTimeout, 2000}, {uiaPutAutoSetFocus, 0}} {
		if err := a.object.call(setting.slot, setting.value); err != nil {
			a.close()
			return nil, err
		}
	}
	var err error
	a.condition, err = weChatUIARead[*weChatUIAObject](a.object, uiaCreateTrueCondition)
	if err != nil || a.condition == nil {
		a.close()
		return nil, errors.Join(errors.New("create UIA search condition"), err)
	}
	return a, nil
}

func (a *weChatAutomation) close() {
	a.condition.release()
	a.object.release()
	win.CoUninitialize()
}

func (a *weChatAutomation) root(ctx context.Context, window HiddenWindow) (*weChatUIAObject, weChatWindowKind, error) {
	if err := ctx.Err(); err != nil {
		return nil, weChatUnknown, err
	}
	if window.Handle == 0 || window.ProcessID == 0 || windowIdentity(window.Handle).ProcessID != window.ProcessID {
		return nil, weChatUnknown, errors.New("WeChat window identity changed")
	}
	root, err := weChatUIARead[*weChatUIAObject](a.object, uiaElementFromHandle, window.Handle)
	if err != nil || root == nil {
		return nil, weChatUnknown, errors.Join(errors.New("read WeChat UIA root"), err)
	}
	pid, err := weChatUIARead[int32](root, uiaCurrentProcessID)
	if err != nil || uint32(pid) != window.ProcessID {
		root.release()
		return nil, weChatUnknown, errors.Join(errors.New("UIA process mismatch"), err)
	}
	class, err := root.text(uiaCurrentClassName)
	if err != nil {
		root.release()
		return nil, weChatUnknown, err
	}
	kind := weChatKind(class)
	if kind == weChatUnknown && weChatCandidateClass(class) {
		main, err := a.renderedMain(ctx, root, window.ProcessID)
		if err != nil {
			root.release()
			return nil, weChatUnknown, err
		}
		if main {
			kind = weChatMainWindow
		}
	}
	return root, kind, nil
}

// Weixin 4.1.12+ can keep the main UIA tree disabled after a fresh launch.
// Its native chat render surface remains visible under the titled Qt shell.
// Require both identities, visibility and the same PID; a generic Qt window
// alone must never confirm login or authorize dismissing an account's window.
func (a *weChatAutomation) renderedMain(ctx context.Context, root *weChatUIAObject, pid uint32) (bool, error) {
	name, err := root.text(uiaCurrentName)
	if err != nil || (name != "微信" && name != "Weixin" && name != "WeChat") {
		return false, err
	}
	elements, err := weChatUIARead[*weChatUIAObject](root, uiaFindAll, uiaTreeScopeDescendants, uintptr(unsafe.Pointer(a.condition)))
	if err != nil || elements == nil {
		return false, err
	}
	defer elements.release()
	count, err := weChatUIARead[int32](elements, uiaArrayLength)
	if err != nil {
		return false, err
	}
	if count < 0 || count > 512 {
		return false, errors.New("unexpected WeChat main tree size")
	}
	for i := int32(0); i < count; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		element, err := weChatUIARead[*weChatUIAObject](elements, uiaArrayElement, uintptr(i))
		if err != nil || element == nil {
			return false, errors.Join(errors.New("read WeChat main element"), err)
		}
		class, err := element.text(uiaCurrentClassName)
		if err != nil || class != "MMUIRenderSubWindowHW" {
			element.release()
			if err != nil {
				return false, err
			}
			continue
		}
		owner, pidErr := weChatUIARead[int32](element, uiaCurrentProcessID)
		offscreen, visibilityErr := weChatUIARead[int32](element, uiaCurrentIsOffscreen)
		element.release()
		return uint32(owner) == pid && offscreen == 0, errors.Join(pidErr, visibilityErr)
	}
	return false, nil
}

func (a *weChatAutomation) observe(ctx context.Context, window HiddenWindow) (weChatObservation, error) {
	result := weChatObservation{Window: window}
	root, kind, err := a.root(ctx, window)
	if err != nil {
		return result, err
	}
	defer root.release()
	result.Kind = kind
	if kind != weChatLoginWindow {
		return result, nil
	}
	button, err := a.loginButton(ctx, root)
	if button != nil {
		result.Ready = true
		button.release()
	}
	return result, err
}

// Search the full login subtree, not a fixed depth or screen coordinate.
// Stop on ambiguity; account selection and QR/phone prompts stay with the user.
func (a *weChatAutomation) loginButton(ctx context.Context, root *weChatUIAObject) (*weChatUIAObject, error) {
	elements, err := weChatUIARead[*weChatUIAObject](root, uiaFindAll, uiaTreeScopeDescendants, uintptr(unsafe.Pointer(a.condition)))
	if err != nil {
		return nil, err
	}
	if elements == nil {
		return nil, nil
	}
	defer elements.release()
	count, err := weChatUIARead[int32](elements, uiaArrayLength)
	if err != nil {
		return nil, err
	}
	if count < 0 || count > 512 {
		return nil, errors.New("unexpected WeChat login tree size")
	}
	var found *weChatUIAObject
	for i := int32(0); i < count; i++ {
		if err := ctx.Err(); err != nil {
			found.release()
			return nil, err
		}
		element, err := weChatUIARead[*weChatUIAObject](elements, uiaArrayElement, uintptr(i))
		if err != nil || element == nil {
			found.release()
			return nil, errors.Join(errors.New("read WeChat login element"), err)
		}
		ready, err := weChatButtonReady(element)
		if err != nil {
			element.release()
			found.release()
			return nil, err
		}
		if !ready {
			element.release()
			continue
		}
		if found != nil {
			element.release()
			found.release()
			return nil, errors.New("multiple WeChat login buttons")
		}
		found = element
	}
	return found, nil
}

func weChatButtonReady(element *weChatUIAObject) (bool, error) {
	kind, err := weChatUIARead[int32](element, uiaCurrentControlType)
	if err != nil || kind != uiaButtonControlTypeID {
		return false, err
	}
	name, err := element.text(uiaCurrentName)
	if err != nil || !weChatLoginButton(name) {
		return false, err
	}
	enabled, err := weChatUIARead[int32](element, uiaCurrentIsEnabled)
	if err != nil || enabled == 0 {
		return false, err
	}
	offscreen, err := weChatUIARead[int32](element, uiaCurrentIsOffscreen)
	return offscreen == 0, err
}

func (a *weChatAutomation) clickLogin(ctx context.Context, window HiddenWindow) error {
	root, kind, err := a.root(ctx, window)
	if err != nil {
		return err
	}
	defer root.release()
	if kind != weChatLoginWindow {
		return errors.New("WeChat login window changed")
	}
	button, err := a.loginButton(ctx, root)
	if err != nil {
		return err
	}
	if button == nil {
		return errors.New("WeChat login button unavailable")
	}
	defer button.release()
	class, err := root.text(uiaCurrentClassName)
	if err != nil {
		return err
	}
	// Weixin 4's Qt provider advertises Invoke but returns success without
	// activating this button. Deliver one click to its login HWND instead.
	if class == "mmui::LoginWindow" {
		return clickWeChatQtButton(ctx, window, button)
	}
	pattern, err := weChatUIARead[*weChatUIAObject](button, uiaGetCurrentPattern, uiaInvokePatternID)
	if err != nil {
		return err
	}
	if pattern == nil {
		return errors.New("WeChat login button does not support Invoke")
	}
	defer pattern.release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if windowIdentity(window.Handle).ProcessID != window.ProcessID {
		return errors.New("WeChat window changed before Invoke")
	}
	return pattern.call(uiaInvoke)
}

func clickWeChatQtButton(ctx context.Context, window HiddenWindow, button *weChatUIAObject) error {
	// UIA rectangles use physical screen pixels. Match that coordinate space
	// during conversion, including when tests or the caller are DPI-unaware.
	setDPI := user32.NewProc("SetThreadDpiAwarenessContext")
	previous, _, dpiErr := setDPI.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	if previous == 0 {
		return fmt.Errorf("set WeChat click DPI context: %w", dpiErr)
	}
	defer setDPI.Call(previous)
	rect, err := weChatUIARead[win.RECT](button, uiaCurrentBoundingRect)
	if err != nil {
		return err
	}
	if rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return errors.New("WeChat login button has no clickable rectangle")
	}
	hwnd := win.HWND(window.Handle)
	point := win.POINT{X: rect.Left + (rect.Right-rect.Left)/2, Y: rect.Top + (rect.Bottom-rect.Top)/2}
	var client win.RECT
	if !win.ScreenToClient(hwnd, &point) || !win.GetClientRect(hwnd, &client) {
		return errors.New("read WeChat login client coordinates")
	}
	if point.X < 0 || point.Y < 0 || point.X >= client.Right || point.Y >= client.Bottom || point.X > 32767 || point.Y > 32767 {
		return errors.New("WeChat login button is outside the client area")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if windowIdentity(window.Handle).ProcessID != window.ProcessID || !isWindowVisible(window.Handle) || win.IsIconic(hwnd) {
		return errors.New("WeChat login window changed before click")
	}
	// HWND-local messages avoid moving the user's pointer or clicking another
	// application that happens to cover the login window.
	position := uintptr(uint16(point.X)) | uintptr(uint16(point.Y))<<16
	for _, input := range []struct {
		message uint32
		keys    uintptr
	}{{win.WM_MOUSEMOVE, 0}, {win.WM_LBUTTONDOWN, win.MK_LBUTTON}, {win.WM_LBUTTONUP, 0}} {
		if ok, _, err := procPostMessageW.Call(window.Handle, uintptr(input.message), input.keys, position); ok == 0 {
			return fmt.Errorf("send WeChat login click: %w", err)
		}
	}
	return nil
}
