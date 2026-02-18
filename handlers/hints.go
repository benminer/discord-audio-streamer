package handlers

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Hints provides random tips to users after successful command execution
type Hints struct {
	cooldowns   map[string]time.Time // guildID -> last hint time
	cooldownMu  sync.RWMutex
	cooldownDur time.Duration
	hintChance  float32
	hints       []string
}

// NewHints creates a new Hints manager with guild-specific cooldowns
func NewHints() *Hints {
	// Seed random for hints (only once per process)
	rand.Seed(time.Now().UnixNano())
	return &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  0.15, // 15% chance
		hints: []string{
			"Pro tip: /radio auto-queues similar songs when the queue is empty",
			"Pro tip: /leaderboard shows the most played songs in this server",
			"Pro tip: /favorites lets you save songs for later",
			"Pro tip: /shuffle randomizes the current queue",
			"Pro tip: /loop repeats the current song",
			"Pro tip: /history shows recently played songs",
			"Pro tip: /recommend lets the AI pick a song based on your taste",
			"Pro tip: /volume adjusts the playback volume (0-150)",
			"Pro tip: /lyrics shows lyrics for the currently playing song",
			"Pro tip: /favorite saves the current song to your favorites",
		},
	}
}

// ShouldShowHint checks if a hint should be shown for this guild
// Returns the hint string and true if a hint should be displayed
func (h *Hints) ShouldShowHint(guildID string) (string, bool) {
	// Check if we should even try to show a hint (15% chance)
	if rand.Float32() > h.hintChance {
		return "", false
	}

	// Check guild cooldown
	h.cooldownMu.RLock()
	lastHint, hasCooldown := h.cooldowns[guildID]
	h.cooldownMu.RUnlock()

	if hasCooldown {
		if time.Since(lastHint) < h.cooldownDur {
			return "", false
		}
	}

	// Select a random hint
	hint := h.hints[rand.Intn(len(h.hints))]

	// Update cooldown
	h.cooldownMu.Lock()
	h.cooldowns[guildID] = time.Now()
	h.cooldownMu.Unlock()

	log.Debugf("Showing hint for guild %s: %s", guildID, hint)
	return hint, true
}

// ClearCooldown removes the cooldown for a guild (useful for testing)
func (h *Hints) ClearCooldown(guildID string) {
	h.cooldownMu.Lock()
	delete(h.cooldowns, guildID)
	h.cooldownMu.Unlock()
}

// GetCooldownRemaining returns remaining cooldown time for a guild
func (h *Hints) GetCooldownRemaining(guildID string) time.Duration {
	h.cooldownMu.RLock()
	defer h.cooldownMu.RUnlock()
	lastHint, exists := h.cooldowns[guildID]
	if !exists {
		return 0
	}
	remaining := h.cooldownDur - time.Since(lastHint)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ShowIfApplicable checks if a hint should be shown and returns it with formatting
func (h *Hints) ShowIfApplicable(guildID string) string {
	hint, show := h.ShouldShowHint(guildID)
	if show {
		return fmt.Sprintf("\n\n💡 %s", hint)
	}
	return ""
}
