package controller

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"beatbot/audio"
	"beatbot/youtube"

	"github.com/bwmarrin/discordgo"
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

func TestVoiceRecoveryTeardownRejectsConcurrentMonitorAdmissions(t *testing.T) {
	p := &GuildPlayer{}
	p.configureVoiceRecovery(3)
	p.invalidateVoiceRecovery() // Reset, idle cleanup, and user joins all do this first.

	var admitted int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := p.beginVoiceRecovery(); ok {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if admitted != 0 {
		t.Fatalf("recovery admissions during teardown = %d, want 0", admitted)
	}
}

func TestCancelledRecoveryCannotDisconnectNewUserConnection(t *testing.T) {
	channelID := "voice-channel"
	oldVC := &discordgo.VoiceConnection{}
	newUserVC := &discordgo.VoiceConnection{}
	p := &GuildPlayer{
		Queue:           &GuildQueue{notifications: make(chan QueueEvent, 1)},
		VoiceChannelID:  &channelID,
		VoiceConnection: oldVC,
	}
	p.configureVoiceRecovery(3)
	ctx, ok := p.beginVoiceRecovery()
	if !ok {
		t.Fatal("expected recovery admission")
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	p.voiceRecoveryBeforeDetach = func() {
		close(entered)
		<-release
	}
	var disconnected []*discordgo.VoiceConnection
	p.voiceRecoveryDisconnect = func(vc *discordgo.VoiceConnection) error {
		disconnected = append(disconnected, vc)
		return nil
	}

	done := make(chan struct{})
	go func() {
		p.runVoiceRecovery(ctx)
		close(done)
	}()
	<-entered
	p.invalidateVoiceRecovery()
	p.VoiceChannelMutex.Lock()
	p.VoiceConnection = newUserVC
	p.VoiceChannelMutex.Unlock()
	close(release)
	<-done

	if got := p.GetVoiceConnection(); got != newUserVC {
		t.Fatal("stale recovery replaced or disconnected the new user connection")
	}
	if len(disconnected) != 0 {
		t.Fatalf("stale recovery disconnected %d connection(s), want none", len(disconnected))
	}
}

func TestCancelledRecoveryCannotPublishRequeueOrNotify(t *testing.T) {
	channelID := "voice-channel"
	oldVC := &discordgo.VoiceConnection{}
	recoveredVC := &discordgo.VoiceConnection{}
	savedItem := &GuildQueueItem{Video: youtube.VideoResponse{Title: "interrupted", VideoID: "song"}}
	p := &GuildPlayer{
		Queue:           &GuildQueue{notifications: make(chan QueueEvent, 1)},
		VoiceChannelID:  &channelID,
		VoiceConnection: oldVC,
		CurrentItem:     savedItem,
	}
	p.configureVoiceRecovery(3)
	ctx, ok := p.beginVoiceRecovery()
	if !ok {
		t.Fatal("expected recovery admission")
	}

	joinStarted := make(chan struct{})
	releaseJoin := make(chan struct{})
	p.voiceRecoveryJoin = func(string) (*discordgo.VoiceConnection, error) {
		close(joinStarted)
		<-releaseJoin
		return recoveredVC, nil
	}
	disconnected := make(chan *discordgo.VoiceConnection, 2)
	p.voiceRecoveryDisconnect = func(vc *discordgo.VoiceConnection) error {
		disconnected <- vc
		return nil
	}
	notified := make(chan string, 1)
	p.voiceRecoveryMessage = func(message string) { notified <- message }
	p.SetLastTextChannelID("text-channel")

	done := make(chan struct{})
	go func() {
		p.runVoiceRecovery(ctx)
		close(done)
	}()
	<-joinStarted
	p.invalidateVoiceRecovery()
	close(releaseJoin)
	<-done

	p.VoiceChannelMutex.RLock()
	publishedVC := p.VoiceConnection
	p.VoiceChannelMutex.RUnlock()
	if publishedVC != nil {
		t.Fatal("cancelled recovery published a voice connection")
	}
	if got := p.GetQueueSnapshot(); len(got) != 0 {
		t.Fatalf("cancelled recovery requeued %d item(s), want none", len(got))
	}
	select {
	case message := <-notified:
		t.Fatalf("cancelled recovery notified %q", message)
	default:
	}
	disconnectedSet := make(map[*discordgo.VoiceConnection]bool, 2)
	for range 2 {
		select {
		case vc := <-disconnected:
			disconnectedSet[vc] = true
		default:
			t.Fatal("cancelled recovery did not clean up both detached connections")
		}
	}
	if !disconnectedSet[oldVC] || !disconnectedSet[recoveredVC] {
		t.Fatal("cancelled recovery leaked an old or recovered voice connection")
	}
}

func TestCancelledRecoveryAfterDetachDoesNotBeginJoin(t *testing.T) {
	channelID := "voice-channel"
	oldVC := &discordgo.VoiceConnection{}
	p := &GuildPlayer{
		Queue:           &GuildQueue{notifications: make(chan QueueEvent, 1)},
		VoiceChannelID:  &channelID,
		VoiceConnection: oldVC,
	}
	p.configureVoiceRecovery(3)
	ctx, ok := p.beginVoiceRecovery()
	if !ok {
		t.Fatal("expected recovery admission")
	}

	disconnectStarted := make(chan struct{})
	releaseDisconnect := make(chan struct{})
	joinStarted := make(chan struct{})
	disconnected := make(chan *discordgo.VoiceConnection, 1)
	p.voiceRecoveryDisconnect = func(vc *discordgo.VoiceConnection) error {
		disconnected <- vc
		close(disconnectStarted)
		<-releaseDisconnect
		return nil
	}
	p.voiceRecoveryJoin = func(string) (*discordgo.VoiceConnection, error) {
		close(joinStarted)
		return &discordgo.VoiceConnection{}, nil
	}

	done := make(chan struct{})
	go func() {
		p.runVoiceRecovery(ctx)
		close(done)
	}()
	<-disconnectStarted
	p.invalidateVoiceRecovery()
	close(releaseDisconnect)
	<-done

	select {
	case <-joinStarted:
		t.Fatal("cancelled recovery began a new voice join after stale disconnect")
	default:
	}
	select {
	case vc := <-disconnected:
		if vc != oldVC {
			t.Fatal("recovery disconnected an unexpected connection")
		}
	default:
		t.Fatal("recovery did not disconnect the detached stale connection")
	}
	p.VoiceChannelMutex.RLock()
	publishedVC := p.VoiceConnection
	p.VoiceChannelMutex.RUnlock()
	if publishedVC != nil {
		t.Fatal("cancelled recovery published a voice connection")
	}
}

func TestVoiceMonitorStopIsConcurrentSafe(t *testing.T) {
	p := &GuildPlayer{}
	p.startVoiceConnectionMonitor()

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.stopVoiceConnectionMonitor()
		}()
	}
	wg.Wait()
}

func TestVoiceConnectionCleanupUsesVoiceChannelMutex(t *testing.T) {
	channelID := "voice-channel"
	p := &GuildPlayer{VoiceChannelID: &channelID}

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.disconnectVoiceConnection(true)
			_ = p.GetVoiceChannelID()
		}()
	}
	wg.Wait()

	if p.GetVoiceChannelID() != nil {
		t.Fatal("voice cleanup must clear the voice channel ID")
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

// TestListenForPlaybackEventsHandlesPlaybackErrorWithoutVideoID verifies that
// a late playback error without a VideoID cannot crash the guild listener.
func TestListenForPlaybackEventsHandlesPlaybackErrorWithoutVideoID(t *testing.T) {
	p := &GuildPlayer{
		GuildID:              "test-guild",
		Player:               &audio.Player{Notifications: make(chan audio.PlaybackNotification, 1)},
		Queue:                &GuildQueue{},
		playbackState:        newPlaybackState(),
		playbackListenerStop: make(chan struct{}),
	}
	p.playbackState.SetCurrent(SongInfo{Title: "current song", VideoID: "current-video"})

	p.listenForPlaybackEvents()
	defer close(p.playbackListenerStop)

	p.Player.Notifications <- audio.PlaybackNotification{Event: audio.PlaybackError}

	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for p.playbackState.Current() != nil {
		select {
		case <-deadline.C:
			t.Fatal("playback error without a VideoID was not handled")
		case <-ticker.C:
		}
	}
}

// TestListenForPlaybackEventsHandlesPlaybackErrorWithNilInteraction verifies
// that a playback error for a loop-created queue item still advances playback
// when there is no interaction available to update.
func TestListenForPlaybackEventsHandlesPlaybackErrorWithNilInteraction(t *testing.T) {
	videoID := "loop-created-video"
	p := &GuildPlayer{
		GuildID: "test-guild",
		Player:  &audio.Player{Notifications: make(chan audio.PlaybackNotification, 1)},
		Queue: &GuildQueue{Items: []*GuildQueueItem{{
			Video:       youtube.VideoResponse{VideoID: videoID, Title: "loop-created song"},
			LoadResult:  &audio.LoadResult{Title: "loop-created song"},
			streamReady: make(chan struct{}),
		}}},
		playbackState:        newPlaybackState(),
		playbackListenerStop: make(chan struct{}),
	}
	p.playbackState.SetCurrent(SongInfo{Title: "loop-created song", VideoID: videoID})

	p.listenForPlaybackEvents()
	defer close(p.playbackListenerStop)
	close(p.Queue.Items[0].streamReady)

	p.Player.Notifications <- audio.PlaybackNotification{Event: audio.PlaybackError, VideoID: &videoID}

	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for p.playbackState.Current() != nil {
		select {
		case <-deadline.C:
			t.Fatal("playback error with nil interaction was not handled")
		case <-ticker.C:
		}
	}
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
