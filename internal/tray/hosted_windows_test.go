//go:build windows

package tray

import (
	"errors"
	"os"
	"testing"

	"github.com/lxn/walk"
	"golang.org/x/sys/windows"
)

type testLogger struct{}

func (testLogger) Info(string) {}
func (testLogger) Warn(string) {}

func TestHostAddFailureRestoresWithoutKeepingAnItem(t *testing.T) {
	for _, failure := range []string{"process unavailable", "tray unavailable"} {
		t.Run(failure, func(t *testing.T) {
			oldOpen, oldCreate := openHostedProcess, createHostedItem
			t.Cleanup(func() { openHostedProcess, createHostedItem = oldOpen, oldCreate })
			wantErr := errors.New(failure)
			var opened windows.Handle
			openHostedProcess = func(access uint32, inherit bool, pid uint32) (windows.Handle, error) {
				if failure == "process unavailable" {
					return 0, wantErr
				}
				var err error
				opened, err = windows.OpenProcess(access, inherit, pid)
				return opened, err
			}
			created := false
			createHostedItem = func(*Host, HostedWindow) (*hostedItem, error) {
				created = true
				return nil, wantErr
			}
			pid := uint32(os.Getpid())
			recoveryLookups := 0
			host := NewHost("en-US", testLogger{}, func(got uint32) uintptr {
				if got != pid {
					t.Errorf("recovery looked up pid %d, want %d", got, pid)
				}
				recoveryLookups++
				return 0 // no real window or desktop manipulation
			})
			if err := host.Add(HostedWindow{ProcessID: pid, Name: "test"}); !errors.Is(err, wantErr) {
				t.Fatalf("Add error = %v, want %v", err, wantErr)
			}
			if host.Count() != 0 || recoveryLookups != 1 {
				t.Fatalf("count=%d recovery lookups=%d; want 0 and 1", host.Count(), recoveryLookups)
			}
			if created != (failure == "tray unavailable") {
				t.Fatalf("tray creation attempted before obtaining a process handle: %t", created)
			}
			if opened != 0 {
				if _, err := windows.WaitForSingleObject(opened, 0); !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
					t.Fatalf("failed Add leaked its process handle: %v", err)
				}
			}
		})
	}
}

func TestHostedItemDisposesOwnedImageOnce(t *testing.T) {
	disposals := 0
	image := walk.NewPaintFuncImageWithDispose(walk.Size{Width: 16, Height: 16}, nil, func() { disposals++ })
	item := hostedItem{ownedIcon: image}
	item.disposeResources()
	item.disposeResources()
	if disposals != 1 {
		t.Fatalf("owned image disposed %d times, want 1", disposals)
	}
}

func TestHostedWindowRejectsMissingIdentity(t *testing.T) {
	if hostedWindowMatchesProcess(0, uint32(os.Getpid())) || hostedWindowMatchesProcess(123, 0) {
		t.Fatal("a window without a verifiable process must not be used")
	}
}

func TestReleaseHostingShowsWindowAndDropsIconWithoutQuittingProgram(t *testing.T) {
	oldDefer := deferOnUIThread
	t.Cleanup(func() { deferOnUIThread = oldDefer })
	deferOnUIThread = func(_ *hostedItem, f func()) { f() }

	// The hosted "program" is this test process: a release that quit the
	// program would end the test run, so surviving is part of the assertion.
	pid := uint32(os.Getpid())
	lookups := 0
	host := NewHost("en-US", testLogger{}, func(got uint32) uintptr {
		if got != pid {
			t.Errorf("release looked up pid %d, want %d", got, pid)
		}
		lookups++
		return 0 // no real window or desktop manipulation
	})
	emptied := 0
	host.SetOnEmpty(func() { emptied++ })
	item := &hostedItem{host: host, info: HostedWindow{ProcessID: pid, Name: "test"}, stop: make(chan struct{})}
	host.items[pid] = item

	item.releaseHosting()

	if lookups != 1 {
		t.Fatalf("window lookups = %d, want 1 (the window is shown once)", lookups)
	}
	if host.Count() != 0 || emptied != 1 {
		t.Fatalf("count=%d onEmpty calls=%d; want 0 and 1", host.Count(), emptied)
	}
	select {
	case <-item.stop:
	default:
		t.Fatal("process watcher was not stopped")
	}
	if !item.closed {
		t.Fatal("item not marked closed")
	}
}
