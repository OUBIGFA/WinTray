//go:build !windows

package orchestrator

func hasRunningProcessByIdentity(expectedPath, expectedName string) bool {
	return false
}
