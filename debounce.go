package petra

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	defaultReloadDebounce = 75 * time.Millisecond
	defaultReloadMaxWait  = 250 * time.Millisecond
)

type eventDebouncer struct {
	mu      sync.Mutex
	delay   time.Duration
	maxWait time.Duration
	emit    func([]fsnotify.Event)

	events   []fsnotify.Event
	timer    *time.Timer
	maxTimer *time.Timer
	closed   bool
}

func newEventDebouncer(delay, maxWait time.Duration, emit func([]fsnotify.Event)) *eventDebouncer {
	delay, maxWait = normalizeReloadTimings(delay, maxWait)
	return &eventDebouncer{
		delay:   delay,
		maxWait: maxWait,
		emit:    emit,
	}
}

func normalizeReloadTimings(delay, maxWait time.Duration) (time.Duration, time.Duration) {
	if delay <= 0 {
		delay = defaultReloadDebounce
	}
	if maxWait <= 0 {
		maxWait = defaultReloadMaxWait
	}
	if maxWait < delay {
		maxWait = delay
	}
	return delay, maxWait
}

func (d *eventDebouncer) Add(event fsnotify.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}

	d.events = append(d.events, event)

	if d.maxTimer == nil {
		d.maxTimer = time.AfterFunc(d.maxWait, d.Flush)
	}

	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, d.Flush)
}

func (d *eventDebouncer) Flush() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}

	events := append([]fsnotify.Event{}, d.events...)
	d.events = nil

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.maxTimer != nil {
		d.maxTimer.Stop()
		d.maxTimer = nil
	}
	emit := d.emit
	d.mu.Unlock()

	if len(events) == 0 || emit == nil {
		return
	}
	emit(events)
}

func (d *eventDebouncer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true
	d.events = nil
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.maxTimer != nil {
		d.maxTimer.Stop()
		d.maxTimer = nil
	}
}
