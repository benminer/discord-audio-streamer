package audio

import (
	"sync"
	"testing"
	"time"
)

// TestPlayerIsPlayingInitial verifies a fresh Player reports not playing.
func TestPlayerIsPlayingInitial(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if p.IsPlaying() {
		t.Error("expected IsPlaying()=false on a fresh Player")
	}
}

// TestPlayerGetPositionNotPlaying verifies GetPosition returns 0 when idle.
func TestPlayerGetPositionNotPlaying(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if got := p.GetPosition(); got != 0 {
		t.Errorf("GetPosition() = %v, want 0 when not playing", got)
	}
}

// TestPlayerIsPlayingAtomicStoreLoad verifies Store/Load round-trip is correct.
// This is the core of the *bool → atomic.Bool migration.
func TestPlayerIsPlayingAtomicStoreLoad(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	if p.IsPlaying() {
		t.Error("initial: want false")
	}

	p.playing.Store(true)
	if !p.IsPlaying() {
		t.Error("after Store(true): want true")
	}

	p.playing.Store(false)
	if p.IsPlaying() {
		t.Error("after Store(false): want false")
	}
}

// TestPlayerIsPlayingConcurrent is a race-detector test. It hammers IsPlaying()
// from many goroutines while the playing flag is toggled concurrently.
// Run with: go test -race ./audio/...
func TestPlayerIsPlayingConcurrent(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 50

	// Writers: toggle playing flag
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p.playing.Store(i%2 == 0)
		}(i)
	}

	// Readers: call IsPlaying concurrently
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.IsPlaying()
		}()
	}

	wg.Wait()
}

// TestPlayerGetPositionConcurrent is a race-detector test for GetPosition(),
// which reads the atomic playing flag from outside the play loop.
func TestPlayerGetPositionConcurrent(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%3 == 0 {
				p.playing.Store(true)
			} else if i%3 == 1 {
				p.playing.Store(false)
			} else {
				_ = p.GetPosition()
			}
		}(i)
	}
	wg.Wait()
}

// TestPlayerStopIsLockfree verifies Stop() completes without blocking even
// when p.mutex is held externally. This was a concern flagged in review —
// if Stop() ever acquired p.mutex, calling it from recovery (while Play()
// holds the same mutex) would deadlock.
func TestPlayerStopIsLockfree(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	// Hold the play mutex as Play() would for the entire song duration
	p.mutex.Lock()
	defer p.mutex.Unlock()

	done := make(chan struct{})
	go func() {
		p.Stop() // must not block on p.mutex
		close(done)
	}()

	select {
	case <-done:
		// good — Stop() completed without acquiring the mutex
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop() blocked while mutex was held — potential deadlock in recovery path")
	}

	if !p.stopping.Load() {
		t.Error("stopping flag not set after Stop()")
	}
}

// TestPlayerIsPausedInitial verifies a fresh Player reports not paused.
func TestPlayerIsPausedInitial(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if p.IsPaused() {
		t.Error("expected IsPaused()=false on a fresh Player")
	}
}
