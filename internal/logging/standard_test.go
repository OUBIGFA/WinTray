package logging

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandardLogIsCapturedAndRestored(t *testing.T) {
	output, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	t.Cleanup(func() {
		log.SetOutput(output)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	var previous bytes.Buffer
	log.SetOutput(&previous)
	log.SetFlags(log.Lshortfile)
	log.SetPrefix("previous: ")
	logger, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	restore := logger.CaptureStandardLog()
	defer restore()
	log.Print("mygo: renderer diagnostic")
	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[DIAG] mygo: renderer diagnostic\n") || previous.Len() != 0 {
		t.Fatalf("library diagnostic was not routed to the app log: %q; stderr=%q", data, previous.String())
	}
	restore()
	if log.Writer() != &previous || log.Flags() != log.Lshortfile || log.Prefix() != "previous: " {
		t.Fatal("standard logger configuration was not restored")
	}
	log.Print("after session")
	if !strings.Contains(previous.String(), "after session") {
		t.Fatal("restored output did not receive later diagnostics")
	}
}

func TestStandardLogWriterReportsFailures(t *testing.T) {
	for _, closed := range []bool{false, true} {
		dir := t.TempDir()
		logger, err := New(dir)
		if err != nil {
			t.Fatal(err)
		}
		if closed {
			if err := logger.Close(); err != nil {
				t.Fatal(err)
			}
		} else {
			// Replace the destination with a directory to force a write error.
			if err := os.Remove(logger.path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(dir, "wintray.log"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if n, err := logger.Write([]byte("diagnostic\n")); n != 0 || err == nil || closed && !errors.Is(err, os.ErrClosed) {
			t.Fatalf("closed=%t: Write = %d, %v", closed, n, err)
		}
		_ = logger.Close()
	}
}
