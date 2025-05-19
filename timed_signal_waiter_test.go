package timedsignalwaiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTimedSignalWaiter_SingleWaiter_ReceivesBroadcast(t *testing.T) {
	t.Parallel()
	b := New()
	waitResult := make(chan error, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second) // Reasonably long timeout
		defer cancel()
		waitResult <- b.Wait(ctx)
	}()

	time.Sleep(50 * time.Millisecond) // Give waiter a chance to start
	b.Broadcast()

	select {
	case err := <-waitResult:
		if err != nil {
			t.Errorf("Expected Wait() to return nil (signal received), got %v", err)
		}
	case <-time.After(2 * time.Second): // Test timeout
		t.Fatal("Test timed out waiting for Wait() result")
	}
}

func TestTimedSignalWaiter_SingleWaiter_ContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	b := New()
	timeoutDuration := 50 * time.Millisecond
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()
	err := b.Wait(ctx)
	if err == nil {
		t.Errorf("Expected Wait() to return an error (timeout), got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected Wait() to return context.DeadlineExceeded, got %v", err)
	}

	elapsedTime := time.Since(startTime)
	// Check if timeout was roughly respected
	if elapsedTime < timeoutDuration || elapsedTime > timeoutDuration*2 { // Allow some leeway
		t.Logf("Warning: Timeout duration was %v, Wait() returned after %v (can be flaky on CI)", timeoutDuration, elapsedTime)
	}
}

func TestTimedSignalWaiter_MultipleWaiters_AllReceiveBroadcast(t *testing.T) {
	t.Parallel()
	b := New()
	numWaiters := 10
	var wg sync.WaitGroup
	wg.Add(numWaiters)
	results := make(chan error, numWaiters)

	for i := 0; i < numWaiters; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			results <- b.Wait(ctx)
		}()
	}

	time.Sleep(50 * time.Millisecond) // Give waiters a chance to start
	b.Broadcast()
	wg.Wait() // Wait for all goroutines to finish

	// Check results
	for i := 0; i < numWaiters; i++ {
		err := <-results
		if err != nil {
			t.Errorf("A waiter timed out or got an error (%v), expected all to receive signal (nil error)", err)
			return // Fail fast
		}
	}
}

func TestTimedSignalWaiter_BroadcastBeforeWait_ResultsInContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	b := New()
	b.Broadcast() // Signal when no one is actively waiting

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := b.Wait(ctx)
	if err == nil {
		t.Errorf("Expected Wait() to return an error (timeout) when signaled before wait, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected Wait() to return context.DeadlineExceeded, got %v", err)
	}
}

func TestTimedSignalWaiter_Reusability_BroadcastWaitBroadcastWait(t *testing.T) {
	t.Parallel()
	b := New()

	// First signal, should be missed by the subsequent Wait if it starts after the signal.
	b.Broadcast()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel1()
	err1 := b.Wait(ctx1)
	if err1 == nil {
		t.Logf("First wait after initial signal unexpectedly returned nil (signal received). This might happen if Wait was extremely fast relative to Signal's channel swap.")
	} else if !errors.Is(err1, context.DeadlineExceeded) {
		t.Errorf("First wait expected context.DeadlineExceeded, got %v", err1)
	}

	// Second round
	signalReceived := make(chan error, 1)
	go func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel2()
		signalReceived <- b.Wait(ctx2)
	}()

	time.Sleep(10 * time.Millisecond) // Ensure goroutine is waiting
	b.Broadcast()                     // New signal

	select {
	case err2 := <-signalReceived:
		if err2 != nil {
			t.Errorf("Second Wait() call did not receive the new signal, got error: %v", err2)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Test timed out waiting for second Wait() result")
	}
}

func TestTimedSignalWaiter_WaitWithImmediatelyDoneContext_NoBroadcastPending(t *testing.T) {
	t.Parallel()
	b := New()
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	err := b.Wait(ctx)
	if err == nil {
		t.Errorf("Expected Wait(ctx_with_zero_timeout) to return an error when no signal pending, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

func TestTimedSignalWaiter_WaitWithImmediatelyDoneContext_AfterBroadcastConsumed(t *testing.T) {
	t.Parallel()
	b := New()
	consumerDone := make(chan struct{})

	// Consumer
	go func() {
		defer close(consumerDone)
		ctxConsume, cancelConsume := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancelConsume()
		_ = b.Wait(ctxConsume) // Consumes the signal, ignore error for this part
	}()
	time.Sleep(10 * time.Millisecond) // Let consumer start
	b.Broadcast()
	<-consumerDone // Ensure signal is consumed

	ctxZero, cancelZero := context.WithTimeout(context.Background(), 0)
	defer cancelZero()
	err := b.Wait(ctxZero)
	if err == nil {
		t.Errorf("Expected Wait(ctx_with_zero_timeout) to return an error on a new channel generation, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

func TestTimedSignalWaiter_WaitWithNegativeTimeoutContext_ResultsInContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	b := New()
	ctx, cancel := context.WithTimeout(context.Background(), -10*time.Millisecond) // Behaves like 0 timeout
	defer cancel()
	err := b.Wait(ctx)
	if err == nil {
		t.Errorf("Expected Wait() with negative timeout to return an error, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

func TestTimedSignalWaiter_ConcurrentBroadcastsAndWaiters(t *testing.T) {
	t.Parallel()

	b := New()
	testDuration := 2 * time.Second // How long to run the test

	numSignalers := 5
	numWaiters := 20
	waiterAttemptTimeout := 30 * time.Millisecond // Timeout for each Wait() call by a waiter

	var signalsSent atomic.Int64
	var waitsAttempted atomic.Int64
	var signalsReceivedByWaiter atomic.Int64
	var timeoutsOccurredForWaiter atomic.Int64

	var wg sync.WaitGroup
	done := make(chan struct{}) // Channel to signal all goroutines to stop

	// Start Signaler Goroutines
	wg.Add(numSignalers)
	for i := 0; i < numSignalers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					b.Broadcast()
					signalsSent.Add(1)
					// time.Sleep(1 * time.Millisecond) // Optional small delay
				}
			}
		}()
	}

	// Start Waiter Goroutines
	wg.Add(numWaiters)
	for i := 0; i < numWaiters; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					waitsAttempted.Add(1)
					ctx, cancel := context.WithTimeout(context.Background(), waiterAttemptTimeout)
					err := b.Wait(ctx)
					cancel() // Important to cancel the context
					if err == nil {
						signalsReceivedByWaiter.Add(1)
					} else {
						timeoutsOccurredForWaiter.Add(1)
					}
				}
			}
		}()
	}

	// Let the test run
	time.Sleep(testDuration)
	close(done) // Tell all goroutines to stop
	wg.Wait()   // Wait for them to finish

	// Log statistics
	t.Logf("TestTimedSignalWaiter_ConcurrentBroadcastsAndWaiters Stats after %v:", testDuration)
	t.Logf("  Approximate Signals Sent by Signalers: %d", signalsSent.Load())
	t.Logf("  Wait Attempts by Waiters: %d", waitsAttempted.Load())
	t.Logf("  Signals Received by Waiters: %d", signalsReceivedByWaiter.Load())
	t.Logf("  Timeouts Occurred for Waiters: %d", timeoutsOccurredForWaiter.Load())

	// Assertions
	if numSignalers > 0 && signalsSent.Load() == 0 {
		t.Errorf("No signals were sent by signalers, expected some activity.")
	}
	if numWaiters > 0 && waitsAttempted.Load() == 0 {
		t.Errorf("No waits were attempted by waiters, expected some activity.")
	}

	// If waiters exist and signals were sent, we expect at least some signals to be received.
	// This is a heuristic: if many signals were sent and many waits attempted, but zero signals received,
	// it might indicate an issue.
	if numWaiters > 0 && signalsSent.Load() > int64(numWaiters*5) && signalsReceivedByWaiter.Load() == 0 && waitsAttempted.Load() > int64(numWaiters) {
		t.Errorf("No signals received by waiters despite numerous signals (%d) and wait attempts (%d). This could indicate a problem.",
			signalsSent.Load(), waitsAttempted.Load())
	}
	// The primary check is that the test completes without panicking or deadlocking.
}

func TestTimedSignalWaiter_DynamicWaiters_DuringBroadcasting(t *testing.T) {
	t.Parallel()

	b := New()
	overallTestDuration := 3 * time.Second        // Total time the test environment runs for.
	signalInterval := 15 * time.Millisecond       // How often a signal is broadcast.
	waiterAttemptTimeout := 60 * time.Millisecond // Timeout for each individual Wait() call.

	targetSuccessfulWaits := 100 // The test aims for waiters to successfully receive this many signals.
	maxConcurrentWaiters := 10   // Maintain this many goroutines actively trying to Wait().

	var successfulWaitsByLeavingWaiters atomic.Int64
	var timeoutsByLeavingWaiters atomic.Int64

	overallDone := make(chan struct{}) // Signals all major components to shut down.
	var managerWg sync.WaitGroup       // For the signaler and the waiter-launching goroutine.

	// Signaler Goroutine
	managerWg.Add(1)
	go func() {
		defer managerWg.Done()
		ticker := time.NewTicker(signalInterval)
		defer ticker.Stop()
		for {
			select {
			case <-overallDone:
				return
			case <-ticker.C:
				b.Broadcast()
			}
		}
	}()

	// Waiter Management Goroutine: Launches and manages short-lived waiter goroutines.
	managerWg.Add(1)
	go func() {
		defer managerWg.Done()
		var activeWaiterInstanceWg sync.WaitGroup // Tracks currently running short-lived waiter instances
		activeWaiterCount := 0
		// buffered channel to receive error results from one-shot waiter goroutines
		waiterResultChan := make(chan error, maxConcurrentWaiters)

		// Loop until target successful waits is met or overallDone is signaled.
		for successfulWaitsByLeavingWaiters.Load() < int64(targetSuccessfulWaits) {
			select {
			case <-overallDone: // Time to shut down the waiter launcher.
				activeWaiterInstanceWg.Wait() // Wait for any in-flight one-shot waiters to complete.
				return
			default:
				// Try to launch new waiters if we are below the concurrent cap and haven't met the target.
				if activeWaiterCount < maxConcurrentWaiters && successfulWaitsByLeavingWaiters.Load() < int64(targetSuccessfulWaits) {
					activeWaiterInstanceWg.Add(1)
					activeWaiterCount++
					go func() { // This is a one-shot waiter goroutine.
						defer activeWaiterInstanceWg.Done()
						// It's possible overallDone was closed just as this was launched.
						// If b.Wait() itself is quick (e.g., immediate timeout or signal), this check is less critical.
						// For robustness, a quick check here is fine.
						select {
						case <-overallDone:
							// If we are already shutting down, don't bother sending to waiterResultChan
							// as the receiver might also be shutting down.
							// activeWaiterCount will be decremented by the defer activeWaiterInstanceWg.Done() implicitly.
							return
						default:
						}
						ctx, cancel := context.WithTimeout(context.Background(), waiterAttemptTimeout)
						// Send the result of b.Wait(ctx) to the channel.
						// The cancel must be called after b.Wait returns.
						err := b.Wait(ctx)
						cancel()
						waiterResultChan <- err
					}()
				}

				select {
				case errResult := <-waiterResultChan:
					activeWaiterCount--
					if errResult == nil {
						successfulWaitsByLeavingWaiters.Add(1)
					} else {
						timeoutsByLeavingWaiters.Add(1)
					}
				case <-overallDone: // Check again for shutdown signal
					activeWaiterInstanceWg.Wait() // Wait for in-flight waiters.
					return
				default:
					// Prevent busy-spinning if no results and at capacity, or no results and nothing to launch.
					if activeWaiterCount == maxConcurrentWaiters ||
						(activeWaiterCount < maxConcurrentWaiters && successfulWaitsByLeavingWaiters.Load() >= int64(targetSuccessfulWaits)) {
						time.Sleep(1 * time.Millisecond) // Small pause
					}
				}
			}
		}
		activeWaiterInstanceWg.Wait() // Final wait for any remaining one-shot waiters.
	}()

	// Main test timer: controls the overall duration of the test.
	testEndTimer := time.NewTimer(overallTestDuration)
	defer testEndTimer.Stop()

	// Monitoring loop: periodically checks if the target is met or if the test duration has expired.
	monitorTicker := time.NewTicker(100 * time.Millisecond)
	defer monitorTicker.Stop()

Loop:
	for {
		select {
		case <-testEndTimer.C:
			t.Logf("TestTimedSignalWaiter_DynamicWaiters_DuringBroadcasting: Overall test duration (%v) expired.", overallTestDuration)
			break Loop
		case <-monitorTicker.C:
			if successfulWaitsByLeavingWaiters.Load() >= int64(targetSuccessfulWaits) {
				t.Logf("TestTimedSignalWaiter_DynamicWaiters_DuringBroadcasting: Target of %d successful waits achieved.", targetSuccessfulWaits)
				break Loop
			}
		}
	}

	close(overallDone) // Signal signaler and waiter manager to stop.
	managerWg.Wait()   // Wait for them to complete their cleanup.

	// Log statistics
	t.Logf("TestTimedSignalWaiter_DynamicWaiters_DuringBroadcasting Stats:")
	t.Logf("  Successful Signal Receptions by Leaving Waiters: %d (Target: %d)", successfulWaitsByLeavingWaiters.Load(), targetSuccessfulWaits)
	t.Logf("  Total Timeouts by Leaving Waiters: %d", timeoutsByLeavingWaiters.Load())

	// Assertions
	if successfulWaitsByLeavingWaiters.Load() < int64(targetSuccessfulWaits) {
		t.Errorf("Did not achieve target of %d successful waits. Got %d.", targetSuccessfulWaits, successfulWaitsByLeavingWaiters.Load())
	}
	if successfulWaitsByLeavingWaiters.Load() == 0 && targetSuccessfulWaits > 0 {
		t.Error("No successful waits were recorded by leaving waiters, expected some activity if target > 0.")
	}
}

func TestTimedSignalWaiter_SingleBroadcaster_ManyResponsiveWaiters(t *testing.T) {
	t.Parallel()

	b := New()

	// Configuration
	numWaiters := 50                        // Number of persistent waiter goroutines
	waitsPerWaiter := 50                    // Each waiter attempts this many Wait() calls
	signalInterval := 5 * time.Millisecond  // Signaler sends a signal this often
	waiterTimeout := 150 * time.Millisecond // Waiters use this timeout. Should be generous compared to signalInterval.
	// If signalInterval=10ms, waiterTimeout=150ms should catch a signal.

	// Statistics
	var signalsSentBySignaler atomic.Int64
	var totalWaitAttempts atomic.Int64
	var successfulWaitsByWaiters atomic.Int64
	var timedOutWaitsByWaiters atomic.Int64

	var signalerWg sync.WaitGroup
	var waiterWg sync.WaitGroup
	signalerDone := make(chan struct{}) // To stop the signaler

	// Single Signaler Goroutine
	signalerWg.Add(1)
	go func() {
		defer signalerWg.Done()
		ticker := time.NewTicker(signalInterval)
		defer ticker.Stop()
		for {
			select {
			case <-signalerDone:
				return
			case <-ticker.C:
				b.Broadcast()
				signalsSentBySignaler.Add(1)
			}
		}
	}()

	// Waiter Goroutines
	waiterWg.Add(numWaiters)
	for i := 0; i < numWaiters; i++ {
		go func(waiterID int) {
			defer waiterWg.Done()
			for j := 0; j < waitsPerWaiter; j++ {
				totalWaitAttempts.Add(1)
				ctx, cancel := context.WithTimeout(context.Background(), waiterTimeout)
				err := b.Wait(ctx)
				cancel()
				if err == nil {
					successfulWaitsByWaiters.Add(1)
				} else {
					timedOutWaitsByWaiters.Add(1)
					// t.Logf("Waiter %d, attempt %d: timed out with error %v (wait: %v, signal: %v)",
					//	waiterID, j, waiterTimeout, signalInterval)
				}
			}
		}(i)
	}

	// Wait for all waiter goroutines to complete all their attempts
	waiterWg.Wait()

	// Stop the signaler
	close(signalerDone)
	signalerWg.Wait() // Wait for signaler to stop

	// Log statistics
	t.Logf("TestTimedSignalWaiter_SingleBroadcaster_ManyResponsiveWaiters Stats:")
	t.Logf("  Signals Sent by Signaler: %d", signalsSentBySignaler.Load())
	t.Logf("  Total Wait Attempts by Waiters: %d", totalWaitAttempts.Load())
	t.Logf("  Successful Waits by Waiters: %d", successfulWaitsByWaiters.Load())
	t.Logf("  Timed Out Waits by Waiters: %d", timedOutWaitsByWaiters.Load())

	// Assertions
	// Expectation: With frequent signals and a reasonably longer waiter timeout,
	// timeouts should be rare or zero.
	expectedTotalAttempts := int64(numWaiters * waitsPerWaiter)
	if totalWaitAttempts.Load() != expectedTotalAttempts {
		t.Errorf("Mismatch in total wait attempts: recorded %d, expected %d", totalWaitAttempts.Load(), expectedTotalAttempts)
	}

	if expectedTotalAttempts > 0 {
		// Define a strict threshold for timeouts.
		// Let's allow a very small number, e.g., less than 0.5% of attempts, or a small absolute number.
		maxAllowedTimeouts := int64(float64(expectedTotalAttempts) * 0.005) // 0.5%
		if maxAllowedTimeouts == 0 && expectedTotalAttempts > 0 {           // Ensure at least 1 is allowed if very few attempts
			if expectedTotalAttempts < 200 { // if total attempts are low, allow 0 or 1
				// For very low total attempts, any timeout might be significant.
				// If strict zero is needed, set maxAllowedTimeouts directly to 0,
				// or use a specific number like 1 or 2 for flakiness.
			}
		}

		if timedOutWaitsByWaiters.Load() > maxAllowedTimeouts {
			timeoutPercentage := float64(timedOutWaitsByWaiters.Load()) / float64(expectedTotalAttempts) * 100
			t.Errorf("Observed %d timeouts (%.2f%%), which exceeds the allowed maximum of %d.",
				timedOutWaitsByWaiters.Load(), timeoutPercentage, maxAllowedTimeouts)
		} else if timedOutWaitsByWaiters.Load() > 0 {
			t.Logf("Observed %d timeouts, which is within the allowed threshold.", timedOutWaitsByWaiters.Load())
		}

		if successfulWaitsByWaiters.Load() == 0 && expectedTotalAttempts > 0 && signalsSentBySignaler.Load() > 0 {
			t.Errorf("No signals were successfully received by waiters, despite %d signals sent and %d attempts.",
				signalsSentBySignaler.Load(), expectedTotalAttempts)
		}
	} else if numWaiters > 0 && waitsPerWaiter > 0 { // Should have made attempts
		t.Error("No wait attempts were recorded by the main counter, check test configuration.")
	}
}
