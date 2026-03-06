package controller

import (
	"sync"
	"testing"
	"time"

	"beatbot/youtube"
)

// TestWaitForStreamURLChannelReady tests when streamReady is closed
// and Stream is set - should return true.
func TestWaitForStreamURLChannelReady(t *testing.T) {
	item := &GuildQueueItem{
		Video:       youtube.VideoResponse{VideoID: "test"},
		streamReady: make(chan struct{}),
	}

	// Close the channel to signal ready
	close(item.streamReady)

	// Set the stream
	item.Stream = &youtube.YoutubeStream{StreamURL: "http://test"}

	// Should return true since Stream is now set
	if !item.WaitForStreamURL() {
		t.Error("expected true when stream is ready and Stream is set")
	}
}

// TestWaitForStreamURLChannelReadyButNoStream tests when streamReady is
// closed but Stream is still nil - should return false.
func TestWaitForStreamURLChannelReadyButNoStream(t *testing.T) {
	item := &GuildQueueItem{
		Video:       youtube.VideoResponse{VideoID: "test"},
		streamReady: make(chan struct{}),
	}

	// Close the channel to signal ready
	close(item.streamReady)

	// But Stream is still nil
	if item.WaitForStreamURL() {
		t.Error("expected false when streamReady closed but Stream is nil")
	}
}

// TestWaitForStreamURLTimeout tests the 30-second timeout.
// This is a slow test so we use a short timeout in the implementation.
func TestWaitForStreamURLTimeout(t *testing.T) {
	item := &GuildQueueItem{
		Video:       youtube.VideoResponse{VideoID: "test"},
		streamReady: make(chan struct{}),
	}

	// Don't close streamReady - should timeout
	result := item.WaitForStreamURL()

	if result {
		t.Error("expected false on timeout")
	}
}

// TestWaitForStreamURLNilChannel tests the fallback when streamReady is nil.
func TestWaitForStreamURLNilChannel(t *testing.T) {
	item := &GuildQueueItem{
		Video:       youtube.VideoResponse{VideoID: "test"},
		Stream:      nil,
		streamReady: nil, // Explicitly nil - should fall back to Stream != nil check
	}

	// Should fall back to checking Stream != nil
	result := item.WaitForStreamURL()
	if result {
		t.Error("expected false when Stream is nil and streamReady is nil")
	}

	// Now set Stream
	item.Stream = &youtube.YoutubeStream{StreamURL: "http://test"}
	result = item.WaitForStreamURL()
	if !result {
		t.Error("expected true when Stream is set (even with nil streamReady)")
	}
}

// TestWaitForStreamURLMultipleWaiters tests multiple goroutines waiting
// on the same streamReady channel - all should be notified.
func TestWaitForStreamURLMultipleWaiters(t *testing.T) {
	item := &GuildQueueItem{
		Video:       youtube.VideoResponse{VideoID: "test"},
		streamReady: make(chan struct{}),
	}

	const waiters = 10
	results := make(chan bool, waiters)

	// Start multiple waiters
	for i := 0; i < waiters; i++ {
		go func() {
			result := item.WaitForStreamURL()
			results <- result
		}()
	}

	// Give them time to start waiting
	time.Sleep(50 * time.Millisecond)

	// Close streamReady and set Stream
	item.Stream = &youtube.YoutubeStream{StreamURL: "http://test"}
	close(item.streamReady)

	// All waiters should return true
	for i := 0; i < waiters; i++ {
		select {
		case result := <-results:
			if !result {
				t.Errorf("waiter %d got false, expected true", i)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("waiter %d did not return", i)
		}
	}
}

// TestWaitForStreamURLConcurrentWithSet tests concurrent access to
// streamReady and Stream - a race-detector test.
func TestWaitForStreamURLConcurrentWithSet(t *testing.T) {
	item := &GuildQueueItem{
		Video:       youtube.VideoResponse{VideoID: "test"},
		streamReady: make(chan struct{}),
		Stream:      nil,
	}

	var wg sync.WaitGroup
	const goroutines = 50

	// Goroutines that wait on the channel
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item.WaitForStreamURL()
		}()
	}

	// One goroutine sets Stream and closes the channel (only once!)
	wg.Add(1)
	go func() {
		defer wg.Done()
		item.Stream = &youtube.YoutubeStream{StreamURL: "http://test"}
		close(item.streamReady)
	}()

	wg.Wait()
}
