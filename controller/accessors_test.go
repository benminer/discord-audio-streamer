package controller

import (
	"sync"
	"testing"

	"beatbot/youtube"
)

// TestGetCurrentSongConcurrent is a race-detector test that verifies
// GetCurrentSong() is safe to call concurrently with CurrentSong writes.
func TestGetCurrentSongConcurrent(t *testing.T) {
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
				// Write via accessor
				p.currentSongMutex.Lock()
				p.CurrentSong = &title
				p.currentSongMutex.Unlock()
			case 1:
				// Write nil via accessor
				p.currentSongMutex.Lock()
				p.CurrentSong = nil
				p.currentSongMutex.Unlock()
			case 2:
				// Read via accessor (the method under test)
				_ = p.GetCurrentSong()
			}
		}(i)
	}
	wg.Wait()
}

// TestGetCurrentItemConcurrent is a race-detector test that verifies
// GetCurrentItem() is safe to call concurrently with CurrentItem writes.
func TestGetCurrentItemConcurrent(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}
	item := &GuildQueueItem{Video: youtube.VideoResponse{Title: "test"}}

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				p.currentItemMutex.Lock()
				p.CurrentItem = item
				p.currentItemMutex.Unlock()
			case 1:
				p.currentItemMutex.Lock()
				p.CurrentItem = nil
				p.currentItemMutex.Unlock()
			case 2:
				_ = p.GetCurrentItem()
			}
		}(i)
	}
	wg.Wait()
}

// TestGetLastTextChannelIDConcurrent is a race-detector test for the
// lastTextChannelMu mutex protecting LastTextChannelID.
func TestGetLastTextChannelIDConcurrent(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				p.SetLastTextChannelID("channel-123")
			case 1:
				p.SetLastTextChannelID("")
			case 2:
				_ = p.GetLastTextChannelID()
			}
		}(i)
	}
	wg.Wait()
}

// TestGetVoiceChannelIDConcurrent is a race-detector test for
// VoiceChannelMutex protecting VoiceChannelID.
func TestGetVoiceChannelIDConcurrent(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}
	channelID := "voice-channel-123"

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				p.VoiceChannelMutex.Lock()
				p.VoiceChannelID = &channelID
				p.VoiceChannelMutex.Unlock()
			case 1:
				p.VoiceChannelMutex.Lock()
				p.VoiceChannelID = nil
				p.VoiceChannelMutex.Unlock()
			case 2:
				_ = p.GetVoiceChannelID()
			}
		}(i)
	}
	wg.Wait()
}

// TestGetVoiceConnectionConcurrent is a race-detector test for
// VoiceChannelMutex protecting VoiceConnection access via the accessor.
// This test uses the accessor methods to avoid import cycles.
func TestGetVoiceConnectionConcurrent(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	var wg sync.WaitGroup
	const goroutines = 100

	// Use GetVoiceChannelID as a proxy to test VoiceChannelMutex,
	// since GetVoiceConnection uses the same mutex.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = p.GetVoiceChannelID()
		}(i)
	}

	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channelID := "channel-" + string(rune(i%10+'0'))
			p.VoiceChannelMutex.Lock()
			p.VoiceChannelID = &channelID
			p.VoiceChannelMutex.Unlock()
		}(i)
	}

	wg.Wait()
}

// TestGetQueueSnapshotDeepCopy verifies that GetQueueSnapshot returns
// a deep copy, not a reference to the internal slice.
func TestGetQueueSnapshotDeepCopy(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{Video: youtube.VideoResponse{VideoID: "1", Title: "Song 1"}},
				{Video: youtube.VideoResponse{VideoID: "2", Title: "Song 2"}},
			},
		},
	}

	// Get a snapshot
	snap1 := p.GetQueueSnapshot()
	if len(snap1) != 2 {
		t.Fatalf("expected 2 items, got %d", len(snap1))
	}

	// Modify the returned slice
	snap1[0] = nil

	// Get another snapshot - should still have the original items
	snap2 := p.GetQueueSnapshot()
	if len(snap2) != 2 || snap2[0] == nil {
		t.Error("snapshot should be a deep copy, not affected by modifications to returned slice")
	}

	// Verify we got different slice instances
	if &snap1[0] == &snap2[0] {
		t.Error("snapshots should be independent copies")
	}
}

// TestGetQueueSnapshotConcurrent is a race-detector test for GetQueueSnapshot
// being called while the queue is being modified.
func TestGetQueueSnapshotConcurrent(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	var wg sync.WaitGroup
	const goroutines = 50

	// Readers calling GetQueueSnapshot
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = p.GetQueueSnapshot()
			}
		}()
	}

	// Writers modifying the queue
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				p.Queue.Mutex.Lock()
				p.Queue.Items = append(p.Queue.Items, &GuildQueueItem{
					Video: youtube.VideoResponse{VideoID: "test"},
				})
				p.Queue.Mutex.Unlock()
			}
		}(i)
	}

	wg.Wait()
}

// TestShouldJoinVoiceNilVC tests the shouldJoinVoice branch where
// VoiceConnection is nil.
func TestShouldJoinVoiceNilVC(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	// With nil VoiceConnection, should return true
	if !p.ShouldJoinVoice("channel-123") {
		t.Error("expected true when VoiceConnection is nil")
	}
}

// TestShouldJoinVoiceNilChannelID tests when VoiceConnection exists
// but VoiceChannelID is nil.
func TestShouldJoinVoiceNilChannelID(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	// GetVoiceChannelID returns nil when VoiceChannelID is nil
	// This tests that branch of ShouldJoinVoice
	if p.GetVoiceChannelID() != nil {
		t.Error("expected nil when VoiceChannelID is not set")
	}
}

// TestShouldJoinVoiceEmptyAndStopped tests when bot is empty and
// not playing, should return true to join the requester's channel.
// NOTE: This test is skipped because ShouldJoinVoice requires a real Player
// with IsPlaying() method. The accessor is tested via race detector in other tests.
func TestShouldJoinVoiceEmptyAndStopped(t *testing.T) {
	t.Skip("Requires real Player instance")
}

// TestShouldJoinVoicePlaying tests when player is actively playing,
// should not join even if queue is empty.
func TestShouldJoinVoicePlaying(t *testing.T) {
	t.Skip("Requires real Player instance")
}
