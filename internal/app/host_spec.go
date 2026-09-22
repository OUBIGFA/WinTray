package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"wintray/internal/stringutil"
)

// hostFlag marks a detached tray-host launch: "wintray.exe --host ...". The
// process created this way owns exactly one hosted tray icon and nothing else.
const hostFlag = "--host"

// hostSpec describes the program a detached host process keeps in the tray.
// It travels on the host process command line, so every field must survive a
// round trip through hostArgs and parseHostArgs.
type hostSpec struct {
	PID      uint32
	Handle   uintptr
	Name     string
	ExePath  string
	Language string
}

func isHostLaunch(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, hostFlag) {
			return true
		}
	}
	return false
}

// hostArgs renders the spec as the command line of a host process.
func hostArgs(spec hostSpec) []string {
	return []string{
		hostFlag,
		"--pid", strconv.FormatUint(uint64(spec.PID), 10),
		"--hwnd", "0x" + strconv.FormatUint(uint64(spec.Handle), 16),
		"--name", spec.Name,
		"--exe", spec.ExePath,
		"--lang", spec.Language,
	}
}

// parseHostArgs reads a spec back from the command line. Unknown flags are
// rejected so a launcher typo cannot silently drop information.
func parseHostArgs(args []string) (hostSpec, error) {
	var spec hostSpec
	seenHost := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.EqualFold(arg, hostFlag) {
			seenHost = true
			continue
		}
		if i+1 >= len(args) {
			return hostSpec{}, fmt.Errorf("missing value for %s", arg)
		}
		value := args[i+1]
		i++
		switch strings.ToLower(arg) {
		case "--pid":
			pid, err := strconv.ParseUint(value, 10, 32)
			if err != nil || pid == 0 {
				return hostSpec{}, fmt.Errorf("invalid --pid %q", value)
			}
			spec.PID = uint32(pid)
		case "--hwnd":
			hwnd, err := strconv.ParseUint(value, 0, 64)
			if err != nil {
				return hostSpec{}, fmt.Errorf("invalid --hwnd %q", value)
			}
			spec.Handle = uintptr(hwnd)
		case "--name":
			spec.Name = value
		case "--exe":
			spec.ExePath = value
		case "--lang":
			spec.Language = value
		default:
			return hostSpec{}, fmt.Errorf("unknown host argument %q", arg)
		}
	}
	if !seenHost {
		return hostSpec{}, errors.New("not a host launch")
	}
	if spec.PID == 0 {
		return hostSpec{}, errors.New("--pid is required")
	}
	if spec.Name == "" {
		spec.Name = stringutil.TrimExt(filepath.Base(spec.ExePath))
	}
	return spec, nil
}
