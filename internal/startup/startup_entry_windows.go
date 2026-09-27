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

// splitStartupCommand only separates the executable from the untouched argument
// tail. Re-joining argv would change quoting for programs with their own parser.
func splitStartupCommand(command string) (path, args string, err error) {
	command = strings.TrimLeft(command, " \t")
	if command == "" || strings.ContainsRune(command, 0) {
		return "", "", errors.New("empty or invalid startup command")
	}
	end := 0
	if command[0] == '"' {
		close := strings.IndexByte(command[1:], '"')
		if close < 0 {
			return "", "", errors.New("unclosed executable quote in startup command")
		}
		end = close + 2
		path = command[1 : end-1]
		if end < len(command) && command[end] != ' ' && command[end] != '\t' {
			return "", "", errors.New("missing separator after startup executable")
		}
	} else {
		end = strings.IndexAny(command, " \t")
		if end < 0 {
			end = len(command)
		}
		path = command[:end]
	}
	if !filepath.IsAbs(path) {
		return "", "", errors.New("startup executable is not an absolute path")
	}
	return path, strings.TrimLeft(command[end:], " \t"), nil
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
		path, args, parseErr := splitStartupCommand(command)
		if parseErr != nil || !sameExecutablePath(path, exePath) {
			// A target mentioned by a wrapper/unquoted ambiguous command is
			// not safe to replace with a direct exe launch.
			if strings.Contains(strings.ToLower(command), strings.ToLower(exePath)) {
				return nil, fmt.Errorf("%s\\%s uses an indirect or ambiguous startup command; select its original launcher instead", source.label, name)
			}
			continue
		}
		entry := startupEntry{
			label: source.label + `\` + name, path: path, args: args, show: 1,
			// A Run value has no working-directory field. Do not silently
			// replace it with the executable's directory as the old task did.
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
