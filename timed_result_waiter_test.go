package timedsignalwaiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testValInt1    = 42
	testValInt2    = 123
	testValString1 = "hello_world"
)

type waitResultWithValue[T any] struct {
	value T
	err   error
}

func TestTimedResultWaiter_Wait_SingleWaiterReceivesBroadcastedResult(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	resultChan := make(chan waitResultWithValue[int], 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		val, err := w.Wait(ctx)
		resultChan <- waitResultWithValue[int]{val, err}
	}()

	time.Sleep(50 * time.Millisecond) // Give waiter a chance to start
	w.Broadcast(testValInt1)

	select {
	case res := <-resultChan:
		if res.err != nil {
			t.Errorf("Expected Wait() to return nil error, got %v", res.err)
		}
		if res.value != testValInt1 {
			t.Errorf("Expected Wait() to return value %d, got %d", testValInt1, res.value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for Wait() result")
	}
}

func TestTimedResultWaiter_Wait_SingleWaiterContextDeadlineExceededReturnsZeroValue(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	timeoutDuration := 50 * time.Millisecond
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	val, err := w.Wait(ctx)

	if err == nil {
		t.Errorf("Expected Wait() to return an error (timeout), got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected Wait() to return context.DeadlineExceeded, got %v", err)
	}

	if val != 0 { // 0 is the zero value for int
		t.Errorf("Expected Wait() to return zero value 0 on timeout, got %d", val)
	}

	elapsedTime := time.Since(startTime)
	if elapsedTime < timeoutDuration {
		t.Errorf("Wait() returned too quickly: %v, expected at least %v", elapsedTime, timeoutDuration)
	}
	// Allow for some scheduling delay, but not excessively long
	if elapsedTime > timeoutDuration*2 && elapsedTime > 200*time.Millisecond { // Increased upper bound
		t.Errorf("Wait() took too long: %v, expected around %v", elapsedTime, timeoutDuration)
	}
}

func TestTimedResultWaiter_Wait_MultipleWaitersAllReceiveBroadcastedResult(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	numWaiters := 10
	var wg sync.WaitGroup
	wg.Add(numWaiters)
	resultsChan := make(chan waitResultWithValue[int], numWaiters)

	for i := 0; i < numWaiters; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			val, err := w.Wait(ctx)
			resultsChan <- waitResultWithValue[int]{val, err}
		}()
	}

	time.Sleep(50 * time.Millisecond) // Give waiters a chance to start
	w.Broadcast(testValInt2)
	wg.Wait() // Wait for all goroutines to finish

	for i := 0; i < numWaiters; i++ {
		res := <-resultsChan
		if res.err != nil {
			t.Errorf("A waiter timed out or got an error (%v), expected all to receive signal (nil error)", res.err)
			return // Fail fast
		}
		if res.value != testValInt2 {
			t.Errorf("A waiter received value %d, expected %d", res.value, testValInt2)
			return // Fail fast
		}
	}
}

func TestTimedResultWaiter_Wait_BroadcastWithNoWaiters_SubsequentWaitTimesOutReturningZeroValue(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	w.Broadcast(testValInt1) // Broadcast when no one is actively waiting

	// A new waiter starts *after* the broadcast. It will wait for the *next* broadcast.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	val, err := w.Wait(ctx)

	if err == nil {
		t.Errorf("Expected Wait() to return an error (timeout), got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected Wait() to return context.DeadlineExceeded, got %v", err)
	}
	if val != 0 { // Zero value for int
		t.Errorf("Expected Wait() to return zero value 0 on timeout, got %d", val)
	}
}

func TestTimedResultWaiter_Wait_Reusability_BroadcastWaitBroadcastWaitReceivesCorrectValues(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	resultChan1 := make(chan waitResultWithValue[int], 1)
	resultChan2 := make(chan waitResultWithValue[int], 1)

	// First wait-broadcast cycle
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		v, e := w.Wait(ctx)
		resultChan1 <- waitResultWithValue[int]{v, e}
	}()
	time.Sleep(20 * time.Millisecond) // Ensure goroutine is waiting
	w.Broadcast(testValInt1)

	select {
	case res1 := <-resultChan1:
		if res1.err != nil {
			t.Errorf("First Wait() call got error: %v", res1.err)
		}
		if res1.value != testValInt1 {
			t.Errorf("First Wait() call expected value %d, got %d", testValInt1, res1.value)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Test timed out waiting for first Wait() result")
	}

	// Second wait-broadcast cycle
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		v, e := w.Wait(ctx)
		resultChan2 <- waitResultWithValue[int]{v, e}
	}()
	time.Sleep(20 * time.Millisecond) // Ensure goroutine is waiting
	w.Broadcast(testValInt2)

	select {
	case res2 := <-resultChan2:
		if res2.err != nil {
			t.Errorf("Second Wait() call got error: %v", res2.err)
		}
		if res2.value != testValInt2 {
			t.Errorf("Second Wait() call expected value %d, got %d", testValInt2, res2.value)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Test timed out waiting for second Wait() result")
	}
}

func TestTimedResultWaiter_Wait_ImmediatelyDoneContext_NoBroadcastPendingReturnsZeroValue(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[string]() // Use string to test with a different type
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	val, err := w.Wait(ctx)
	if err == nil {
		t.Errorf("Expected Wait() to return an error, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
	if val != "" { // Zero value for string
		t.Errorf("Expected zero value \"\" for string, got %q", val)
	}
}

func TestTimedResultWaiter_Wait_ImmediatelyDoneContext_AfterPreviousBroadcastConsumedReturnsZeroValue(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	consumerDone := make(chan struct{})

	// Consumer goroutine
	go func() {
		defer close(consumerDone)
		ctxConsume, cancelConsume := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancelConsume()
		_, _ = w.Wait(ctxConsume) // Consumes the broadcast
	}()

	time.Sleep(10 * time.Millisecond) // Let consumer start
	w.Broadcast(testValInt1)          // Broadcast a value
	<-consumerDone                    // Ensure broadcast is consumed

	// Now, w.currentP points to a holder with result: testValInt1 and an *open* channel.
	// A new Wait with an immediately done context should timeout.
	ctxZero, cancelZero := context.WithTimeout(context.Background(), 0)
	defer cancelZero()
	val, err := w.Wait(ctxZero)

	if err == nil {
		t.Errorf("Expected Wait() to return an error, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
	if val != 0 { // Zero value for int
		t.Errorf("Expected zero value 0 for int, got %d", val)
	}
}

func TestTimedResultWaiter_Wait_NegativeTimeoutContext_ReturnsZeroValueAndDeadlineExceeded(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	ctx, cancel := context.WithTimeout(context.Background(), -10*time.Millisecond) // Behaves like 0 timeout
	defer cancel()

	val, err := w.Wait(ctx)
	if err == nil {
		t.Errorf("Expected Wait() to return an error, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
	if val != 0 { // Zero value for int
		t.Errorf("Expected zero value 0 for int, got %d", val)
	}
}

// --- Concurrency Tests ---
// These tests are more about liveness and general correctness under concurrent load.
// Asserting specific values can be tricky, so we focus on non-error and non-zero results.

func TestTimedResultWaiter_Wait_ConcurrentBroadcastsAndWaiting_WaitersReceiveBroadcastedValues(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	testDuration := 2 * time.Second

	numBroadcasters := 5
	numWaiters := 20
	waiterAttemptTimeout := 75 * time.Millisecond // Timeout for individual Wait calls

	var broadcastsSent atomic.Int64
	var waitsAttempted atomic.Int64
	var successfulWaits atomic.Int64
	var timedOutWaits atomic.Int64

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Broadcaster goroutines
	for i := 0; i < numBroadcasters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			valToBroadcast := id*1000 + 1 // Ensure non-zero
			for {
				select {
				case <-ctx.Done():
					return
				default:
					w.Broadcast(valToBroadcast)
					broadcastsSent.Add(1)
					valToBroadcast++
					time.Sleep(time.Duration(5+id%5) * time.Millisecond) // Stagger broadcasts
				}
			}
		}(i)
	}

	// Waiter goroutines
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					waitsAttempted.Add(1)
					waitCtx, waitCancel := context.WithTimeout(ctx, waiterAttemptTimeout)
					val, err := w.Wait(waitCtx)
					waitCancel()

					if err == nil {
						successfulWaits.Add(1)
						if val == 0 { // Assuming 0 is not a valid broadcasted value here
							t.Errorf("Waiter received zero value with nil error")
						}
					} else if errors.Is(err, context.DeadlineExceeded) {
						timedOutWaits.Add(1)
					} else {
						// context.Canceled is expected when the overall test context is done.
						if !errors.Is(err, context.Canceled) {
							t.Errorf("Waiter received unexpected error: %v", err)
						}
					}
				}
			}
		}()
	}

	time.Sleep(testDuration)
	cancel() // Signal goroutines to stop
	wg.Wait()

	t.Logf("TestTimedResultWaiter_Wait_ConcurrentBroadcastsAndWaiting Stats after %v:", testDuration)
	t.Logf("  Broadcasts Sent: %d", broadcastsSent.Load())
	t.Logf("  Wait Attempts: %d", waitsAttempted.Load())
	t.Logf("  Successful Waits: %d", successfulWaits.Load())
	t.Logf("  Timed Out Waits: %d", timedOutWaits.Load())

	if successfulWaits.Load() == 0 && broadcastsSent.Load() > 0 {
		t.Errorf("No successful waits despite broadcasts being sent.")
	}
}

func TestTimedResultWaiter_Wait_DynamicWaitersDuringContinuousBroadcasting_WaitersReceiveBroadcastedValues(t *testing.T) {
	t.Parallel()
	w := NewResultWaiter[int]()
	overallTestDuration := 3 * time.Second
	broadcastInterval := 15 * time.Millisecond
	waiterAttemptTimeout := 60 * time.Millisecond
	maxConcurrentWaiters := 10
	targetSuccessfulWaits := int64(50) // Target for short-lived waiters

	var broadcastsSent atomic.Int64
	var successfulWaitsByLeavingWaiters atomic.Int64
	var timeoutsByLeavingWaiters atomic.Int64

	overallCtx, overallCancel := context.WithCancel(context.Background())
	defer overallCancel() // Ensure everything stops eventually

	var managerWg sync.WaitGroup

	// Broadcaster
	managerWg.Add(1)
	go func() {
		defer managerWg.Done()
		ticker := time.NewTicker(broadcastInterval)
		defer ticker.Stop()
		valToBroadcast := 1001 // Ensure non-zero
		for {
			select {
			case <-overallCtx.Done():
				return
			case <-ticker.C:
				w.Broadcast(valToBroadcast)
				broadcastsSent.Add(1)
				valToBroadcast++
			}
		}
	}()

	// Waiter Manager (spawns short-lived waiters)
	managerWg.Add(1)
	go func() {
		defer managerWg.Done()
		activeWaiters := 0
		var activeWaiterWg sync.WaitGroup
		waiterResults := make(chan waitResultWithValue[int], maxConcurrentWaiters)

		for successfulWaitsByLeavingWaiters.Load() < targetSuccessfulWaits {
			select {
			case <-overallCtx.Done():
				activeWaiterWg.Wait()
				return
			default:
			}

			if activeWaiters < maxConcurrentWaiters {
				activeWaiters++
				activeWaiterWg.Add(1)
				go func() {
					defer activeWaiterWg.Done()
					waitCtx, waitCancel := context.WithTimeout(overallCtx, waiterAttemptTimeout)
					val, err := w.Wait(waitCtx)
					waitCancel()
					waiterResults <- waitResultWithValue[int]{val, err}
				}()
			}

			select {
			case res := <-waiterResults:
				activeWaiters--
				if res.err == nil {
					successfulWaitsByLeavingWaiters.Add(1)
					if res.value == 0 { // Assuming 0 is not a valid broadcasted value
						t.Errorf("Dynamic waiter received zero value with nil error")
					}
				} else if errors.Is(res.err, context.DeadlineExceeded) {
					timeoutsByLeavingWaiters.Add(1)
				} else {
					t.Errorf("Dynamic waiter received unexpected error: %v", res.err)
				}
			case <-overallCtx.Done():
				activeWaiterWg.Wait()
				return
			case <-time.After(5 * time.Millisecond): // Non-blocking check for results
			}
			if successfulWaitsByLeavingWaiters.Load() >= targetSuccessfulWaits {
				break
			}
		}
		activeWaiterWg.Wait() // Wait for any remaining active waiters
	}()

	testEndTimer := time.NewTimer(overallTestDuration)
	defer testEndTimer.Stop()

	monitorTicker := time.NewTicker(100 * time.Millisecond)
	defer monitorTicker.Stop()

Loop:
	for {
		select {
		case <-testEndTimer.C:
			t.Logf("TestTimedResultWaiter_Wait_DynamicWaiters: Overall test duration (%v) expired.", overallTestDuration)
			break Loop
		case <-monitorTicker.C:
			if successfulWaitsByLeavingWaiters.Load() >= targetSuccessfulWaits {
				t.Logf("TestTimedResultWaiter_Wait_DynamicWaiters: Target of %d successful waits achieved.", targetSuccessfulWaits)
				break Loop
			}
		}
	}

	overallCancel()  // Signal all goroutines to stop
	managerWg.Wait() // Wait for broadcaster and manager to complete

	t.Logf("TestTimedResultWaiter_Wait_DynamicWaitersDuringContinuousBroadcasting Stats:")
	t.Logf("  Broadcasts Sent: %d", broadcastsSent.Load())
	t.Logf("  Successful Waits by Leaving Waiters: %d (Target: %d)", successfulWaitsByLeavingWaiters.Load(), targetSuccessfulWaits)
	t.Logf("  Timeouts by Leaving Waiters: %d", timeoutsByLeavingWaiters.Load())

	if successfulWaitsByLeavingWaiters.Load() < targetSuccessfulWaits && broadcastsSent.Load() > 0 {
		t.Logf("Warning: Target successful waits not met, but test might still be valid if time ran out.")
	}
	if successfulWaitsByLeavingWaiters.Load() == 0 && broadcastsSent.Load() > targetSuccessfulWaits {
		t.Errorf("No successful waits by dynamic waiters despite many broadcasts.")
	}
}

// Note: The "SingleBroadcasterManyResponsiveWaiters" test is harder to make meaningful assertions
// about *which specific value* each waiter gets in a highly concurrent scenario without complex
// synchronization that might alter the behavior being tested. The primary goal here is that
// waiters do receive *a* valid, broadcasted value if they don't time out.
// The existing concurrency tests cover this aspect well.
// If specific value sequence tracking is needed, the test would become significantly more complex.
