//go:build windows

package orchestrator

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LsaGetLogonSessionData returns the logon timestamp for the current token's
// authentication session, independent of when WinTray itself was started.
var logonTimeLookup = currentLogonTime

var (
	secur32                = windows.NewLazySystemDLL("secur32.dll")
	lsaGetLogonSessionData = secur32.NewProc("LsaGetLogonSessionData")
	lsaFreeReturnBuffer    = secur32.NewProc("LsaFreeReturnBuffer")
)

type logonSessionData struct {
	size                  uint32
	logonID               windows.LUID
	userName              windows.NTUnicodeString
	logonDomain           windows.NTUnicodeString
	authenticationPackage windows.NTUnicodeString
	logonType             uint32
	session               uint32
	sid                   uintptr
	logonTime             int64
}

func currentLogonTime() (time.Time, error) {
	var stats struct {
		tokenID          windows.LUID
		authenticationID windows.LUID
		padding          [128]byte
	}
	var size uint32
	if err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenStatistics,
		(*byte)(unsafe.Pointer(&stats)), uint32(unsafe.Sizeof(stats)), &size); err != nil {
		return time.Time{}, fmt.Errorf("token statistics: %w", err)
	}
	var data *logonSessionData
	status, _, _ := lsaGetLogonSessionData.Call(uintptr(unsafe.Pointer(&stats.authenticationID)), uintptr(unsafe.Pointer(&data)))
	if status != 0 {
		return time.Time{}, fmt.Errorf("logon session lookup: NTSTATUS 0x%x", status)
	}
	if data == nil {
		return time.Time{}, fmt.Errorf("logon session lookup returned no data")
	}
	defer lsaFreeReturnBuffer.Call(uintptr(unsafe.Pointer(data)))
	if data.logonTime <= 0 {
		return time.Time{}, fmt.Errorf("logon session has no sign-in time")
	}
	const unixEpochFiletime = 116444736000000000
	return time.Unix(0, (data.logonTime-unixEpochFiletime)*100), nil
}
