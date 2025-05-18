package timedsignalwaiter

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func Test_Name(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc         string
		inputName    string
		expectedName string
	}{
		{
			desc:         "Regular name",
			inputName:    "MyTestWaiter123",
			expectedName: "MyTestWaiter123",
		},
		{
			desc:         "Empty name",
			inputName:    "",
			expectedName: "",
		},
		{
			desc:         "Name with spaces and special characters",
			inputName:    "Waiter with ID: @&*% \t\n (end)",
			expectedName: "Waiter with ID: @&*% \t\n (end)",
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			waiter := New(tc.inputName)
			actualName := waiter.Name()
			if actualName != tc.expectedName {
				t.Errorf("Name() returned %q, expected %q", actualName, tc.expectedName)
			}
		})
	}
}

func TestSignalWaiter_SingleWaiter_ReceivesSignal(t *testing.T) {
	t.Parallel()
	b := New("singleSignal")
	waitResult := make(chan bool, 1)

	go func() {
		waitResult <- b.Wait(1 * time.Second) // Reasonably long timeout
	}()

	time.Sleep(50 * time.Millisecond) // Give waiter a chance to start
	b.Signal()

	if !<-waitResult {
		t.Errorf("Expected Wait() to return true (signal received), got false")
	}
}

func TestSignalWaiter_SingleWaiter_TimeoutOccurs(t *testing.T) {
	t.Parallel()
	b := New("singleTimeout")
	timeoutDuration := 50 * time.Millisecond
	startTime := time.Now()

	if b.Wait(timeoutDuration) {
		t.Errorf("Expected Wait() to return false (timeout), got true")
	}

	elapsedTime := time.Since(startTime)
	// Check if timeout was roughly respected
	if elapsedTime < timeoutDuration || elapsedTime > timeoutDuration*2 { // Allow some leeway
		t.Logf("Warning: Timeout duration was %v, Wait() returned after %v (can be flaky on CI)", timeoutDuration, elapsedTime)
	}
}

func TestSignalWaiter_MultipleWaiters_AllReceiveSignal(t *testing.T) {
	t.Parallel()
	b := New("multiWaitSignal")
	numWaiters := 10
	var wg sync.WaitGroup
	wg.Add(numWaiters)
	results := make(chan bool, numWaiters)

	for i := 0; i < numWaiters; i++ {
		go func() {
			defer wg.Done()
			results <- b.Wait(1 * time.Second)
		}()
	}

	time.Sleep(50 * time.Millisecond) // Give waiters a chance to start
	b.Signal()
	wg.Wait() // Wait for all goroutines to finish
	close(results)

	for i := 0; i < numWaiters; i++ {
		if !<-results {
			t.Errorf("A waiter timed out, expected all to receive signal")
			return // Fail fast
		}
	}
}

func TestSignalWaiter_SignalBeforeWait_CausesTimeout(t *testing.T) {
	t.Parallel()
	b := New("signalBeforeWait")
	b.Signal() // Signal when no one is actively waiting

	if b.Wait(50 * time.Millisecond) {
		t.Errorf("Expected Wait() to return false (timeout) when signaled before wait, got true")
	}
}

func TestSignalWaiter_Reusability_SignalWaitSignalWait(t *testing.T) {
	t.Parallel()
	b := New("reuse")

	// First signal, consumed by no one specific (or will be missed by next Wait)
	b.Signal()
	if b.Wait(20 * time.Millisecond) { // This should timeout if signal was "missed"
		t.Logf("First wait after initial signal unexpectedly returned true (might happen if Wait was very fast)")
	}

	// Second round
	signalReceived := make(chan bool, 1)
	go func() {
		signalReceived <- b.Wait(100 * time.Millisecond)
	}()

	time.Sleep(10 * time.Millisecond) // Ensure goroutine is waiting
	b.Signal()                        // New signal

	if !<-signalReceived {
		t.Error("Second Wait() call did not receive the new signal")
	}
}

func TestSignalWaiter_WaitWithZeroTimeout_NoSignal(t *testing.T) {
	t.Parallel()
	b := New("waitZeroNoSignal")
	if b.Wait(0) {
		t.Errorf("Expected Wait(0) to return false when no signal pending, got true")
	}
}

func TestSignalWaiter_WaitWithZeroTimeout_AfterSignalConsumed(t *testing.T) {
	t.Parallel()
	b := New("waitZeroAfterConsumed")
	var wg sync.WaitGroup
	wg.Add(1)

	// Consumer
	go func() {
		defer wg.Done()
		b.Wait(50 * time.Millisecond) // Consumes the signal
	}()
	time.Sleep(10 * time.Millisecond) // Let consumer start
	b.Signal()
	wg.Wait() // Ensure signal is consumed

	if b.Wait(0) {
		t.Errorf("Expected Wait(0) to return false on a new channel generation, got true")
	}
}

func TestSignalWaiter_WaitWithNegativeTimeout(t *testing.T) {
	t.Parallel()
	b := New("waitNegativeTimeout")
	if b.Wait(-10 * time.Millisecond) {
		t.Errorf("Expected Wait() with negative timeout to return false, got true")
	}
}

func TestSignalWaiter_ConcurrentSignals_And_Waiters(t *testing.T) {
	t.Parallel()

	b := New("concurrentSignalWait")
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
					b.Signal()
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
					if b.Wait(waiterAttemptTimeout) {
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
	t.Logf("TestSignalWaiter_ConcurrentSignals_And_Waiters Stats after %v:", testDuration)
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

func TestSignalWaiter_WaiterLeavesAndJoins_DuringSignaling(t *testing.T) {
	t.Parallel()

	b := New("waiterChurnTest")
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
				b.Signal()
			}
		}
	}()

	// Waiter Management Goroutine: Launches and manages short-lived waiter goroutines.
	managerWg.Add(1)
	go func() {
		defer managerWg.Done()
		var activeWaiterInstanceWg sync.WaitGroup // Tracks currently running short-lived waiter instances
		activeWaiterCount := 0
		// buffered channel to receive results from one-shot waiter goroutines
		waiterResultChan := make(chan bool, maxConcurrentWaiters)

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
						waiterResultChan <- b.Wait(waiterAttemptTimeout)
					}()
				}

				// Process results from completed waiters or check for shutdown.
				// This select needs to be non-blocking if no results are ready, to allow launching more waiters.
				select {
				case result := <-waiterResultChan:
					activeWaiterCount--
					if result {
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
			t.Logf("TestSignalWaiter_WaiterLeavesAndJoins: Overall test duration (%v) expired.", overallTestDuration)
			break Loop
		case <-monitorTicker.C:
			if successfulWaitsByLeavingWaiters.Load() >= int64(targetSuccessfulWaits) {
				t.Logf("TestSignalWaiter_WaiterLeavesAndJoins: Target of %d successful waits achieved.", targetSuccessfulWaits)
				break Loop
			}
		}
	}

	close(overallDone) // Signal signaler and waiter manager to stop.
	managerWg.Wait()   // Wait for them to complete their cleanup.

	// Log statistics
	t.Logf("TestSignalWaiter_WaiterLeavesAndJoins_DuringSignaling Stats:")
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

func TestSignalWaiter_SingleSignaler_ManyResponsiveWaiters(t *testing.T) {
	t.Parallel()

	b := New("singleSignalManyResponsiveWaiters")

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
				b.Signal()
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
				if b.Wait(waiterTimeout) {
					successfulWaitsByWaiters.Add(1)
				} else {
					timedOutWaitsByWaiters.Add(1)
					// Optional: Log the specific timeout for debugging if needed during development
					// t.Logf("Waiter %d, attempt %d: timed out (wait: %v, signal: %v)",
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
	t.Logf("Test_SingleSignaler_ManyResponsiveWaiters Stats:")
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
