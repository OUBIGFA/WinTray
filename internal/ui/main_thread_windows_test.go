//go:build windows

package ui

import (
	"os"
	"testing"
)

var testMainThread = make(chan func())

func TestMain(m *testing.M) {
	done := make(chan int, 1)
	go func() { done <- m.Run() }()
	for {
		select {
		case f := <-testMainThread:
			f()
		case code := <-done:
			os.Exit(code)
		}
	}
}
func runTestMainThread(f func()) {
	done := make(chan struct{})
	testMainThread <- func() { defer close(done); f() }
	<-done
}
