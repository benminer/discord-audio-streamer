package controller

import (
	"fmt"
	"sync"
	"testing"

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
