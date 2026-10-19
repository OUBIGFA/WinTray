//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestScriptCommandMatchesOnlyActualScriptOperand(t *testing.T) {
	for _, tc := range []struct {
		script, command string
		want            bool
	}{
		{`C:\My Scripts\job.cmd`, `cmd.exe /d /s /c ""C:\My Scripts\job.cmd" "arg one""`, true},
		{`C:\My Scripts\job.bat`, `cmd.exe /c "C:\My Scripts\job.bat"`, true},
		{`C:\My Scripts\job.cmd`, `cmd.exe /d /s /c ""C:\My Scripts\other.cmd" "C:\My Scripts\job.cmd""`, false},
		{`C:\Scripts\job.cmd`, `cmd.exe /c echo C:\Scripts\job.cmd`, false},
		{`C:\Scripts\job.cmd`, `cmd.exe /c job.cmd`, false},
		{`C:\My Scripts\job.ps1`, `powershell.exe -NoProfile -ExecutionPolicy Bypass -File "C:\My Scripts\job.ps1" -Mode check`, true},
		{`C:\My Scripts\job.ps1`, `powershell.exe -Command echo -File "C:\My Scripts\job.ps1"`, false},
		{`C:\My Scripts\job.ps1`, `powershell.exe -File "C:\My Scripts\other.ps1" "C:\My Scripts\job.ps1"`, false},
		{`C:\My Scripts\job.py`, `python.exe "c:\my scripts\JOB.py" --quiet`, true},
		{`C:\My Scripts\job.py`, `python.exe "C:\My Scripts\other.py" "C:\My Scripts\job.py"`, false},
		{`C:\My Scripts\job.pyw`, `pythonw.exe "C:\My Scripts\job.pyw"`, true},
		{`C:\My Scripts\job.py`, `python.exe -c "C:\My Scripts\job.py"`, false},
		{`C:\My Scripts\job.py`, ``, false},
	} {
		if got := scriptCommandMatches(tc.command, tc.script); got != tc.want {
			t.Errorf("scriptCommandMatches(%q, %q) = %t, want %t", tc.command, tc.script, got, tc.want)
		}
	}
	if hasNativeConsole(`C:\Scripts\job.pyw`) {
		t.Fatal("pythonw must not be given an artificial console")
	}
}

func TestProgramWindowSelectionRejectsOtherProcessesAndAuxiliaryWindows(t *testing.T) {
	windows := []ManagedWindowInfo{
		{Handle: 1, ProcessID: 2, Title: "same program name", ClassName: "ConsoleWindowClass", IsForeground: true},
		{Handle: 2, ProcessID: 1, Title: "tooltip", IsToolWindow: true},
		{Handle: 3, ProcessID: 1, Title: "", ClassName: "PseudoConsoleWindow"},
		{Handle: 4, ProcessID: 1, Title: "utility", OwnerHandle: 20},
		{Handle: 5, ProcessID: 1, Title: "original console", ClassName: "ConsoleWindowClass"},
	}
	got := selectProgramWindow(windows, map[uint32]bool{1: true})
	if got.Handle != 5 || got.ProcessID != 1 {
		t.Fatalf("selected non-native/unrelated window: %+v", got)
	}
	if got := selectProgramWindow(windows[:3], map[uint32]bool{1: true}); got.Handle != 0 {
		t.Fatalf("selected auxiliary window: %+v", got)
	}
}

func TestProgramWindowActionDoesNotLaunchOrAllocateReplacementConsole(t *testing.T) {
	entry := startupTestEntry(t)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	for _, visible := range []bool{true, false} {
		if _, err := svc.SetProgramWindowVisible(context.Background(), entry, visible); !errors.Is(err, ErrProgramNotRunning) {
			t.Fatalf("not-running action error: %v", err)
		}
		if len(startupTestPIDs(entry.ExePath)) != 0 {
			t.Fatal("window action launched the program")
		}
	}
	// Simulate a legacy no-window launch; allocating a new console would not
	// recover its discarded output and could change the program's behavior.
	cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if _, err := svc.SetProgramWindowVisible(context.Background(), entry, true); !errors.Is(err, ErrProgramNoWindow) {
		t.Fatalf("legacy no-window action: %v", err)
	}
	if hwnd := FindConsoleWindow(uint32(cmd.Process.Pid)); hwnd != 0 {
		t.Fatalf("allocated a replacement console: 0x%X", hwnd)
	}
	if len(startupTestPIDs(entry.ExePath)) != 1 {
		t.Fatal("window action duplicated a legacy process")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.SetProgramWindowVisible(ctx, entry, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled action: %v", err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if _, err := svc.SetProgramWindowVisible(context.Background(), entry, true); !errors.Is(err, ErrProgramNotRunning) {
		t.Fatalf("exited program retained window action: %v", err)
	}
}

func TestStopNowTerminatesRunningProgramWithoutWindow(t *testing.T) {
	entry := startupTestEntry(t)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if !svc.IsProgramRunning(entry) {
		t.Fatal("running program was not detected")
	}
	if err := svc.StopNow(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(startupTestPIDs(entry.ExePath)) != 0 {
		t.Fatal("stop action left the no-window program running")
	}
}

func TestNativeConsoleArgumentsSurviveWindowsQuoting(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []launchMode{launchHiddenConsole, launchVisible} {
		output := filepath.Join(t.TempDir(), "arguments.json")
		want := []string{"", "two words", `C:\My Path\`, `say "hello"`, "中文"}
		cmd, err := startProcess(self, runnerHelperArgs("args", output, want...), mode)
		if err != nil {
			t.Fatal(err)
		}
		waitRunnerCommand(t, cmd)
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("native arguments = %q, want %q", got, want)
		}
	}
}
