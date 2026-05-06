package petra

import (
	"context"
	"fmt"
	"sync"
)

type watcherReadiness struct {
	total int
	done  chan struct{}

	mu     sync.Mutex
	ready  int
	err    error
	closed bool
}

func newWatcherReadiness(total int) *watcherReadiness {
	r := &watcherReadiness{
		total: total,
		done:  make(chan struct{}),
	}
	if total == 0 {
		close(r.done)
		r.closed = true
	}
	return r
}

func (r *watcherReadiness) markReady() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.ready++
	if r.ready >= r.total {
		r.closeLocked()
	}
}

func (r *watcherReadiness) markFailed(kind, folder string, err error) {
	if r == nil || err == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.err = fmt.Errorf("%s watcher %q: %w", kind, folder, err)
	r.closeLocked()
}

func (r *watcherReadiness) wait(ctx context.Context) error {
	if r == nil {
		return nil
	}

	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *watcherReadiness) closeLocked() {
	close(r.done)
	r.closed = true
}
