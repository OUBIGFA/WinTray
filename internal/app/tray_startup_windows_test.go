//go:build windows

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"wintray/internal/ipc"
	"wintray/internal/traybox"
)

func startupFixture(t *testing.T, setPaths func([]string) error) *trayStartup {
	t.Helper()
	s := &trayStartup{setPaths: setPaths,
		readyName: fmt.Sprintf("WinTray_StartupTest_%d_%d", os.Getpid(), time.Now().UnixNano()),
		ready:     make(chan struct{}),
	}
	t.Cleanup(s.close)
	return s
}

func assertTrayStartupReady(t *testing.T, s *trayStartup, want bool) {
	t.Helper()
	if got := ipc.WaitReady(s.readyName, 0, time.Millisecond); got != want {
		t.Fatalf("logon-task marker ready=%t, want %t", got, want)
	}
	select {
	case <-s.ready:
		if !want {
			t.Fatal("in-process startup released before collection succeeded")
		}
	default:
		if want {
			t.Fatal("collector recovered but in-process startup is still blocked")
		}
	}
}

// Reproduce sign-in without killing Explorer: the collector's first two
// attempts see the exact missing-taskbar error from currentShellEndpoint.
// Exercise the real retry dispatcher and cross-process readiness event.
func TestTrayStartupRetriesMissingTaskbarAndReleasesTasks(t *testing.T) {
	calls := 0
	selected := []string{"boot.exe"}
	s := startupFixture(t, func(paths []string) error {
		calls++
		if calls < 3 {
			return errors.Join(traybox.ErrTaskbarUnavailable, errors.New("cleanup diagnostic"))
		}
		if !reflect.DeepEqual(paths, []string{"current.exe"}) {
			t.Fatalf("retry restored stale startup selection: %v", paths)
		}
		return nil
	})
	if err := s.collect(selected); !errors.Is(err, traybox.ErrTaskbarUnavailable) {
		t.Fatal(err)
	}
	assertTrayStartupReady(t, s, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queued := make(chan func(), 1)
	done := make(chan struct{})
	var reported error
	go func() {
		defer close(done)
		retryTrayCollection(ctx, func(f func()) { queued <- f }, func() error {
			return s.collect(selected)
		}, func(err error) { reported = err })
	}()
	for calls < 3 {
		select {
		case f := <-queued:
			selected = []string{"current.exe"} // edit while waiting for Explorer
			f()
			assertTrayStartupReady(t, s, calls == 3)
		case <-ctx.Done():
			t.Fatal("taskbar readiness was never retried")
		}
	}
	<-done
	if reported != nil {
		t.Fatalf("temporary taskbar absence was reported as permanent: %v", reported)
	}
	// A later successful selection must not republish/close the local gate.
	if err := s.collect(selected); err != nil {
		t.Fatal(err)
	}
	assertTrayStartupReady(t, s, true)
	s.close()
	if ipc.WaitReady(s.readyName, 0, time.Millisecond) {
		t.Fatal("session exit retained its startup marker")
	}
}

func TestTrayStartupManualRecoveryPublishesReadiness(t *testing.T) {
	for _, paths := range [][]string{{"selected.exe"}, nil} {
		t.Run(fmt.Sprintf("selected=%d", len(paths)), func(t *testing.T) {
			failure := errors.New("component unavailable")
			s := startupFixture(t, func([]string) error { return failure })
			if err := s.collect([]string{"boot.exe"}); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			assertTrayStartupReady(t, s, false)
			failure = nil
			if err := s.collect(paths); err != nil {
				t.Fatal(err)
			}
			assertTrayStartupReady(t, s, true)
		})
	}
}

func TestTrayStartupRetryStopsOnPermanentFailureOrExit(t *testing.T) {
	for _, exiting := range []bool{false, true} {
		t.Run(fmt.Sprintf("exiting=%t", exiting), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			failure := errors.New("bridge component unavailable")
			calls := 0
			s := startupFixture(t, func([]string) error { calls++; return failure })
			queued := make(chan func(), 1)
			done := make(chan struct{})
			var reported error
			go func() {
				defer close(done)
				retryTrayCollection(ctx, func(f func()) { queued <- f }, func() error {
					return s.collect([]string{"selected.exe"})
				}, func(err error) { reported = err })
			}()
			var callback func()
			select {
			case callback = <-queued:
			case <-ctx.Done():
				t.Fatal("retry was not queued")
			}
			if exiting {
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("exit waited for the UI callback")
				}
			}
			callback() // simulate a callback already queued before Exit
			<-done
			assertTrayStartupReady(t, s, false)
			if exiting {
				if calls != 0 || reported != nil {
					t.Fatal("late retry collected icons or showed an error after exit")
				}
			} else if calls != 1 || !errors.Is(reported, failure) {
				t.Fatalf("permanent error must stop and report: calls=%d err=%v", calls, reported)
			}
		})
	}
}
