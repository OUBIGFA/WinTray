package stringutil

import "path/filepath"

// TrimExt removes the file extension from a filename.
// e.g. "notepad.exe" → "notepad", "README" → "README"
func TrimExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)]
}
