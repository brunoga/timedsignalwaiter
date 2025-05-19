package timedsignalwaiter

import (
	"context"
	"sync/atomic"
)

// TimedSignalWaiter is a synchronization primitive that allows one or more
// goroutines to wait for a broadcast signal from another goroutine.
// The wait operation can be canceled or timed out via a context.Context.
type TimedSignalWaiter struct {
	chP atomic.Pointer[chan struct{}]
}

// New creates a new TimedSignalWaiter.
func New() *TimedSignalWaiter {
	b := &TimedSignalWaiter{}

	ch := make(chan struct{})
	b.chP.Store(&ch)

	return b
}

// Wait blocks until a signal is broadcast via the Broadcast method or the
// provided context is done. It returns nil if a broadcast signal was received,
// or ctx.Err() if the context was done.
func (b *TimedSignalWaiter) Wait(ctx context.Context) error {
	select {
	case <-*b.chP.Load():
		// Signal received.
		return nil
	case <-ctx.Done():
		// Timeout expired.
		return ctx.Err()
	}
}

// Broadcast sends a signal to all goroutines currently waiting on this
// TimedSignalWaiter.
func (b *TimedSignalWaiter) Broadcast() {
	for {
		new := make(chan struct{})
		old := b.chP.Load()
		if b.chP.CompareAndSwap(old, &new) {
			close(*old)
			break
		}
	}
}
