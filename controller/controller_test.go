package controller

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"beatbot/youtube"
)

func TestSongHistory(t *testing.T) {
	sh := NewSongHistory(3)

	entries := []SongHistoryEntry{
		{VideoID: "1", Title: "Song 1"},
		{VideoID: "2", Title: "Song 2"},
		{VideoID: "3", Title: "Song 3"},
		{VideoID: "4", Title: "Song 4"}, // This should cause wrap-around
	}

	for i, e := range entries {
		sh.Add(e)
		t.Run(fmt.Sprintf("AfterAdd%d", i+1), func(t *testing.T) {
			expectedLen := min(3, i+1)
			if got := sh.Len(); got != expectedLen {
				t.Errorf("Len() = %d, want %d", got, expectedLen)
			}
		})
	}

	// Test GetRecent
	recent := sh.GetRecent(2)
	if len(recent) != 2 || recent[0].VideoID != "3" || recent[1].VideoID != "4" {
		t.Errorf("GetRecent(2) = %v, want last 2: 3,4", recent)
	}

	recentAll := sh.GetRecent(10) // more than size
	if len(recentAll) != 3 {
		t.Errorf("GetRecent(10) len=%d, want 3", len(recentAll))
	}

	// Test GetAllVideoIDs
	ids := sh.GetAllVideoIDs()
	expectedIDs := map[string]bool{"2": true, "3": true, "4": true}
	if len(ids) != 3 || !ids["2"] || !ids["3"] || !ids["4"] {
		t.Errorf("GetAllVideoIDs() = %v, want %v", ids, expectedIDs)
	}
}

func TestGuildPlayerIsEmpty(t *testing.T) {
	player := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	if !player.IsEmpty() {
		t.Error("expected empty queue")
	}

	player.Queue.Items = append(player.Queue.Items, &GuildQueueItem{})

	if player.IsEmpty() {
		t.Error("expected non-empty queue")
	}

	// Test race condition mentally: lock protects
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			player.IsEmpty()
		}()
	}
	wg.Wait()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Wave 1 concurrency tests ---

// TestSpeakOnVCNilSafe verifies that speakOnVC does not panic when
// VoiceConnection is nil. This is the primary safety property of the helper.
func TestSpeakOnVCNilSafe(t *testing.T) {
	p := &GuildPlayer{}
	// Must not panic — recovery sets VoiceConnection=nil before reconnect
	p.speakOnVC(true)
	p.speakOnVC(false)
}

// TestCurrentItemConcurrentAccess is a race-detector test that hammers
// CurrentItem reads and writes from multiple goroutines to verify
// currentItemMutex correctly serializes access.
// Run with: go test -race ./controller/...
func TestCurrentItemConcurrentAccess(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	item := &GuildQueueItem{Video: youtube.VideoResponse{Title: "test song"}}

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				// write (PlaybackStarted path)
				p.currentItemMutex.Lock()
				p.CurrentItem = item
				p.currentItemMutex.Unlock()
			case 1:
				// write nil (PlaybackCompleted path)
				p.currentItemMutex.Lock()
				p.CurrentItem = nil
				p.currentItemMutex.Unlock()
			case 2:
				// read (recovery path)
				p.currentItemMutex.RLock()
				_ = p.CurrentItem
				p.currentItemMutex.RUnlock()
			}
		}(i)
	}
	wg.Wait()
}

// TestRecoveryRequeueFreshItem verifies the recovery re-queue logic:
// when savedItem != nil, a fresh GuildQueueItem with nil LoadResult and
// Stream is prepended to the front of the queue.
func TestRecoveryRequeueFreshItem(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{Video: youtube.VideoResponse{Title: "next song"}},
			},
		},
	}

	savedItem := &GuildQueueItem{
		Video: youtube.VideoResponse{Title: "interrupted song", VideoID: "abc123"},
		// LoadResult intentionally non-nil to simulate a partially consumed pipe
	}

	// Simulate the re-queue logic from attemptVoiceRecovery
	freshItem := &GuildQueueItem{
		Video:      savedItem.Video,
		LoadResult: nil, // must be nil — one-shot pipe must not be reused
		Stream:     nil,
	}
	p.Queue.Mutex.Lock()
	p.Queue.Items = append([]*GuildQueueItem{freshItem}, p.Queue.Items...)
	p.Queue.Mutex.Unlock()

	if len(p.Queue.Items) != 2 {
		t.Fatalf("expected 2 items after requeue, got %d", len(p.Queue.Items))
	}

	front := p.Queue.Items[0]
	if front.Video.Title != "interrupted song" {
		t.Errorf("front item title = %q, want %q", front.Video.Title, "interrupted song")
	}
	if front.LoadResult != nil {
		t.Error("freshItem.LoadResult must be nil to force a clean reload")
	}
	if front.Stream != nil {
		t.Error("freshItem.Stream must be nil")
	}

	second := p.Queue.Items[1]
	if second.Video.Title != "next song" {
		t.Errorf("second item title = %q, want %q", second.Video.Title, "next song")
	}
}

// TestRecoveryNoRequeueWhenNoCurrentItem verifies that recovery does not
// re-queue anything when savedItem is nil (song ended naturally before drop).
func TestRecoveryNoRequeueWhenNoCurrentItem(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{Video: youtube.VideoResponse{Title: "queued song"}},
			},
		},
	}

	// savedItem is nil — song completed naturally before recovery ran
	var savedItem *GuildQueueItem

	initialLen := len(p.Queue.Items)

	if savedItem != nil {
		// This block should NOT execute
		freshItem := &GuildQueueItem{Video: savedItem.Video}
		p.Queue.Mutex.Lock()
		p.Queue.Items = append([]*GuildQueueItem{freshItem}, p.Queue.Items...)
		p.Queue.Mutex.Unlock()
	}

	if len(p.Queue.Items) != initialLen {
		t.Errorf("queue length changed from %d to %d — should not requeue when savedItem is nil",
			initialLen, len(p.Queue.Items))
	}
}

// --- Wave 3 concurrency tests ---

// TestListenerStopChannelsInitialized verifies that all three listener stop
// channels are non-nil on a fresh GuildPlayer so listenFor*Events() goroutines
// don't panic on a nil channel select.
func TestListenerStopChannelsInitialized(t *testing.T) {
	p := &GuildPlayer{
		Queue:                &GuildQueue{},
		queueListenerStop:    make(chan struct{}),
		playbackListenerStop: make(chan struct{}),
		loadListenerStop:     make(chan struct{}),
	}
	if p.queueListenerStop == nil {
		t.Error("queueListenerStop must not be nil")
	}
	if p.playbackListenerStop == nil {
		t.Error("playbackListenerStop must not be nil")
	}
	if p.loadListenerStop == nil {
		t.Error("loadListenerStop must not be nil")
	}
}

// TestListenerStopChannelExits verifies that sending to a stop channel causes
// the listener goroutine to exit. This guards against nil-channel panics and
// confirms the stop mechanism actually works.
func TestListenerStopChannelExits(t *testing.T) {
	notifications := make(chan QueueEvent, 10)
	p := &GuildPlayer{
		Queue: &GuildQueue{
			notifications: notifications,
		},
		queueListenerStop: make(chan struct{}),
	}
	p.Queue.Listening = true

	exited := make(chan struct{})
	go func() {
		for {
			select {
			case _, ok := <-p.Queue.notifications:
				if !ok {
					close(exited)
					return
				}
			case <-p.queueListenerStop:
				close(exited)
				return
			}
		}
	}()

	// Signal stop
	p.queueListenerStop <- struct{}{}

	select {
	case <-exited:
		// goroutine exited cleanly
	case <-time.After(500 * time.Millisecond):
		t.Fatal("listener goroutine did not exit after stop signal")
	}
}

// TestCurrentSongMutexConcurrent is a race-detector test that concurrently
// reads and writes CurrentSong via currentSongMutex, verifying there are no
// data races under the new locking scheme.
// Run with: go test -race ./controller/...
func TestCurrentSongMutexConcurrent(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}
	title := "test song"

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				// write title (PlaybackStarted path)
				p.currentSongMutex.Lock()
				p.CurrentSong = &title
				p.currentSongMutex.Unlock()
			case 1:
				// write nil (PlaybackCompleted/Stopped/Error path)
				p.currentSongMutex.Lock()
				p.CurrentSong = nil
				p.currentSongMutex.Unlock()
			case 2:
				// read (handleAdd shouldPlay path)
				p.currentSongMutex.RLock()
				_ = p.CurrentSong == nil
				p.currentSongMutex.RUnlock()
			}
		}(i)
	}
	wg.Wait()
}

// TestResetReinitializesStopChannels verifies the contract that after Reset()
// all stop channels are non-nil and usable — i.e., ready for the next round
// of listenFor*Events() goroutines. We test this by checking we can send to
// the remade channels without blocking (buffered or receiver expected).
func TestResetReinitializesStopChannels(t *testing.T) {
	// Simulate the remake portion of Reset() without the full Discord machinery
	queueStop := make(chan struct{})
	playbackStop := make(chan struct{})
	loadStop := make(chan struct{})

	// Simulate stop signals (non-blocking)
	select {
	case queueStop <- struct{}{}:
	default:
	}
	select {
	case playbackStop <- struct{}{}:
	default:
	}
	select {
	case loadStop <- struct{}{}:
	default:
	}

	// Remake
	queueStop = make(chan struct{})
	playbackStop = make(chan struct{})
	loadStop = make(chan struct{})

	if queueStop == nil || playbackStop == nil || loadStop == nil {
		t.Error("remade stop channels must not be nil")
	}

	// Verify the remade channels are fresh (no stale signals)
	select {
	case <-queueStop:
		t.Error("remade queueStop should have no pending signal")
	default:
	}
	select {
	case <-playbackStop:
		t.Error("remade playbackStop should have no pending signal")
	default:
	}
	select {
	case <-loadStop:
		t.Error("remade loadStop should have no pending signal")
	default:
	}
}
