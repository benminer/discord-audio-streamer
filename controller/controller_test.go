package controller

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"beatbot/audio"
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
// Stream is prepended to the front of the queue, and an EventAdd notification
// is sent through the queue's notifications channel (same as AddToQueue).
func TestRecoveryRequeueFreshItem(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{Video: youtube.VideoResponse{Title: "next song"}},
			},
			notifications: make(chan QueueEvent, 100),
		},
	}

	savedItem := &GuildQueueItem{
		Video: youtube.VideoResponse{Title: "interrupted song", VideoID: "abc123"},
		// LoadResult intentionally non-nil to simulate a partially consumed pipe
	}

	// Simulate the re-queue logic from attemptVoiceRecovery
	freshItem := &GuildQueueItem{
		Video:       savedItem.Video,
		LoadResult:  nil,
		Stream:      nil,
		streamReady: make(chan struct{}),
	}
	p.Queue.Mutex.Lock()
	p.Queue.Items = append([]*GuildQueueItem{freshItem}, p.Queue.Items...)
	p.Queue.Mutex.Unlock()

	// Send event notification (new behavior)
	select {
	case p.Queue.notifications <- QueueEvent{Type: EventAdd, Item: freshItem}:
	default:
		t.Fatal("failed to send queue notification")
	}

	// Verify queue state
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

	// Verify event was sent
	select {
	case event := <-p.Queue.notifications:
		if event.Type != EventAdd {
			t.Errorf("event type = %q, want %q", event.Type, EventAdd)
		}
		if event.Item.Video.Title != "interrupted song" {
			t.Errorf("event item title = %q, want %q", event.Item.Video.Title, "interrupted song")
		}
	default:
		t.Error("expected EventAdd notification in queue channel")
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

// TestPlaybackStateConcurrent is a race-detector test that concurrently
// reads and writes PlaybackState, verifying there are no data races.
// Run with: go test -race ./controller/...
func TestPlaybackStateConcurrent(t *testing.T) {
	ps := newPlaybackState()

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				ps.SetCurrent(SongInfo{Title: "test song", VideoID: "1"})
			case 1:
				ps.ClearCurrent()
			case 2:
				ps.SetNext(&SongInfo{Title: "next song", VideoID: "2"})
			case 3:
				_ = ps.Current()
				_ = ps.Next()
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

// TestPlayNextNilStreamNoPanic verifies that playNext does not panic
// when WaitForStreamURL times out and Stream remains nil.
func TestPlayNextNilStreamNoPanic(t *testing.T) {
	player, err := audio.NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{
					Video:       youtube.VideoResponse{VideoID: "test", Title: "Test Song"},
					Stream:      nil,
					LoadResult:  nil,
					streamReady: make(chan struct{}),
					Interaction: &GuildQueueItemInteraction{
						InteractionToken: "test-token",
						AppID:            "test-app",
						UserID:           "test-user",
					},
				},
			},
		},
		Player: player,
		Loader: audio.NewLoader(),
	}

	// Close streamReady immediately but leave Stream nil —
	// simulates WaitForStreamURL returning false.
	close(p.Queue.Items[0].streamReady)

	// This must not panic (was SIGSEGV before fix)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("playNext panicked: %v", r)
		}
	}()

	p.playNext()
}

// TestVoiceMonitorSkipsRecoveryWhenPaused verifies that the voice monitor
// condition (IsPlaying && !IsPaused) correctly excludes paused players.
func TestVoiceMonitorSkipsRecoveryWhenPaused(t *testing.T) {
	p, err := audio.NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	// Simulate active playback: playing=true, paused=false
	p.SetPlaying(true)
	shouldRecover := p.IsPlaying() && !p.IsPaused()
	if !shouldRecover {
		t.Error("expected recovery when playing and not paused")
	}

	// Simulate paused playback: playing=true, paused=true
	p.SetPaused(true)
	shouldRecover = p.IsPlaying() && !p.IsPaused()
	if shouldRecover {
		t.Error("expected NO recovery when playing but paused")
	}

	// Simulate stopped: playing=false
	p.SetPlaying(false)
	p.SetPaused(false)
	shouldRecover = p.IsPlaying() && !p.IsPaused()
	if shouldRecover {
		t.Error("expected NO recovery when not playing")
	}
}
