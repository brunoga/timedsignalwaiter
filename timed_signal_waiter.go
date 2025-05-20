package timedsignalwaiter

import (
	"context"
)

// TimedSignalWaiter allows goroutines to wait for a broadcast signal.
// The wait can be timed out or canceled via a context.
type TimedSignalWaiter struct {
	resultWaiter *TimedResultWaiter[struct{}]
}

// New creates a new TimedSignalWaiter.
func New() *TimedSignalWaiter {
	return &TimedSignalWaiter{
		resultWaiter: NewResultWaiter[struct{}](),
	}
}

// Wait blocks until a signal is broadcast or the context is done.
// Returns nil on signal, or ctx.Err() if context is done.
func (b *TimedSignalWaiter) Wait(ctx context.Context) error {
	// Wait on the internal resultWaiter. The actual result (struct{}{}) is ignored.
	_, err := b.resultWaiter.Wait(ctx)
	return err
}

// Broadcast sends a signal to all waiting goroutines.
func (b *TimedSignalWaiter) Broadcast() {
	// Broadcast a placeholder value.
	b.resultWaiter.Broadcast(struct{}{})
}
