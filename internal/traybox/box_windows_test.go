//go:build windows

package traybox

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func newRegistryTestBox(root registry.Key, paths func() []string) *Box {
	box := NewBox(`C:\WinTray-test-only\WinTray.exe`, paths)
	box.updatePromoted = func(path string, promoted bool, commit func() error) error {
		return updatePromotedAt(root, path, promoted, commit)
	}
	box.updatePaths = func(paths []string, promoted bool, commit func() error) error {
		return updatePromotedPathsAt(root, paths, promoted, commit)
	}
	return box
}

func TestBoxSetSelectedUpdatesOnlyMatchingProgram(t *testing.T) {
	root := newTestSettingsKey(t)
	first := newTestIconKey(t, root, "first", testExePath, 1, registry.DWORD)
	otherPath := `C:\WinTray-test-only\second.exe`
	other := newTestIconKey(t, root, "other", otherPath, 1, registry.DWORD)
	var paths []string
	box := newRegistryTestBox(root, func() []string { return paths })
	if box.HasSelection() || box.View().Err != nil || box.Release() != nil {
		t.Fatal("empty selection should not access the notification registry")
	}
	if err := box.SetSelected(testExePath, true, func() error {
		assertPromotedValue(t, first, 0, registry.DWORD)
		assertPromotedValue(t, other, 1, registry.DWORD)
		paths = []string{testExePath}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !box.HasSelection() {
		t.Fatal("selection was not committed")
	}
	if err := box.SetSelected(testExePath, false, func() error {
		assertPromotedValue(t, first, 1, registry.DWORD)
		paths = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertPromotedValue(t, other, 1, registry.DWORD)
}

func TestBoxSetSelectedSaveFailureRestoresEachOriginalValue(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "collect", true: "restore"}[selected], func(t *testing.T) {
			root := newTestSettingsKey(t)
			original := []struct {
				name  string
				value uint64
				typ   uint32
			}{
				{"hidden", 0, registry.DWORD},
				{"visible", 1, registry.DWORD},
				{"missing", 0, 0},
				{"other integer", 7, registry.DWORD},
				{"qword", 1 << 40, registry.QWORD},
			}
			var keys []registry.Key
			for _, state := range original {
				keys = append(keys, newTestIconKey(t, root, state.name, strings.ToUpper(testExePath), state.value, state.typ))
			}
			other := newTestIconKey(t, root, "unrelated", `C:\WinTray-test-only\other.exe`, 1, registry.DWORD)
			saveErr := errors.New("disk full")
			box := newRegistryTestBox(root, func() []string { return nil })
			if err := box.SetSelected(testExePath, !selected, func() error {
				want := uint64(0)
				if selected {
					want = 1
				}
				for _, k := range keys {
					assertPromotedValue(t, k, want, registry.DWORD)
				}
				return saveErr
			}); !errors.Is(err, saveErr) {
				t.Fatalf("got %v, want save failure", err)
			}
			for i, state := range original {
				assertPromotedValue(t, keys[i], state.value, state.typ)
			}
			assertPromotedValue(t, other, 1, registry.DWORD)
		})
	}
}

func TestBoxSetSelectedSystemFailureDoesNotCommit(t *testing.T) {
	root := newTestSettingsKey(t)
	icon := newTestIconKey(t, root, "icon", testExePath, 1, registry.DWORD)
	denyTestKeyWrites(t, icon)
	box := newRegistryTestBox(root, func() []string { return nil })
	if err := box.SetSelected(testExePath, true, func() error {
		t.Error("committed after system failure")
		return nil
	}); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("got %v, want access denied", err)
	}
	assertPromotedValue(t, icon, 1, registry.DWORD)
}

func TestBoxSetSelectedJoinsSaveAndRollbackFailures(t *testing.T) {
	root := newTestSettingsKey(t)
	keys := make(map[string]registry.Key)
	for _, name := range []string{"a", "b", "c"} {
		keys[name] = newTestIconKey(t, root, name, testExePath, 1, registry.DWORD)
	}
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		t.Fatal(err)
	}
	deleted := names[len(names)-1]
	saveErr := errors.New("cannot persist settings")
	box := newRegistryTestBox(root, func() []string { return nil })
	err = box.SetSelected(testExePath, true, func() error {
		if err := registry.DeleteKey(root, deleted); err != nil {
			t.Fatal(err)
		}
		return saveErr
	})
	if !errors.Is(err, saveErr) || !errors.Is(err, windows.ERROR_KEY_DELETED) {
		t.Fatalf("got %v, want both save and rollback failures", err)
	}
	if !strings.Contains(err.Error(), "restore "+deleted+" IsPromoted") {
		t.Errorf("rollback error lacks icon context: %v", err)
	}
	for _, name := range names[:len(names)-1] {
		assertPromotedValue(t, keys[name], 1, registry.DWORD)
	}
}

func TestBoxReleaseRestoresAllSelectedProgramsAtomically(t *testing.T) {
	root := newTestSettingsKey(t)
	first := newTestIconKey(t, root, "a", testExePath, 0, registry.DWORD)
	second := newTestIconKey(t, root, "b", `C:\WinTray-test-only\second.exe`, 0, registry.DWORD)
	box := newRegistryTestBox(root, func() []string {
		return []string{testExePath, `C:\WinTray-test-only\second.exe`}
	})
	if err := box.Release(); err != nil {
		t.Fatal(err)
	}
	assertPromotedValue(t, first, 1, registry.DWORD)
	assertPromotedValue(t, second, 1, registry.DWORD)
}
