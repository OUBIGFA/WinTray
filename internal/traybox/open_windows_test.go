//go:build windows

package traybox

import (
	"context"
	"fmt"
	"github.com/lxn/win"
	"testing"
)

// Real callback messages are counted; opening is an actual visible window,
// not the fact that SendNotifyMessage returned success.
func TestOpenIconFallsBackOnlyWhenSingleClickDoesNotOpen(t *testing.T) {
	for _, singleOpens := range []bool{true, false} {
		t.Run(fmt.Sprint(singleOpens), func(t *testing.T) {
			f, l := selectedListener(t)
			win.SetWindowPos(f.program, 0, -10000, -10000, 320, 200, win.SWP_NOACTIVATE|win.SWP_NOZORDER)
			send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 12, flags: nifMessage, callback: testCallback})
			f.mu.Lock()
			f.onEvent = func(hwnd win.HWND, _, event uintptr) {
				if event == win.WM_LBUTTONDBLCLK || singleOpens && event == win.WM_LBUTTONUP {
					win.ShowWindow(hwnd, win.SW_SHOWNOACTIVATE)
				}
			}
			f.mu.Unlock()
			if err := OpenIcon(context.Background(), l.snapshot()[0], nil); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "opened program", func() bool { return win.IsWindowVisible(f.program) })
			_, events, _ := f.snapshot()
			double := 0
			for _, e := range events {
				if e.lParam == win.WM_LBUTTONDBLCLK {
					double++
				}
			}
			want := 1
			if singleOpens {
				want = 0
			}
			if double != want {
				t.Fatalf("singleOpens=%t: double clicks=%d want=%d", singleOpens, double, want)
			}
			win.ShowWindow(f.program, win.SW_HIDE)
		})
	}
}

func TestOpenIconCancellationDoesNotSendClicks(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 12, flags: nifMessage, callback: testCallback})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := OpenIcon(ctx, l.snapshot()[0], nil); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	_, events, _ := f.snapshot()
	if len(events) != 0 {
		t.Fatal("cancelled open sent clicks")
	}
}

func TestOpenIconDoesNotDoubleClickRemovedRegistration(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 12, flags: nifMessage, callback: testCallback})
	if err := OpenIcon(context.Background(), l.snapshot()[0], func() (Icon, error) { return Icon{}, ErrIconGone }); err != ErrIconGone {
		t.Fatalf("removed icon: %v", err)
	}
	_, events, _ := f.snapshot()
	for _, event := range events {
		if event.lParam == win.WM_LBUTTONDBLCLK {
			t.Fatal("double-clicked a removed icon")
		}
	}
}
