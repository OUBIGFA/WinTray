//go:build windows

package traybox

import (
	"encoding/binary"
	"errors"
	"runtime"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Selection is a bounded wire snapshot, not pointers or memory owned by Go.
// Hosted owners are explicit HWNDs so WinTray's own icon is never collected.
func (n *nativeBridge) selectPaths(target shellEndpoint, paths []string) error {
	var owners []uint32
	if n.owners != nil {
		n.owners.Range(func(key, value any) bool {
			owner := value.(hostedIconOwner)
			if Contains(paths, owner.path) {
				owners = append(owners, key.(uint32))
			}
			return true
		})
	}
	wide := []uint16{}
	for _, path := range paths {
		text, err := windows.UTF16FromString(path)
		if err != nil {
			return err
		}
		wide = append(wide, text...)
	}
	wide = append(wide, 0)
	if len(wide) == 1 {
		wide = append(wide, 0)
	}
	data := make([]byte, 8+4*len(owners)+2*len(wide))
	if len(data) > 262144 {
		return errors.New("tray selection is too large")
	}
	binary.LittleEndian.PutUint32(data, uint32(len(wide)*2))
	binary.LittleEndian.PutUint32(data[4:], uint32(len(owners)))
	for i, owner := range owners {
		binary.LittleEndian.PutUint32(data[8+i*4:], owner)
	}
	for i, c := range wide {
		binary.LittleEndian.PutUint16(data[8+4*len(owners)+i*2:], c)
	}
	cds := copyDataStruct{bridgeSelectData, uint32(len(data)), uintptr(unsafe.Pointer(&data[0]))}
	var result uintptr
	var pin runtime.Pinner
	pin.Pin(&data[0])
	pin.Pin(&cds)
	pin.Pin(&result)
	defer pin.Unpin()
	ok, _, err := procSendMessageTimeoutW.Call(uintptr(target.hwnd), win.WM_COPYDATA, uintptr(n.broker), uintptr(unsafe.Pointer(&cds)), smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&result)))
	if ok == 0 {
		return err
	}
	if result == 0 {
		return errors.New("shell bridge rejected tray selection")
	}
	return nil
}
