package stringutil

import "testing"

func TestTrimExt(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"notepad.exe", "notepad"},
		{"README", "README"},
		{"archive.tar.gz", "archive.tar"},
		{".hidden", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := TrimExt(tc.input)
		if got != tc.want {
			t.Errorf("TrimExt(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
