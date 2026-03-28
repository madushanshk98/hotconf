package hotconf

import (
	"sync"
	"time"
)

// debouncer delays execution of fn until no more calls arrive within interval.
// Safe for concurrent use.
type debouncer struct {
	interval time.Duration
	fn       func()

	mu    sync.Mutex
	timer *time.Timer
}

func newDebouncer(interval time.Duration, fn func()) *debouncer {
	return &debouncer{
		interval: interval,
		fn:       fn,
	}
}

// trigger resets the timer. The wrapped fn will fire only after interval
// elapses without another call to trigger.
func (d *debouncer) trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.interval, d.fn)
}

// stop cancels any pending invocation.
func (d *debouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}
