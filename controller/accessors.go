package controller

// accessors.go — Thread-safe getter/setter methods for GuildPlayer fields that are
// accessed from multiple goroutines. All external callers (handlers, etc.) must use
// these instead of reading struct fields directly to avoid data races.

// --- CurrentSong ---

// GetCurrentSong returns the title of the currently playing song, or nil if nothing is playing.
// Delegates to PlaybackState, the single source of truth for playback state.
//
//	if song := player.GetCurrentSong(); song != nil { ... use *song ... }
func (p *GuildPlayer) GetCurrentSong() *string {
	if p.playbackState == nil {
		return nil
	}
	current := p.playbackState.Current()
	if current == nil {
		return nil
	}
	return &current.Title
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

// --- AnnounceEnabled / AnnounceVoice ---

// GetAnnounceEnabled returns a thread-safe copy of AnnounceEnabled.
func (p *GuildPlayer) GetAnnounceEnabled() bool {
	p.announceMu.RLock()
	defer p.announceMu.RUnlock()
	return p.AnnounceEnabled
}

// SetAnnounceEnabled sets AnnounceEnabled under the announce mutex.
func (p *GuildPlayer) SetAnnounceEnabled(v bool) {
	p.announceMu.Lock()
	p.AnnounceEnabled = v
	p.announceMu.Unlock()
}

// GetAnnounceVoice returns a thread-safe copy of AnnounceVoice.
func (p *GuildPlayer) GetAnnounceVoice() string {
	p.announceMu.RLock()
	defer p.announceMu.RUnlock()
	return p.AnnounceVoice
}

// SetAnnounceVoice sets AnnounceVoice under the announce mutex.
func (p *GuildPlayer) SetAnnounceVoice(voice string) {
	p.announceMu.Lock()
	p.AnnounceVoice = voice
	p.announceMu.Unlock()
}

// TriggerTTSRegen signals the TTS watcher to attempt generation for the current transition.
// Safe to call when no next song is set — generateTransitionTTS will no-op.
func (p *GuildPlayer) TriggerTTSRegen() {
	if p.playbackState != nil {
		p.playbackState.SignalRegen()
	}
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
