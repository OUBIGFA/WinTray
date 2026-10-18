package ui

import (
	"errors"
	"testing"
)

func TestPreferCPURenderer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build uint32
		want  bool
	}{
		{"unknown build", 0, false},
		{"windows 10 1809", 17763, false},
		{"windows 10 22h2", 19045, false},
		{"last windows 10 build", 21999, false},
		{"windows 11 21h2", 22000, true},
		{"windows 11 23h2", 22631, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := preferCPURenderer(tc.build); got != tc.want {
				t.Fatalf("preferCPURenderer(%d) = %v, want %v", tc.build, got, tc.want)
			}
		})
	}
}

func TestConfigureRenderer(t *testing.T) {
	tests := []struct {
		name       string
		build      uint32
		env        map[string]string
		wantValue  string
		wantForced string
	}{
		{name: "windows 10 leaves the choice to mygo", build: 19045, wantValue: ""},
		{name: "windows 11 forces the cpu renderer", build: 22631, wantValue: "0", wantForced: "0"},
		{name: "explicit value wins on windows 10", build: 19045, env: map[string]string{"MYGO_GPU": "1"}, wantValue: "1"},
		{name: "explicit cpu value wins on windows 11", build: 22631, env: map[string]string{"MYGO_GPU": "0"}, wantValue: "0"},
		{name: "empty override still counts as set", build: 22631, env: map[string]string{"MYGO_GPU": ""}, wantValue: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				if key != rendererEnvVar {
					t.Fatalf("looked up %q, want %q", key, rendererEnvVar)
				}
				value, ok := tc.env[key]
				return value, ok
			}
			var forced *string
			setenv := func(key, value string) error {
				if key != rendererEnvVar {
					t.Fatalf("set %q, want %q", key, rendererEnvVar)
				}
				forced = &value
				return nil
			}
			got, err := configureRenderer(tc.build, lookup, setenv)
			if err != nil {
				t.Fatalf("configureRenderer: %v", err)
			}
			if got != tc.wantValue {
				t.Fatalf("value = %q, want %q", got, tc.wantValue)
			}
			switch {
			case tc.wantForced == "" && forced != nil:
				t.Fatalf("forced MYGO_GPU to %q, want no write", *forced)
			case tc.wantForced != "" && forced == nil:
				t.Fatalf("did not force MYGO_GPU, want %q", tc.wantForced)
			case tc.wantForced != "" && *forced != tc.wantForced:
				t.Fatalf("forced MYGO_GPU to %q, want %q", *forced, tc.wantForced)
			}
		})
	}
}

func TestConfigureRendererSetenvFailure(t *testing.T) {
	want := errors.New("no environment")
	_, err := configureRenderer(22631, func(string) (string, bool) { return "", false }, func(string, string) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("configureRenderer error = %v, want %v", err, want)
	}
}
