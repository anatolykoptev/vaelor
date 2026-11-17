package filewatcher

import (
	"sync"
	"sync/atomic"
	"time"
)

const defaultDebounceDelay = 500 * time.Millisecond // Default delay for debouncing when none is specified

// debounceEntry holds a timer and its associated function for per-key debouncing.
type debounceEntry struct {
	fn    func()
	timer *time.Timer
}

// baseDebouncer contains shared state and logic for debouncer implementations.
// It provides common lifecycle management (stop, waitgroup tracking) that both
// Debouncer and GlobalDebouncer need.
type baseDebouncer struct {
	delay   time.Duration
	mu      sync.Mutex
	stopped atomic.Bool
	wg      sync.WaitGroup // tracks in-flight callbacks
}

// newBaseDebouncer creates a baseDebouncer with validated delay.
func newBaseDebouncer(delay time.Duration) baseDebouncer {
	if delay <= 0 {
		delay = defaultDebounceDelay
	}

	return baseDebouncer{
		delay:   delay,
		mu:      sync.Mutex{},
		stopped: atomic.Bool{},
		wg:      sync.WaitGroup{},
	}
}

// isStopped returns whether the debouncer has been stopped.
func (b *baseDebouncer) isStopped() bool {
	return b.stopped.Load()
}

// markStopped atomically marks the debouncer as stopped.
func (b *baseDebouncer) markStopped() {
	b.stopped.Store(true)
}

// wait waits for all in-flight callbacks to complete.
func (b *baseDebouncer) wait() {
	b.wg.Wait()
}

// runIfActive locks the base mutex and invokes fn while holding the lock
// only if the debouncer is not stopped. If the debouncer is already stopped,
// fn is not invoked and the lock is released.
func (b *baseDebouncer) runIfActive(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isStopped() {
		return
	}

	fn()
}

// add increments the WaitGroup for a pending callback.
func (b *baseDebouncer) add() {
	b.wg.Add(1)
}

// done decrements the WaitGroup when a callback completes or is cancelled.
func (b *baseDebouncer) done() {
	b.wg.Done()
}

// stopTimer stops a timer and compensates the waitgroup.
// Use this when cancelling a pending timer callback.
func (b *baseDebouncer) stopTimer(timer *time.Timer) {
	if timer != nil {
		timer.Stop()
		b.done()
	}
}

// executeCallbacks runs all provided callbacks with proper waitgroup tracking.
func (b *baseDebouncer) executeCallbacks(callbacks []func()) {
	for _, fn := range callbacks {
		b.add()
		fn()
		b.done()
	}
}

// stop marks the debouncer stopped, runs cleanup under the mutex, then waits
// for all in-flight callbacks to drain. Centralizes the lock/markStopped/unlock/wait
// discipline shared by Debouncer.Stop and GlobalDebouncer.Stop.
func (b *baseDebouncer) stop(cleanup func()) {
	b.mu.Lock()
	b.markStopped()
	cleanup()
	b.mu.Unlock()

	b.wait()
}

// Debouncer prevents rapid successive function executions by coalescing
// calls within a delay window. It supports per-key debouncing so that
// different keys (e.g., file paths) are debounced independently.
type Debouncer struct {
	base    baseDebouncer
	entries map[DebounceKey]*debounceEntry
}

// NewDebouncer creates a new Debouncer with the specified delay.
func NewDebouncer(delay time.Duration) *Debouncer {
	return &Debouncer{
		base:    newBaseDebouncer(delay),
		entries: make(map[DebounceKey]*debounceEntry),
	}
}

// Debounce schedules fn to run after the delay, resetting any pending
// execution for the same key. This ensures callback runs only once for a burst
// of events sharing the same key.
func (d *Debouncer) Debounce(key DebounceKey, callback func()) {
	d.base.runIfActive(func() {
		if entry, exists := d.entries[key]; exists {
			d.base.stopTimer(entry.timer)
		}

		d.base.add()

		entry := &debounceEntry{
			fn:    callback,
			timer: nil,
		}
		entry.timer = time.AfterFunc(d.base.delay, func() {
			defer d.base.done()

			d.base.mu.Lock()
			delete(d.entries, key)
			stopped := d.base.isStopped()
			d.base.mu.Unlock()

			if stopped {
				return
			}

			callback()
		})
		d.entries[key] = entry
	})
}

// Flush executes all pending functions immediately and clears all timers.
func (d *Debouncer) Flush() {
	d.base.mu.Lock()

	callbacks := make([]func(), 0, len(d.entries))

	for key, entry := range d.entries {
		entry.timer.Stop()
		callbacks = append(callbacks, entry.fn)

		delete(d.entries, key)
		d.base.done()
	}

	d.base.mu.Unlock()
	d.base.executeCallbacks(callbacks)
}

// Stop cancels all pending executions without running them.
// Waits for any in-flight callbacks to complete before returning.
func (d *Debouncer) Stop() {
	d.base.stop(func() {
		for key, entry := range d.entries {
			entry.timer.Stop()
			delete(d.entries, key)
			// Each cancelled timer means we called wg.Add(1) but callback won't run
			// so we need to decrement
			d.base.done()
		}
	})
}

// Pending returns the number of keys with pending executions.
func (d *Debouncer) Pending() int {
	d.base.mu.Lock()
	defer d.base.mu.Unlock()

	return len(d.entries)
}

// GlobalDebouncer coalesces all events into a single timer, regardless of key.
// Useful when you want to batch all file changes into one action.
type GlobalDebouncer struct {
	base  baseDebouncer
	fn    func()
	timer *time.Timer
}

// NewGlobalDebouncer creates a new GlobalDebouncer with the specified delay.
func NewGlobalDebouncer(delay time.Duration) *GlobalDebouncer {
	return &GlobalDebouncer{
		base:  newBaseDebouncer(delay),
		fn:    nil,
		timer: nil,
	}
}

// Debounce resets the global timer. callback runs only once after the delay
// since the last call, regardless of how many times Debounce is called.
// The key parameter is intentionally ignored — GlobalDebouncer coalesces all
// events into a single timer regardless of their key.
//
// Note: only the last callback is executed. If multiple events have different
// callbacks, earlier ones are discarded. This is by design: GlobalDebouncer
// coalesces all events into a single action.
func (g *GlobalDebouncer) Debounce(_ DebounceKey, callback func()) {
	g.base.runIfActive(func() {
		if g.timer != nil {
			g.base.stopTimer(g.timer)
		}

		g.base.add()

		g.fn = callback
		g.timer = time.AfterFunc(g.base.delay, func() {
			defer g.base.done()

			g.base.mu.Lock()
			g.timer = nil
			g.fn = nil
			stopped := g.base.isStopped()
			g.base.mu.Unlock()

			if stopped {
				return
			}

			callback()
		})
	})
}

// Flush executes the pending function immediately and clears the timer.
func (g *GlobalDebouncer) Flush() {
	g.base.mu.Lock()

	var callback func()

	if g.timer != nil {
		oldTimer := g.timer
		g.timer = nil
		callback = g.fn
		g.fn = nil

		oldTimer.Stop() // compensate for cancelled timer
		g.base.done()
	}

	g.base.mu.Unlock()

	if callback != nil {
		g.base.executeCallbacks([]func(){callback})
	}
}

// Stop cancels the pending execution.
// Waits for any in-flight callback to complete before returning.
func (g *GlobalDebouncer) Stop() {
	g.base.stop(func() {
		if g.timer != nil {
			g.base.stopTimer(g.timer)
			g.timer = nil
			g.fn = nil
		}
	})
}

// Pending returns whether there is a pending execution.
func (g *GlobalDebouncer) Pending() int {
	g.base.mu.Lock()
	defer g.base.mu.Unlock()

	if g.timer != nil {
		return 1
	}

	return 0
}
