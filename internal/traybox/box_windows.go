//go:build windows

package traybox

import (
	"errors"
)

// Box manages icons for programs explicitly selected in WinTray's program list.
// Its callbacks and state changes run on the UI thread.
type Box struct {
	selfPath       string
	paths          func() []string
	updatePromoted func(string, bool, func() error) error
	updatePaths    func([]string, bool, func() error) error
}

func NewBox(selfPath string, paths func() []string) *Box {
	return &Box{selfPath: selfPath, paths: paths, updatePromoted: updatePromoted, updatePaths: updatePromotedPaths}
}

func (b *Box) HasSelection() bool { return len(b.paths()) != 0 }

// SetSelected updates the system icon setting before committing the matching
// program's checkbox. On failure, the prior icon setting is restored.
func (b *Box) SetSelected(exePath string, selected bool, commit func() error) error {
	return b.updatePromoted(exePath, !selected, commit)
}

type View struct {
	Boxed []Located
	Err   error
}

// Apply hides selected programs' icons again after they start or update.
func (b *Box) Apply() error {
	var errs []error
	for _, p := range b.paths() {
		errs = append(errs, SetPromoted(p, false))
	}
	return errors.Join(errs...)
}

func (b *Box) View() View {
	if !b.HasSelection() {
		return View{}
	}
	applyErr := b.Apply()
	if errors.Is(applyErr, ErrUnsupported) {
		return View{Err: applyErr}
	}
	icons, err := ReadIcons()
	if err != nil {
		return View{Err: err}
	}
	v := View{Err: applyErr}
	selected := b.paths()
	for _, icon := range Locate(icons, b.selfPath) {
		if Contains(selected, icon.ExePath) {
			v.Boxed = append(v.Boxed, icon)
		}
	}
	return v
}

// Release restores selected programs' icons before resetting WinTray's data.
func (b *Box) Release() error {
	if !b.HasSelection() {
		return nil
	}
	return b.updatePaths(b.paths(), true, nil)
}

// RestorePaths restores icons selected by the previous global tray-box setting.
// The caller must persist its removal only after this succeeds.
func RestorePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return updatePromotedPaths(paths, true, nil)
}
