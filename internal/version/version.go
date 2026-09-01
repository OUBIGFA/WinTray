// Package version exposes the application version and the upstream project
// location. Number is overridable at build time via
// -ldflags "-X wintray/internal/version.Number=1.2.3".
package version

// Number is the released version of this build.
var Number = "1.1.0"

const (
	// RepositoryURL is the upstream project page, also used as the download
	// destination when a newer release is available.
	RepositoryURL = "https://github.com/OUBIGFA/WinTray"

	// LatestReleaseAPI returns the newest published release of the project.
	LatestReleaseAPI = "https://api.github.com/repos/OUBIGFA/WinTray/releases/latest"
)
