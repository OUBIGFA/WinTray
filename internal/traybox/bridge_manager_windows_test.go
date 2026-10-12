//go:build windows

package traybox

import (
	"errors"
	"reflect"
	"testing"
)

type bridgeFixture struct {
	manager                                   *bridgeManager
	target                                    shellEndpoint
	attempts, activated, cancelled, refreshed int
	failure                                   error
}

func newBridgeFixture() *bridgeFixture {
	f := &bridgeFixture{target: shellEndpoint{bridgeTarget{2, 20, "shell"}, 3}}
	f.manager = &bridgeManager{active: func() bool { return true }, report: func(error) {},
		scan: func() (shellEndpoint, error) { return f.target, nil },
		attach: func(shellEndpoint) (*bridgeLease, error) {
			f.attempts++
			if f.failure != nil {
				return nil, f.failure
			}
			return &bridgeLease{selectPaths: func([]string) error { return nil }, activate: func() error { f.activated++; return nil }, cancel: func() error { f.cancelled++; return nil }}, nil
		},
		refresh: func([]string) { f.refreshed++ },
	}
	return f
}
func TestBridgeSelectionConfirmsBeforeActivationAndRestoresBeforeDetach(t *testing.T) {
	f := newBridgeFixture()
	paths := []string{"a", "b"}
	if err := f.manager.apply(paths, func() error {
		if f.attempts != 1 || f.activated != 0 {
			t.Fatal("not prepared dormantly")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if f.activated != 1 {
		t.Fatal("route not activated")
	}
	if err := f.manager.apply([]string{"c"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if f.attempts != 1 {
		t.Fatal("selection reinjected applications")
	}
	if err := f.manager.apply(nil, func() error {
		if f.cancelled != 0 {
			t.Fatal("cancelled before restoration")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if f.cancelled != 1 {
		t.Fatal("did not cancel")
	}
}
func TestBridgeSelectionRollbackPreservesWorkingRoute(t *testing.T) {
	f := newBridgeFixture()
	failure := errors.New("restore failed")
	if err := f.manager.apply([]string{"a"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.apply([]string{"b"}, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if f.cancelled != 0 || !reflect.DeepEqual(f.manager.paths, []string{"a"}) {
		t.Fatal("old selection lost")
	}
}
func TestBridgeRejectedFirstSelectionNeverActivates(t *testing.T) {
	f := newBridgeFixture()
	failure := errors.New("commit failed")
	if err := f.manager.apply([]string{"a"}, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if f.activated != 0 || f.cancelled != 1 || f.manager.lease != nil {
		t.Fatal("rejected selection retained a route")
	}
}
func TestBridgeShellRestartRetriesNewIdentityNotFailedEndpoint(t *testing.T) {
	f := newBridgeFixture()
	if err := f.manager.apply([]string{"a"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.target.created++
	f.failure = errors.New("shell hook failed")
	if err := f.manager.poll(false); err == nil {
		t.Fatal("failure lost")
	}
	if err := f.manager.poll(true); err != nil || f.attempts != 2 {
		t.Fatal("failed endpoint retried")
	}
	f.target.hwnd++
	f.failure = nil
	if err := f.manager.poll(false); err != nil {
		t.Fatal(err)
	}
	if f.attempts != 3 || f.activated != 2 || f.refreshed != 1 {
		t.Fatal("new shell not refreshed")
	}
}
func TestBridgeActivationFailureLeavesNativeRouteAndAllowsExplicitRetry(t *testing.T) {
	f := newBridgeFixture()
	failure := errors.New("ready failed")
	var reported error
	f.manager.report = func(err error) { reported = err }
	attach := f.manager.attach
	f.manager.attach = func(e shellEndpoint) (*bridgeLease, error) {
		l, err := attach(e)
		l.activate = func() error { return failure }
		return l, err
	}
	if err := f.manager.apply([]string{"a"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(reported, failure) || f.manager.lease != nil || f.cancelled != 1 {
		t.Fatal("activation failure not cancelled/reported")
	}
	if err := f.manager.poll(true); err != nil || f.attempts != 1 || f.refreshed != 0 {
		t.Fatal("failure retried automatically")
	}
	f.manager.attach = attach
	if err := f.manager.apply([]string{"a"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if f.attempts != 2 || f.activated != 1 {
		t.Fatal("explicit retry failed")
	}
}
func TestBridgePausesAfterRecoveryFailure(t *testing.T) {
	f := newBridgeFixture()
	if err := f.manager.apply([]string{"a"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.manager.active = func() bool { return false }
	f.target.hwnd++
	if err := f.manager.poll(true); err != nil || f.attempts != 1 {
		t.Fatal("continued without recovery")
	}
	if err := f.manager.apply(nil, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
func TestBridgeTimeoutStopsCollectionWithoutRetryingEndpoint(t *testing.T) {
	f := newBridgeFixture()
	if err := f.manager.apply([]string{"a"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	faulted := false
	f.manager.fault = func() { faulted = true }
	f.manager.lease.alive = func() bool { return false }
	if err := f.manager.poll(false); err == nil || !faulted || f.cancelled != 1 {
		t.Fatal("timed-out route did not release collection")
	}
	if err := f.manager.poll(true); err != nil || f.attempts != 1 {
		t.Fatal("timed-out route was injected again")
	}
}

func TestBoxQueuesBackgroundFailureUntilUIExists(t *testing.T) {
	box := NewBox("", nil)
	first := errors.New("first")
	box.reportError(first)
	var got error
	box.SetErrorHandler(func(err error) { got = err })
	if !errors.Is(got, first) {
		t.Fatal("startup failure lost")
	}
}
