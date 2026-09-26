package app

import "sync"

// latestOnly runs apply off the caller's goroutine, one call at a time. A
// request made while one is running replaces any request still waiting, so
// the newest value is always applied last and stale ones are skipped.
type latestOnly[T any] struct {
	apply func(T)

	mu      sync.Mutex
	pending *T
	running bool
	done    sync.WaitGroup
}

func newLatestOnly[T any](apply func(T)) *latestOnly[T] {
	return &latestOnly[T]{apply: apply}
}

func (l *latestOnly[T]) Request(v T) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = &v
	if l.running {
		return
	}
	l.running = true
	l.done.Add(1)
	go l.drain()
}

func (l *latestOnly[T]) drain() {
	defer l.done.Done()
	for {
		l.mu.Lock()
		next := l.pending
		l.pending = nil
		if next == nil {
			l.running = false
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		l.apply(*next)
	}
}

// Wait blocks until every request made so far has been handled.
func (l *latestOnly[T]) Wait() {
	l.done.Wait()
}
