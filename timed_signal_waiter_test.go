package broadcaster

import (
	"fmt"
	"testing"
	"time"
)

func TestBroadcastContinuously(t *testing.T) {
	b := New("test")
	done := make(chan bool)
	errors := make(chan error, 10) // Buffer to catch errors without blocking

	// Start a routine that will broadcast signals continuously
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				b.Signal()
				time.Sleep(10 * time.Millisecond) // Short delay before sending next signal
			}
		}
	}()

	// Start multiple goroutines that will wait on the broadcaster
	for i := 0; i < 100; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ { // Each goroutine tries to wait 100 times
				if !b.Wait(50 * time.Millisecond) {
					errors <- fmt.Errorf("timeout on goroutine %d", id)
				}
			}
		}(i)
	}

	// Let the test run for a while
	time.Sleep(2 * time.Second)
	close(done) // Signal to stop broadcasting

	// Check for errors
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
}
