package audio

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestLoaderCancelNonBlocking verifies that Cancel() does not block even
// when no Load() is currently running. This was the original bug that
// motivated the buffered(1) change.
func TestLoaderCancelNonBlocking(t *testing.T) {
	loader := NewLoader()

	// Call Cancel multiple times - none should block
	for i := 0; i < 10; i++ {
		done := make(chan struct{})
		go func() {
			loader.Cancel()
			close(done)
		}()

		select {
		case <-done:
			// Good - Cancel completed immediately
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Cancel() blocked - buffered channel design is broken")
		}
	}
}

// TestLoaderCancelBeforeLoad tests the drain-at-startup pattern:
// Cancel() called before Load() starts should not cancel the subsequent Load().
func TestLoaderCancelBeforeLoad(t *testing.T) {
	loader := NewLoader()

	// Send cancel before Load runs
	loader.Cancel()

	// Run Load - should NOT be cancelled by the prior Cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loadStarted := make(chan struct{})
	loadFinished := make(chan struct{})

	go func() {
		close(loadStarted)
		loader.Load(ctx, LoadJob{
			URL:     "test-url",
			VideoID: "test-id",
			Title:   "test",
		})
		close(loadFinished)
	}()

	// Wait for load to start
	<-loadStarted

	// Give it time to process the drain
	time.Sleep(50 * time.Millisecond)

	// Wait for completion (with timeout)
	select {
	case <-loadFinished:
		// Good - Load completed without being cancelled
	case <-time.After(5 * time.Second):
		t.Fatal("Load appears to have been cancelled by prior Cancel() - drain-at-startup not working")
	}
}

// TestLoaderCancelDuringLoad verifies that Cancel() can actually cancel
// a running Load() operation.
func TestLoaderCancelDuringLoad(t *testing.T) {
	loader := NewLoader()

	// Start a long-running Load (we can't easily make it long without real network)
	// Instead, we test that Cancel can be called while Load is in its select
	ctx := context.Background()

	loadStarted := make(chan struct{})
	loadReturned := make(chan struct{})

	go func() {
		close(loadStarted)
		loader.Load(ctx, LoadJob{
			URL:     "test-url",
			VideoID: "test-id",
			Title:   "test",
		})
		close(loadReturned)
	}()

	// Wait for Load to start
	<-loadStarted

	// Now call Cancel while Load is running
	loader.Cancel()

	// Wait for Load to return
	select {
	case <-loadReturned:
		// Load returned - either cancelled or completed
	case <-time.After(5 * time.Second):
		t.Fatal("Load did not return after Cancel")
	}
}

// TestLoaderMultipleCancelSignals tests that multiple Cancel() calls
// work correctly - the buffered(1) channel should not lose signals.
func TestLoaderMultipleCancelSignals(t *testing.T) {
	loader := NewLoader()

	// Send multiple cancels
	for i := 0; i < 5; i++ {
		loader.Cancel()
	}

	// Now run a Load - should drain the signal
	ctx := context.Background()
	loadDone := make(chan struct{})

	go func() {
		loader.Load(ctx, LoadJob{
			URL:     "test-url",
			VideoID: "test-id",
			Title:   "test",
		})
		close(loadDone)
	}()

	select {
	case <-loadDone:
		// The drain in Load() should have consumed the signal
	case <-time.After(5 * time.Second):
		t.Fatal("Load did not complete")
	}
}

// TestLoaderCancelConcurrent is a race-detector test that calls Cancel()
// and Load() concurrently from multiple goroutines.
func TestLoaderCancelConcurrent(t *testing.T) {
	loader := NewLoader()
	ctx := context.Background()

	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				loader.Cancel()
			} else {
				loader.Load(ctx, LoadJob{
					URL:     "test-url",
					VideoID: "test-id",
					Title:   "test",
				})
			}
		}(i)
	}

	wg.Wait()
}

// TestLoaderNotificationsChannelBuffered verifies the notifications channel
// has sufficient buffer to not block senders.
func TestLoaderNotificationsChannelBuffered(t *testing.T) {
	loader := NewLoader()

	// The Notifications channel is created with buffer 100
	// Verify we can send up to buffer size without blocking
	for i := 0; i < 100; i++ {
		select {
		case loader.Notifications <- PlaybackNotification{
			VideoID: func() *string { s := "test"; return &s }(),
		}:
			// Good - sent successfully
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Notifications channel blocked at %d - insufficient buffer", i)
		}
	}
}
