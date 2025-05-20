package timedsignalwaiter

import (
	"context"
	"sync/atomic"
)

// resultHolder holds the broadcasted result and the channel for signaling.
type resultHolder[T any] struct {
	ch     chan struct{} // Closed to signal waiters.
	result T             // The result associated with this signal.
}

// TimedResultWaiter allows goroutines to wait for a broadcasted result.
// The wait can be timed out or canceled via a context.
type TimedResultWaiter[T any] struct {
	currentP atomic.Pointer[resultHolder[T]]
}

// NewResultWaiter creates a new TimedResultWaiter for a given generic type T.
func NewResultWaiter[T any]() *TimedResultWaiter[T] {
	initialHolder := &resultHolder[T]{
		ch: make(chan struct{}),
	}
	w := &TimedResultWaiter[T]{}
	w.currentP.Store(initialHolder)
	return w
}

// Broadcast sets the result and signals waiting goroutines.
func (w *TimedResultWaiter[T]) Broadcast(result T) {
	// New holder for subsequent Wait calls, with the new result and channel.
	newHolder := &resultHolder[T]{
		ch:     make(chan struct{}),
		result: result,
	}

	for {
		oldHolder := w.currentP.Load()
		if w.currentP.CompareAndSwap(oldHolder, newHolder) {
			// For waiters on oldHolder: update their result and unblock them.
			oldHolder.result = result
			close(oldHolder.ch)
			break
		}
	}
}

// Wait blocks until a result is broadcast via the Broadcast method (triggering the signal)
// or the context is done. Returns the broadcasted result and a nil error if a result was
// received due to a Broadcast or the zero value for T and ctx.Err() if the context is
// done before a result is received.
func (w *TimedResultWaiter[T]) Wait(ctx context.Context) (T, error) {
	holder := w.currentP.Load()

	select {
	case <-holder.ch:
		// Signal received. Return the result from this holder.
		return holder.result, nil
	case <-ctx.Done():
		// Context cancelled/timed out.
		var zero T
		return zero, ctx.Err()
	}
}
