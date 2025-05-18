package timedsignalwaiter

import (
	"sync/atomic"
	"time"
)

// TimedSignalWaiter is a synchronization primitive that allows one or more
// goroutines to wait for a signal from another goroutine or for a timeout.
type TimedSignalWaiter struct {
	name string
	chP  atomic.Pointer[chan struct{}]
}

// New creates a new TimedSignalWaiter.
func New(name string) *TimedSignalWaiter {
	b := &TimedSignalWaiter{
		name: name,
	}

	ch := make(chan struct{})
	b.chP.Store(&ch)

	return b
}

// Wait waits for a signal from another goroutine or for a timeout.
// It returns true if the signal was received, and false if the timeout expired.
func (b *TimedSignalWaiter) Wait(timeout time.Duration) bool {
	if timeout <= 0 {
		// Immediate expiration. Either the channel has been signaled already or
		// we immediatelly signal a timeout.
		select {
		case <-*b.chP.Load():
			return true
		default:
			return false
		}
	}

	select {
	case <-*b.chP.Load():
		// Signal received.
		return true
	case <-time.After(timeout):
		// Timeout expired.
		return false
	}
}

// Signal signals all goroutines waiting on the Broadcaster.
func (b *TimedSignalWaiter) Signal() {
	for {
		new := make(chan struct{})
		old := b.chP.Load()
		if b.chP.CompareAndSwap(old, &new) {
			close(*old)
			break
		}
	}
}

// Name returns the name of the TimedSignalWaiter.
func (b *TimedSignalWaiter) Name() string {
	return b.name
}
