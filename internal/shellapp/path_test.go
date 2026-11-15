package shellapp

import "testing"

func TestApplicationIDDoesNotAcceptFilePathsOrMalformedShellPaths(t *testing.T) {
	for _, path := range []string{"", "C:/Apps/Reader.exe", "shell:AppsFolder", "shell:AppsFolder\\Reader", "shell:AppsFolder\\!Reader", "shell:AppsFolder\\Family!", "shell:AppsFolder\\Family!Reader!Other", "shell:AppsFolder\\Family!Reader\\other", "shell:AppsFolder\\Family!Read er"} {
		if IsPath(path) {
			t.Fatalf("accepted an invalid application ID: %q", path)
		}
	}
	if got := ID("SHELL:APPSFOLDER\\Example.App_123456789abcd!Reader"); got != "Example.App_123456789abcd!Reader" {
		t.Fatalf("application identity changed: %q", got)
	}
}

func TestApplicationKeyKeepsDifferentLaunchArguments(t *testing.T) {
	first := Application{Path: "C:/Apps/Editor.exe", Args: "--profile one", WorkingDirectory: "C:/Data"}
	second := Application{Path: "c:/apps/editor.EXE", Args: "--profile one", WorkingDirectory: "c:/data"}
	if first.Key() != second.Key() {
		t.Fatal("path case created a duplicate launch entry")
	}
	second.Args = "--profile two"
	if first.Key() == second.Key() {
		t.Fatal("different launch arguments were merged")
	}
}
