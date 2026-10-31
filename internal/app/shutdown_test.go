package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExitDeadlineBoundsBlockedCleanup(t *testing.T) {
	for _, mode := range []string{"blocked", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), exitGracePeriod+3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExitDeadlineProcess$")
			cmd.Env = append(os.Environ(), "WINTRAY_EXIT_DEADLINE_TEST="+mode)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("exit deadline: %v\n%s", err, output)
			}
			if mode == "cancelled" && !strings.Contains(string(output), "normal cleanup finished") {
				t.Fatalf("stopped deadline still terminated the process: %s", output)
			}
		})
	}
}

func TestExitDeadlineProcess(t *testing.T) {
	mode := os.Getenv("WINTRAY_EXIT_DEADLINE_TEST")
	if mode == "" {
		return
	}
	var deadline exitDeadline
	defer deadline.stop()
	deadline.start()
	deadline.start() // repeated exit requests must not extend the deadline
	if mode == "cancelled" {
		deadline.stop()
		time.Sleep(exitGracePeriod + 100*time.Millisecond)
		fmt.Println("normal cleanup finished")
		return
	}
	// Simulate an outer registration wait or native cleanup that never ends.
	defer func() { <-make(chan struct{}) }()
}
