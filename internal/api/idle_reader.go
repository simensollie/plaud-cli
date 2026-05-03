package api

import (
	"errors"
	"io"
	"sync"
	"time"
)

// ErrIdleTimeout is returned by an idleTimeoutReader when no bytes have been
// read within the configured idle window. Callers should treat it as a
// terminal failure for the current download attempt; F-15 handles retry
// policy at a higher layer.
var ErrIdleTimeout = errors.New("audio download stalled: no bytes received within idle window")

// idleTimeoutReader wraps an io.ReadCloser and aborts a Read if no progress
// is made for the configured idle window. Each successful Read resets the
// deadline. It does NOT enforce a total time budget; only progress-or-die.
type idleTimeoutReader struct {
	inner   io.ReadCloser
	timeout time.Duration

	mu       sync.Mutex
	timer    *time.Timer
	timedOut bool
	closed   bool
}

// newIdleTimeoutReader wraps rc with an idle timeout. The returned reader
// owns rc; callers must Close the wrapper, not rc directly.
func newIdleTimeoutReader(rc io.ReadCloser, timeout time.Duration) *idleTimeoutReader {
	r := &idleTimeoutReader{
		inner:   rc,
		timeout: timeout,
	}
	r.timer = time.AfterFunc(timeout, r.fire)
	return r
}

// fire is invoked by the timer when the idle window elapses. It closes the
// underlying reader so any in-flight Read on a wedged TCP socket returns
// promptly rather than waiting on the OS-level connection timeout.
func (r *idleTimeoutReader) fire() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.timedOut = true
	r.mu.Unlock()
	// Close the inner reader from the timer goroutine to actually unstick a
	// wedged Read on a network socket.
	_ = r.inner.Close()
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)

	r.mu.Lock()
	timedOut := r.timedOut
	if !timedOut && n > 0 {
		// Reset the idle window on any progress. Stop returns false if the
		// timer has already fired or been stopped; either is fine since
		// timedOut would be set in the fired case.
		if r.timer != nil {
			r.timer.Stop()
			r.timer.Reset(r.timeout)
		}
	}
	r.mu.Unlock()

	if timedOut {
		return n, ErrIdleTimeout
	}
	return n, err
}

func (r *idleTimeoutReader) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	if r.timer != nil {
		r.timer.Stop()
	}
	r.mu.Unlock()
	return r.inner.Close()
}
