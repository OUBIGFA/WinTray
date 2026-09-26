package app

import (
	"sync"
	"testing"
)

func TestLatestOnlyAppliesNewestRequestLast(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var mu sync.Mutex
	var applied []int
	l := newLatestOnly(func(v int) {
		if v == 1 {
			close(started)
			<-release
		}
		mu.Lock()
		applied = append(applied, v)
		mu.Unlock()
	})

	l.Request(1)
	<-started
	// Both arrive while 1 is still being applied; only the newest survives.
	l.Request(2)
	l.Request(3)
	close(release)
	l.Wait()

	if len(applied) != 2 || applied[0] != 1 || applied[1] != 3 {
		t.Fatalf("applied = %v, want [1 3]", applied)
	}

	l.Request(4)
	l.Wait()
	if applied[len(applied)-1] != 4 {
		t.Fatalf("request after idle not applied: %v", applied)
	}
}
