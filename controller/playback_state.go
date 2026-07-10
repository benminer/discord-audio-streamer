package controller

import (
	"sync"

	"beatbot/audio"

	log "github.com/sirupsen/logrus"
)

// SongInfo holds metadata about a song for playback state tracking.
// Used as the value type inside PlaybackState — always copied on read.
type SongInfo struct {
	Title       string
	VideoID     string
	IsRadioPick bool
	QueuedBy    string // display name of user who queued, empty for radio picks
}

// PlaybackState is the authoritative single-source-of-truth for what is
// currently playing, what is coming next, and any pre-generated TTS buffer.
// All fields are protected by a single RWMutex — no more scattered mutexes.
// The TTS watcher goroutine reads from this struct; queue mutations and
// playback events write to it.
type PlaybackState struct {
	mu          sync.RWMutex
	current     *SongInfo
	next        *SongInfo
	ttsBuffer   *audio.TTSPlayback
	gen         int64
	regenNotify chan struct{}
}

func newPlaybackState() *PlaybackState {
	return &PlaybackState{
		regenNotify: make(chan struct{}, 1),
	}
}

// --- Current song ---

func (ps *PlaybackState) SetCurrent(info SongInfo) {
	ps.mu.Lock()
	ps.current = &info
	ps.mu.Unlock()
}

func (ps *PlaybackState) ClearCurrent() {
	ps.mu.Lock()
	ps.current = nil
	ps.mu.Unlock()
}

func (ps *PlaybackState) Current() *SongInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.current == nil {
		return nil
	}
	c := *ps.current
	return &c
}

// --- Next song ---

func (ps *PlaybackState) SetNext(info *SongInfo) {
	ps.mu.Lock()
	ps.next = info
	ps.ttsBuffer = nil
	ps.mu.Unlock()
	if info != nil {
		log.Debugf("[playback-state] SetNext: %s (ttsBuffer cleared, regen signaled)", info.Title)
	}
	ps.SignalRegen()
}

func (ps *PlaybackState) ClearNext() {
	ps.mu.Lock()
	ps.next = nil
	ps.ttsBuffer = nil
	ps.gen++
	ps.mu.Unlock()
	log.Debug("[playback-state] ClearNext: cleared next + TTS buffer")
	ps.SignalRegen()
}

func (ps *PlaybackState) Next() *SongInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.next == nil {
		return nil
	}
	n := *ps.next
	return &n
}

// --- TTS buffer ---

// HasTTS returns whether a TTS buffer is available without consuming it.
func (ps *PlaybackState) HasTTS() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.ttsBuffer != nil
}

// ConsumeTTS atomically reads and clears the TTS buffer.
// Used by Player at the end-of-song transition point.
func (ps *PlaybackState) ConsumeTTS() *audio.TTSPlayback {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	tts := ps.ttsBuffer
	ps.ttsBuffer = nil
	if tts != nil {
		log.Debug("[playback-state] ConsumeTTS: buffer consumed")
	}
	return tts
}

// StartRegen increments the generation counter and returns the new value.
// Call before starting TTS generation; pass the returned gen to SetTTSBuffer.
func (ps *PlaybackState) StartRegen() int64 {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.gen++
	return ps.gen
}

// SetTTSBuffer stores the TTS buffer only if gen matches the current generation
// counter. Returns true if the buffer was stored, false if stale.
func (ps *PlaybackState) SetTTSBuffer(gen int64, tts *audio.TTSPlayback) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if gen != ps.gen {
		return false
	}
	ps.ttsBuffer = tts
	return true
}

// SignalRegen sends a non-blocking notification to the TTS watcher goroutine.
func (ps *PlaybackState) SignalRegen() {
	select {
	case ps.regenNotify <- struct{}{}:
	default:
	}
}
