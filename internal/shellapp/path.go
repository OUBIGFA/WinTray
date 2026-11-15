package shellapp

import (
	"image"
	"strings"
)

const PathPrefix = "shell:AppsFolder\\"

// A shell path keeps package upgrades from invalidating a managed app. The
// family and application IDs distinguish apps even when they share one EXE.
func ID(path string) string {
	if len(path) <= len(PathPrefix) || !strings.EqualFold(path[:len(PathPrefix)], PathPrefix) {
		return ""
	}
	identifier := path[len(PathPrefix):]
	family, application, found := strings.Cut(identifier, "!")
	if !found || family == "" || application == "" || strings.ContainsAny(identifier, "\\/:\x00\r\n\t \"\x27") || strings.Contains(application, "!") {
		return ""
	}
	return identifier
}

func IsPath(path string) bool { return ID(path) != "" }

type Application struct {
	Name             string
	Path             string
	Args             string
	WorkingDirectory string
	Icon             image.Image
}

func (app Application) Key() string {
	return strings.ToLower(app.Path) + "\x00" + app.Args + "\x00" + strings.ToLower(app.WorkingDirectory)
}
