//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"wintray/internal/config"
)

// Clicking only starts authentication. Only a same-process main window proves completion.
func TestWeChatWaitRequiresMainWindow(t *testing.T) {
	login := weChatObservation{Window: HiddenWindow{Handle: 10, ProcessID: 20}, Kind: weChatLoginWindow, Ready: true}
	main := weChatObservation{Window: HiddenWindow{Handle: 11, ProcessID: 20}, Kind: weChatMainWindow}
	for _, tc := range []struct {
		name   string
		frames [][]weChatObservation
		want   HiddenWindow
		clicks int
	}{
		{"login then main", [][]weChatObservation{{login}, {login}, {main}}, main.Window, 1},
		{"already logged in", [][]weChatObservation{{main}}, main.Window, 0},
		{"phone confirmation", [][]weChatObservation{{login}}, HiddenWindow{}, 1},
		{"QR code", [][]weChatObservation{{{Window: login.Window, Kind: weChatLoginWindow}}}, HiddenWindow{}, 0},
		{"multiple accounts", [][]weChatObservation{{login, main}}, HiddenWindow{}, 0},
		{"process changed", [][]weChatObservation{{login}, {{Window: HiddenWindow{Handle: 12, ProcessID: 21}, Kind: weChatMainWindow}}}, HiddenWindow{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
			defer cancel()
			frame, clicks := 0, 0
			got, err := waitWeChatLogin(ctx, time.Millisecond, func() ([]weChatObservation, error) {
				i := min(frame, len(tc.frames)-1)
				frame++
				return tc.frames[i], nil
			}, func(w HiddenWindow) error {
				if w != login.Window {
					t.Fatalf("wrong login target: %+v", w)
				}
				clicks++
				return nil
			})
			if got != tc.want || (err != nil) != (tc.want.Handle == 0) || clicks != tc.clicks {
				t.Fatalf("got=%+v err=%v clicks=%d", got, err, clicks)
			}
		})
	}
}

func TestWeChatLoginPrecedesWindowDismissal(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(map[bool]string{false: "incomplete", true: "complete"}[success], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Weixin.exe")
			if err := os.WriteFile(path, []byte("stub"), 0600); err != nil {
				t.Fatal(err)
			}
			entry := config.ManagedAppEntry{Name: "WeChat", ExePath: path, RunOnStartup: true, WeChatAutoLogin: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}}
			enum := &testEnumerator{windows: []ManagedWindowInfo{{Handle: 0x101, ProcessID: 1234, ProcessPath: path, ProcessName: "Weixin", Title: "Login", ClassName: "WeChatLoginWnd"}}}
			mgr := &testManager{}
			svc := NewService(enum, mgr, &testLogger{})
			svc.wechatLogin = func(context.Context, string) (HiddenWindow, error) {
				if len(mgr.closeCalls)+len(mgr.escapeCalls)+len(mgr.hideCalls) != 0 {
					t.Fatal("window dismissed before login")
				}
				if !success {
					return HiddenWindow{}, context.DeadlineExceeded
				}
				enum.windows = append(enum.windows, ManagedWindowInfo{Handle: 0x102, ProcessID: 1234, ProcessPath: path, ProcessName: "Weixin", Title: "WeChat", ClassName: "WeChatMainWndForPC"})
				return HiddenWindow{Handle: 0x102, ProcessID: 1234}, nil
			}
			result := svc.StartAndManage(context.Background(), entry, 0)
			if !success {
				if result.Code != ResultWeChatLoginIncomplete || result.Managed || len(mgr.closeCalls)+len(mgr.escapeCalls)+len(mgr.hideCalls) != 0 {
					t.Fatalf("incomplete login dismissed: %+v", result)
				}
			} else if !result.Managed || len(mgr.closeCalls) != 1 || mgr.closeCalls[0] != 0x102 {
				t.Fatalf("wrong dismissal: %+v calls=%v", result, mgr.closeCalls)
			}
		})
	}
}

func TestWeChatWaitCancellationAndInvokeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitWeChatLogin(ctx, time.Millisecond, func() ([]weChatObservation, error) { t.Fatal("scan after cancellation"); return nil, nil }, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := errors.New("invoke failed")
	_, err = waitWeChatLogin(context.Background(), time.Millisecond, func() ([]weChatObservation, error) {
		return []weChatObservation{{Window: HiddenWindow{1, 2}, Kind: weChatLoginWindow, Ready: true}}, nil
	}, func(HiddenWindow) error { return want })
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
}
