//go:build windows

package traybox

import (
	"crypto/rand"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const testExePath = `C:\WinTray-test-only\example.exe`

// Every registry test uses a new, private HKCU tree, never NotifyIconSettings.
func newTestSettingsKey(t *testing.T) registry.Key {
	t.Helper()
	path := `Software\WinTray-traybox-test-` + rand.Text()
	root, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		names, err := root.ReadSubKeyNames(-1)
		if err != nil {
			t.Error(err)
		}
		for _, name := range names {
			if err := registry.DeleteKey(root, name); err != nil {
				t.Errorf("delete test child %s: %v", name, err)
			}
		}
		root.Close()
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
			t.Errorf("delete test root: %v", err)
		}
		if key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ); !errors.Is(err, registry.ErrNotExist) {
			if err == nil {
				key.Close()
			}
			t.Errorf("test root still exists: %v", err)
		}
	})
	return root
}

func newTestIconKey(t *testing.T, root registry.Key, name, path string, value uint64, typ uint32) registry.Key {
	t.Helper()
	k, _, err := registry.CreateKey(root, name, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close() })
	if err := k.SetStringValue("ExecutablePath", path); err != nil {
		t.Fatal(err)
	}
	switch typ {
	case registry.DWORD:
		err = k.SetDWordValue("IsPromoted", uint32(value))
	case registry.QWORD:
		err = k.SetQWordValue("IsPromoted", value)
	}
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func assertPromotedValue(t *testing.T, k registry.Key, want uint64, wantType uint32) {
	t.Helper()
	got, typ, err := k.GetIntegerValue("IsPromoted")
	if wantType == 0 {
		if !errors.Is(err, registry.ErrNotExist) {
			t.Errorf("IsPromoted should be absent; got %d type %d, err %v", got, typ, err)
		}
		return
	}
	if err != nil || got != want || typ != wantType {
		t.Errorf("IsPromoted = %d type %d, err %v; want %d type %d", got, typ, err, want, wantType)
	}
}

// Only restrict keys created above. Denying SET_VALUE leaves reading and
// deletion available, so cleanup still removes the entire temporary tree.
func denyTestKeyWrites(t *testing.T, k registry.Key) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(D;;0x0002;;;WD)(A;;KA;;;%s)", user.User.Sid.String()))
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(windows.Handle(k), windows.SE_REGISTRY_KEY, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestUpdatePromotedDoesNotRequireWriteAccessForOtherOrUnchangedIcons(t *testing.T) {
	root := newTestSettingsKey(t)
	other := newTestIconKey(t, root, "other", `C:\WinTray-test-only\other.exe`, 1, registry.DWORD)
	unchanged := newTestIconKey(t, root, "unchanged", testExePath, 0, registry.QWORD)
	changed := newTestIconKey(t, root, "changed", testExePath, 1, registry.DWORD)
	denyTestKeyWrites(t, other)
	denyTestKeyWrites(t, unchanged)
	if err := updatePromotedAt(root, testExePath, false, nil); err != nil {
		t.Fatal(err)
	}
	assertPromotedValue(t, other, 1, registry.DWORD)
	assertPromotedValue(t, unchanged, 0, registry.QWORD)
	assertPromotedValue(t, changed, 0, registry.DWORD)
}

func TestUpdatePromotedRollsBackPartialSystemFailure(t *testing.T) {
	for _, failure := range []string{"write denied", "unreadable value"} {
		t.Run(failure, func(t *testing.T) {
			root := newTestSettingsKey(t)
			keys := make(map[string]registry.Key)
			for _, name := range []string{"a", "b", "c"} {
				keys[name] = newTestIconKey(t, root, name, testExePath, 0, 0)
			}
			// Use the registry's actual order: two updates precede the failure.
			names, err := root.ReadSubKeyNames(-1)
			if err != nil {
				t.Fatal(err)
			}
			if err := keys[names[1]].SetQWordValue("IsPromoted", 1<<40); err != nil {
				t.Fatal(err)
			}
			bad := keys[names[2]]
			wantErr := error(windows.ERROR_ACCESS_DENIED)
			if failure == "write denied" {
				denyTestKeyWrites(t, bad)
			} else {
				if err := bad.SetStringValue("IsPromoted", "not an integer"); err != nil {
					t.Fatal(err)
				}
				wantErr = registry.ErrUnexpectedType
			}
			committed := false
			err = updatePromotedAt(root, testExePath, false, func() error {
				committed = true
				return nil
			})
			if !errors.Is(err, wantErr) {
				t.Fatalf("got %v, want %v", err, wantErr)
			}
			if committed {
				t.Error("commit called after system failure")
			}
			assertPromotedValue(t, keys[names[0]], 0, 0)
			assertPromotedValue(t, keys[names[1]], 1<<40, registry.QWORD)
			if failure == "write denied" {
				assertPromotedValue(t, bad, 0, 0)
			} else if value, _, err := bad.GetStringValue("IsPromoted"); err != nil || value != "not an integer" {
				t.Errorf("unsupported value was changed: %q, %v", value, err)
			}
		})
	}
}
