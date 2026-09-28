//go:build windows

package startup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// approvalValue distinguishes a missing value from an empty/invalid value.
// Restoring 'absent' must delete our override, not manufacture an enabled one.
type approvalValue struct {
	Exists bool   `json:"exists"`
	Data   []byte `json:"data,omitempty"`
}

type startupEntry struct {
	label        string
	path         string
	args         string
	workingDir   string
	shortcut     string
	show         int32
	highest      bool
	machine      bool
	enabled      bool
	approvalPath string
	approvalName string
	approval     approvalValue // current-user approval only; never write HKLM
}

func (entry startupEntry) sameLaunch(other startupEntry) bool {
	return sameExecutablePath(entry.path, other.path) && entry.args == other.args &&
		strings.EqualFold(entry.workingDir, other.workingDir) && entry.show == other.show &&
		entry.highest == other.highest && entry.shortcut == other.shortcut
}

func (entry startupEntry) approvalID() string {
	return strings.ToLower(entry.approvalPath + `\` + entry.approvalName)
}

func standardRunSources() []runSource {
	return []runSource{
		{registry.CURRENT_USER, runKeyPath, registry.WOW64_64KEY, startupApprovedPath + `\Run`, `HKCU\Run`},
		{registry.LOCAL_MACHINE, runKeyPath, registry.WOW64_64KEY, startupApprovedPath + `\Run`, `HKLM\Run`},
		{registry.LOCAL_MACHINE, runKeyPath, registry.WOW64_32KEY, startupApprovedPath + `\Run32`, `HKLM\Run32`},
	}
}

// errNotStartupTarget marks a startup command that launches some other
// program; callers skip it silently.
var errNotStartupTarget = errors.New("startup command launches another executable")

// StartupCommandError reports an original startup command that mentions the
// program but cannot be attributed to it exactly. Taking it over could start
// a different executable or drop a wrapper's own behaviour, so it fails
// closed. Unquoted is set when only missing quotes make it ambiguous: quoting
// the executable path resolves it.
type StartupCommandError struct {
	Source   string
	Unquoted bool
}

func (e *StartupCommandError) Error() string {
	if e.Unquoted {
		return e.Source + ": the original startup command is unquoted and its path contains spaces, so WinTray cannot identify it safely; quote the executable path of that startup entry"
	}
	return e.Source + ": the original startup command starts the program through another program, so WinTray cannot take it over safely; use another launch mode for this program"
}

// ambiguousStartupCommand is the source-less form returned by
// splitStartupCommandFor; callers attach the entry they read it from.
type ambiguousStartupCommand struct{ unquoted bool }

func (e ambiguousStartupCommand) Error() string {
	return (&StartupCommandError{Source: "startup entry", Unquoted: e.unquoted}).Error()
}

func (e ambiguousStartupCommand) at(source string) error {
	return &StartupCommandError{Source: source, Unquoted: e.unquoted}
}

// splitStartupCommandFor separates expected from the untouched argument tail
// of command; re-joining argv would change quoting for programs with their own
// parser. An unquoted path is resolved the way Windows resolves it: each
// prefix ending before a space or tab is tried in turn, and the first one that
// names an existing file is what runs. It returns errNotStartupTarget when
// command launches another program, and an ambiguousStartupCommand when it
// mentions expected but WinTray cannot prove that expected is what runs.
func splitStartupCommandFor(command, expected string) (path, args string, err error) {
	command = strings.TrimLeft(command, " \t")
	path, args, err = locateStartupExecutable(command, expected)
	if errors.Is(err, errNotStartupTarget) && strings.Contains(strings.ToLower(command), strings.ToLower(strings.Trim(strings.TrimSpace(expected), `"`))) {
		// A wrapper (cmd /c, rundll32, a launcher) or an unrelated program
		// taking expected as an argument must not become a bare launch.
		return "", "", ambiguousStartupCommand{}
	}
	return path, args, err
}

func locateStartupExecutable(command, expected string) (path, args string, err error) {
	if command == "" || strings.ContainsRune(command, 0) {
		return "", "", errNotStartupTarget
	}
	if command[0] == '"' {
		close := strings.IndexByte(command[1:], '"')
		if close < 0 {
			return "", "", errNotStartupTarget
		}
		end := close + 2
		if end < len(command) && command[end] != ' ' && command[end] != '\t' {
			return "", "", errNotStartupTarget
		}
		path = command[1 : end-1]
		if !sameExecutablePath(path, expected) {
			return "", "", errNotStartupTarget
		}
		return path, strings.TrimLeft(command[end:], " \t"), nil
	}
	blocked := false
	for end := 0; end <= len(command); end++ {
		if end < len(command) && command[end] != ' ' && command[end] != '\t' {
			continue
		}
		candidate := command[:end]
		if candidate == "" || strings.HasSuffix(candidate, " ") || strings.HasSuffix(candidate, "\t") {
			continue // a run of separators yields no new candidate
		}
		if !filepath.IsAbs(candidate) {
			// Windows searches its path for a relative name; whatever it
			// finds is a wrapper at best, never provably expected.
			return "", "", errNotStartupTarget
		}
		if sameExecutablePath(candidate, expected) {
			if blocked {
				return "", "", ambiguousStartupCommand{unquoted: true}
			}
			return candidate, strings.TrimLeft(command[end:], " \t"), nil
		}
		if !blocked && launchableFile(candidate) {
			// Windows would start this shorter file instead. Keep looking
			// only to tell a missing-quotes problem from another program.
			blocked = true
		}
	}
	return "", "", errNotStartupTarget
}

// launchableFile reports whether Windows could start candidate as the
// executable of an unquoted command: the name itself, or, without an
// extension, the name with one of the extensions it tries.
func launchableFile(candidate string) bool {
	names := []string{candidate}
	if filepath.Ext(candidate) == "" {
		for _, ext := range []string{".exe", ".com", ".bat", ".cmd"} {
			names = append(names, candidate+ext)
		}
	}
	for _, name := range names {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func readApproval(root registry.Key, path, name string) (approvalValue, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return approvalValue{}, nil
	}
	if err != nil {
		return approvalValue{}, err
	}
	defer key.Close()
	data, _, err := key.GetBinaryValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return approvalValue{}, nil
	}
	if err != nil {
		return approvalValue{}, err
	}
	return approvalValue{Exists: true, Data: data}, nil
}

func approvalEnabled(value approvalValue) (bool, error) {
	if !value.Exists {
		return true, nil
	}
	if len(value.Data) >= 12 {
		switch value.Data[0] {
		case 2, 6:
			return true, nil
		case 3, 7:
			return false, nil
		}
	}
	return false, fmt.Errorf("unknown StartupApproved state %x", value.Data)
}

func sameApproval(a, b approvalValue) bool {
	return a.Exists == b.Exists && bytes.Equal(a.Data, b.Data)
}

func (entry *startupEntry) readApproval() error {
	var err error
	entry.approval, err = readApproval(registry.CURRENT_USER, entry.approvalPath, entry.approvalName)
	if err != nil {
		return err
	}
	entry.enabled, err = approvalEnabled(entry.approval)
	if err != nil || !entry.machine {
		return err
	}
	machine, err := readApproval(registry.LOCAL_MACHINE, entry.approvalPath, entry.approvalName)
	if err != nil {
		return err
	}
	enabled, err := approvalEnabled(machine)
	entry.enabled = entry.enabled && enabled
	return err
}

func findRunStartupEntries(exePath string, sources []runSource) ([]startupEntry, error) {
	var entries []startupEntry
	for _, source := range sources {
		key, err := registry.OpenKey(source.root, source.path, registry.QUERY_VALUE|source.view)
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source.label, err)
		}
		found, err := readRunStartupEntries(key, source, exePath)
		key.Close()
		if err != nil {
			return nil, err
		}
		entries = append(entries, found...)
	}
	return entries, nil
}

func readRunStartupEntries(key registry.Key, source runSource, exePath string) ([]startupEntry, error) {
	names, err := key.ReadValueNames(0)
	if err != nil {
		return nil, err
	}
	var entries []startupEntry
	for _, name := range names {
		command, kind, err := key.GetStringValue(name)
		if errors.Is(err, registry.ErrNotExist) || errors.Is(err, registry.ErrUnexpectedType) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if kind == registry.EXPAND_SZ {
			command, err = registry.ExpandString(command)
			if err != nil {
				return nil, err
			}
		}
		path, args, parseErr := splitStartupCommandFor(command, exePath)
		if errors.Is(parseErr, errNotStartupTarget) {
			continue
		}
		var ambiguous ambiguousStartupCommand
		if errors.As(parseErr, &ambiguous) {
			return nil, ambiguous.at(source.label + `\` + name)
		}
		if parseErr != nil {
			return nil, parseErr
		}
		entry := startupEntry{
			label: source.label + `\` + name, path: path, args: args, show: 1,
			// A Run value has no working-directory field. Preserve the
			// inherited launch context rather than inventing a directory.
			machine:      source.root == registry.LOCAL_MACHINE,
			approvalPath: source.approvalPath, approvalName: name,
		}
		if err := entry.readApproval(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.label, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func findFolderStartupEntries(exePath, folder string, machine bool) ([]startupEntry, error) {
	files, err := os.ReadDir(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []startupEntry
	for _, file := range files {
		if file.IsDir() || !strings.EqualFold(filepath.Ext(file.Name()), ".lnk") {
			continue
		}
		path := filepath.Join(folder, file.Name())
		link, err := readStartupShortcut(path)
		if err != nil {
			return nil, fmt.Errorf("read startup shortcut %s: %w", path, err)
		}
		target, err := expandedPath(link.path)
		if err != nil {
			return nil, err
		}
		if !sameExecutablePath(target, exePath) {
			continue
		}
		entry := startupEntry{
			label: path, path: target, args: link.arguments, workingDir: link.directory,
			shortcut: path, show: link.show, highest: link.runAsAdmin, machine: machine,
			approvalPath: startupApprovedPath + `\StartupFolder`, approvalName: file.Name(),
		}
		if err := entry.readApproval(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.label, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func findStartupEntries(exePath string) ([]startupEntry, error) {
	entries, err := findRunStartupEntries(exePath, standardRunSources())
	if err != nil {
		return nil, err
	}
	for _, folder := range []struct {
		id      *windows.KNOWNFOLDERID
		machine bool
	}{{windows.FOLDERID_Startup, false}, {windows.FOLDERID_CommonStartup, true}} {
		path, err := windows.KnownFolderPath(folder.id, 0)
		if err != nil {
			return nil, fmt.Errorf("locate startup folder: %w", err)
		}
		found, err := findFolderStartupEntries(exePath, path, folder.machine)
		if err != nil {
			return nil, err
		}
		entries = append(entries, found...)
	}
	return entries, nil
}
