//go:build windows

package startup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// LogonTask starts WinTray through a per-user Task Scheduler logon trigger.
// Explorer launches Run entries one by one, seconds apart, in the order the
// values were created, so a Run entry re-created after other programs lands
// behind the programs WinTray is meant to handle. A logon task starts as soon
// as the user signs in, before that queue, and needs no administrator rights
// because the trigger and principal are limited to the current user.
type LogonTask struct {
	name    string
	userSID string
	run     func(args ...string) ([]byte, error)
}

func NewLogonTask() (*LogonTask, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current user: %w", err)
	}
	sid := user.User.Sid.String()
	// Task names are machine wide; the SID keeps each user's task separate.
	return &LogonTask{name: "WinTray-" + sid, userSID: sid, run: runSchtasks}, nil
}

// Sync registers the task for exePath/args unless an enabled task with the
// same definition is already in place.
func (t *LogonTask) Sync(exePath, args string) error {
	definition, fingerprint := logonTaskXML(t.userSID, exePath, args)
	if out, err := t.run("/Query", "/TN", t.name, "/XML"); err == nil && taskUpToDate(out, fingerprint) {
		return nil
	}
	file, err := os.CreateTemp("", "wintray-task-*.xml")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	_, writeErr := file.Write(utf16LEWithBOM(definition))
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return writeErr
	}
	_, err = t.run("/Create", "/TN", t.name, "/XML", path, "/F")
	return err
}

// Remove deletes the task. A task that does not exist counts as removed.
func (t *LogonTask) Remove() error {
	_, err := t.run("/Delete", "/TN", t.name, "/F")
	if err == nil {
		return nil
	}
	if _, queryErr := t.run("/Query", "/TN", t.name); queryErr != nil {
		return nil
	}
	return err
}

// logonTaskXML returns the task definition and the fingerprint embedded in
// its description. The fingerprint is ASCII so it survives whatever encoding
// schtasks uses when exporting the registered task.
func logonTaskXML(userSID, exePath, args string) (string, string) {
	body := fmt.Sprintf(`  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%[1]s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%[1]s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>false</StartWhenAvailable>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>4</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%[2]s</Command>
      <Arguments>%[3]s</Arguments>
      <WorkingDirectory>%[4]s</WorkingDirectory>
    </Exec>
  </Actions>
`, xmlText(userSID), xmlText(exePath), xmlText(args), xmlText(filepath.Dir(exePath)))
	sum := sha256.Sum256([]byte(body))
	fingerprint := "wintray-task-" + hex.EncodeToString(sum[:8])
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>WinTray</Author>
    <Description>Starts WinTray at logon. ` + fingerprint + `</Description>
  </RegistrationInfo>
` + body + `</Task>
`, fingerprint
}

// taskUpToDate reports whether an exported task carries fingerprint and has
// neither the task nor its trigger disabled.
func taskUpToDate(exported []byte, fingerprint string) bool {
	contains := func(s string) bool {
		return bytes.Contains(exported, []byte(s)) || bytes.Contains(exported, utf16LE(s))
	}
	return contains(fingerprint) && !contains("<Enabled>false</Enabled>")
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func utf16LEWithBOM(s string) []byte {
	return append([]byte{0xFF, 0xFE}, utf16LE(s)...)
}

func runSchtasks(args ...string) ([]byte, error) {
	cmd := exec.Command(filepath.Join(systemDir(), "schtasks.exe"), args...)
	// WinTray is a GUI program; without this every call flashes a console.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(oemText(out))
		if msg == "" {
			return out, fmt.Errorf("schtasks %s: %w", args[0], err)
		}
		return out, fmt.Errorf("schtasks %s: %w: %s", args[0], err, msg)
	}
	return out, nil
}

func systemDir() string {
	if dir, err := windows.GetSystemDirectory(); err == nil && dir != "" {
		return dir
	}
	return `C:\Windows\System32`
}

// cpOEM selects the console (OEM) code page for MultiByteToWideChar.
const cpOEM = 1

// oemText decodes console output, which schtasks writes in the OEM code page.
func oemText(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	n, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return string(b)
	}
	buf := make([]uint16, n)
	if _, err = windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), &buf[0], n); err != nil {
		return string(b)
	}
	return string(utf16.Decode(buf))
}
