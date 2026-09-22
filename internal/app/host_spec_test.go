package app

import (
	"strings"
	"testing"
)

func TestHostArgs_RoundTrip(t *testing.T) {
	want := hostSpec{
		PID:      4321,
		Handle:   0x9A1F0,
		Name:     `Sync "thing" 主程序`,
		ExePath:  `C:\Program Files\Syncthing\syncthing.exe`,
		Language: "zh-CN",
	}
	args := hostArgs(want)
	if !isHostLaunch(args) {
		t.Fatalf("hostArgs(%+v) = %v, not recognised as a host launch", want, args)
	}
	got, err := parseHostArgs(args)
	if err != nil {
		t.Fatalf("parseHostArgs(%v): %v", args, err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestParseHostArgs_DefaultsNameFromExecutable(t *testing.T) {
	got, err := parseHostArgs([]string{"--host", "--pid", "77", "--hwnd", "0x0", "--name", "", "--exe", `D:\tools\frpc.exe`, "--lang", "en-US"})
	if err != nil {
		t.Fatalf("parseHostArgs: %v", err)
	}
	if got.Name != "frpc" || got.Handle != 0 || got.PID != 77 {
		t.Fatalf("spec = %+v, want name frpc, handle 0, pid 77", got)
	}
}

func TestParseHostArgs_RejectsBrokenLaunches(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "not a host launch", args: []string{"--pid", "5"}, want: "not a host launch"},
		{name: "missing pid", args: []string{"--host", "--name", "x"}, want: "--pid is required"},
		{name: "zero pid", args: []string{"--host", "--pid", "0"}, want: "invalid --pid"},
		{name: "bad hwnd", args: []string{"--host", "--pid", "5", "--hwnd", "zz"}, want: "invalid --hwnd"},
		{name: "unknown flag", args: []string{"--host", "--pid", "5", "--bogus", "1"}, want: "unknown host argument"},
		{name: "dangling flag", args: []string{"--host", "--pid"}, want: "missing value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseHostArgs(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseHostArgs(%v) error = %v, want containing %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestHostLaunch_IsNotAnAutorunOrInteractiveLaunch(t *testing.T) {
	args := hostArgs(hostSpec{PID: 1, Name: "x"})
	if isAutorunLaunch(args) || isCleanupRestoreLaunch(args) {
		t.Fatalf("host launch %v must not be treated as autorun or cleanup", args)
	}
	if isHostLaunch([]string{"--autorun", "--background"}) {
		t.Fatal("regular launches must not be treated as host launches")
	}
}
