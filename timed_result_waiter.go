package timedsignalwaiter

import (
	"context"
	"sync/atomic"
)

// resultAndChannelHolder holds the channel to wait on and the associated result.
// This struct is managed by an atomic.Pointer in TimedResultWaiter.
// Bundling the channel and result ensures that a waiter receives the result
// specifically associated with the signal event that woke it up.
type resultAndChannelHolder[T any] struct {
	ch     chan struct{} // Closed to signal waiters.
	result T             // The result associated with this signal.
}

// TimedResultWaiter allows one or more goroutines to wait for a broadcast
// signal and receive an associated result of a generic type T.
// The wait operation can be canceled or timed out via a context.Context.
type TimedResultWaiter[T any] struct {
	// currentP holds a pointer to the current resultAndChannelHolder.
	// This allows atomic updates to both the signal channel and its associated result.
	currentP atomic.Pointer[resultAndChannelHolder[T]]
}

// NewResultWaiter creates a new TimedResultWaiter for a given generic type T.
// Initially, it holds the zero value for T until the first Broadcast.
func NewResultWaiter[T any]() *TimedResultWaiter[T] {
	// Initialize with a holder containing an open channel and the zero value for T.
	// Waiters attaching before the first broadcast will wait on this initial channel.
	initialHolder := &resultAndChannelHolder[T]{
		ch: make(chan struct{}),
		// result is the zero value of T by default
	}
	w := &TimedResultWaiter[T]{}
	w.currentP.Store(initialHolder)
	return w
}

// Broadcast sets the result and signals all currently waiting goroutines.
// Waiters that are currently blocked in Wait() will receive the provided 'result'
// when they wake up. Goroutines that call Wait() after this Broadcast has
// completed (and before the next one) will also receive this 'result'.
func (w *TimedResultWaiter[T]) Broadcast(result T) {
	// Prepare a holder for the *next* generation of waiters.
	// This new holder will carry the current 'result' for those future waiters.
	nextGenerationHolder := &resultAndChannelHolder[T]{
		ch:     make(chan struct{}),
		result: result,
	}

	for {
		// currentHolder is the one that existing waiters are currently blocked on.
		currentHolder := w.currentP.Load()

		// Atomically swap w.currentP to point to the nextGenerationHolder.
		// This means new calls to Wait() will now use nextGenerationHolder.
		if w.currentP.CompareAndSwap(currentHolder, nextGenerationHolder) {
			// We've successfully made nextGenerationHolder the "current" one
			// for new waiters.
			//
			// Now, update the result in the currentHolder (which is now the "old" one).
			// This ensures that waiters blocked on currentHolder.ch receive the new 'result'.
			currentHolder.result = result
			// Close the channel of the currentHolder to wake up its waiters.
			close(currentHolder.ch)
			break
		}
		// CAS failed, another goroutine updated currentP. Retry the loop.
	}
}

// Wait blocks until a result is broadcast via the Broadcast method (triggering the signal)
// or the provided context is done (e.g., timeout or cancellation).
//
// It returns:
// - The broadcasted result and a nil error if a result was received due to a Broadcast.
// - The zero value for T and ctx.Err() if the context is done before a result is received.
func (w *TimedResultWaiter[T]) Wait(ctx context.Context) (T, error) {
	// Load the current holder. Waiters will listen on the channel from this specific holder.
	holder := w.currentP.Load()

	select {
	case <-holder.ch:
		// Signal received on the channel from the loaded holder.
		// Return the result from this same holder, ensuring causal consistency.
		return holder.result, nil
	case <-ctx.Done():
		// Context was done (e.g., timeout or cancellation).
		var zero T
		return zero, ctx.Err()
	}
}
