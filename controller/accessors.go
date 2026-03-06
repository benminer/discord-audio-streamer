package controller

// accessors.go — Thread-safe getter/setter methods for GuildPlayer fields that are
// accessed from multiple goroutines. All external callers (handlers, etc.) must use
// these instead of reading struct fields directly to avoid data races.

// --- CurrentSong ---

// GetCurrentSong returns a copy of the current song title pointer under the read lock.
// Callers should capture the returned pointer in a local variable and nil-check + dereference
// in one step to avoid TOCTOU races:
//
//	if song := player.GetCurrentSong(); song != nil { ... use *song ... }
func (p *GuildPlayer) GetCurrentSong() *string {
	p.currentSongMutex.RLock()
	defer p.currentSongMutex.RUnlock()
	return p.CurrentSong
}

// --- CurrentItem ---

// GetCurrentItem returns a copy of the current queue item pointer under the read lock.
func (p *GuildPlayer) GetCurrentItem() *GuildQueueItem {
	p.currentItemMutex.RLock()
	defer p.currentItemMutex.RUnlock()
	return p.CurrentItem
}

// --- VoiceConnection / VoiceChannelID ---

// GetVoiceChannelID returns the current voice channel ID under the read lock.
func (p *GuildPlayer) GetVoiceChannelID() *string {
	p.VoiceChannelMutex.RLock()
	defer p.VoiceChannelMutex.RUnlock()
	return p.VoiceChannelID
}

// GetVoiceConnection returns the current voice connection under the read lock.
func (p *GuildPlayer) GetVoiceConnection() interface{} {
	p.VoiceChannelMutex.RLock()
	defer p.VoiceChannelMutex.RUnlock()
	return p.VoiceConnection
}

// --- LastTextChannelID ---

// GetLastTextChannelID returns the last text channel ID under the read lock.
func (p *GuildPlayer) GetLastTextChannelID() string {
	p.lastTextChannelMu.RLock()
	defer p.lastTextChannelMu.RUnlock()
	return p.LastTextChannelID
}

// SetLastTextChannelID sets the last text channel ID under the write lock.
func (p *GuildPlayer) SetLastTextChannelID(id string) {
	p.lastTextChannelMu.Lock()
	defer p.lastTextChannelMu.Unlock()
	p.LastTextChannelID = id
}

// --- Queue snapshot ---

// GetQueueSnapshot returns a copy of the current queue items slice under the queue mutex.
// The returned slice is safe to iterate without holding any lock — it's a snapshot.
func (p *GuildPlayer) GetQueueSnapshot() []*GuildQueueItem {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	if len(p.Queue.Items) == 0 {
		return nil
	}
	snap := make([]*GuildQueueItem, len(p.Queue.Items))
	copy(snap, p.Queue.Items)
	return snap
}

// --- ShouldJoinVoice ---

// ShouldJoinVoice returns true if the bot needs to join (or move to) the given voice channel.
// This encapsulates the VoiceChannelMutex read so callers don't access the fields directly.
func (p *GuildPlayer) ShouldJoinVoice(requesterChannelID string) bool {
	p.VoiceChannelMutex.RLock()
	vc := p.VoiceConnection
	vcID := p.VoiceChannelID
	p.VoiceChannelMutex.RUnlock()

	if vc == nil || vcID == nil {
		return true
	}
	// Move to the requester's channel if we're stopped and in a different one.
	return p.IsEmpty() && !p.Player.IsPlaying() && *vcID != requesterChannelID
}
