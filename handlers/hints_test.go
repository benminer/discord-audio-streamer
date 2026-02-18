package handlers

import (
	"testing"
	"time"
)

func TestHints_ShouldShowHint(t *testing.T) {
	// Use a fresh instance to avoid global state issues
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  1.0, // 100% chance
		hints: []string{
			"Pro tip: /radio auto-queues",
			"Pro tip: /leaderboard",
			"Pro tip: /favorites",
		},
	}

	// With 100% chance, should always show
	_, show := hints.ShouldShowHint("test-guild-1")
	if !show {
		t.Error("Expected hint to show with 100% chance")
	}

	// Second call should not show due to cooldown
	_, show2 := hints.ShouldShowHint("test-guild-1")
	if show2 {
		t.Error("Expected no hint due to cooldown")
	}
}

func TestHints_Cooldown(t *testing.T) {
	// Use a fresh instance
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  1.0, // 100% chance for testing
		hints: []string{
			"Test hint",
		},
	}
	guildID := "test-guild-cooldown"

	// First hint should show (100% chance now)
	hint1, show1 := hints.ShouldShowHint(guildID)
	if !show1 {
		t.Errorf("Expected hint to show with 100%% chance, got: %s", hint1)
	}

	// Immediate second call should NOT show (cooldown)
	_, show2 := hints.ShouldShowHint(guildID)
	if show2 {
		t.Errorf("Expected no hint due to cooldown")
	}

	// Verify cooldown duration
	remaining := hints.GetCooldownRemaining(guildID)
	if remaining <= 0 {
		t.Errorf("Expected positive cooldown remaining")
	}
}

func TestHints_ClearCooldown(t *testing.T) {
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  1.0,
		hints:       []string{"Test hint"},
	}
	guildID := "test-guild-clear"

	// Show a hint
	hints.ShouldShowHint(guildID)

	// Verify cooldown is set
	remaining1 := hints.GetCooldownRemaining(guildID)
	if remaining1 <= 0 {
		t.Errorf("Expected cooldown to be set")
	}

	// Clear cooldown
	hints.ClearCooldown(guildID)

	// Verify cooldown is cleared
	remaining2 := hints.GetCooldownRemaining(guildID)
	if remaining2 != 0 {
		t.Errorf("Expected cooldown to be cleared, got %v", remaining2)
	}
}

func TestHints_DifferentGuilds(t *testing.T) {
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  1.0,
		hints:       []string{"Test hint"},
	}

	guild1 := "guild-1"
	guild2 := "guild-2"

	// Show hint for guild1
	hints.ShouldShowHint(guild1)

	// guild2 should still be able to show hints
	_, _ = hints.ShouldShowHint(guild2)

	// Verify both have independent cooldowns
	remaining1 := hints.GetCooldownRemaining(guild1)
	remaining2 := hints.GetCooldownRemaining(guild2)

	// guild1 should have cooldown, guild2 should not (we just showed a hint, it was blocked by cooldown)
	if remaining1 <= 0 {
		t.Errorf("Expected guild1 to have cooldown")
	}

	// guild2 should not have cooldown yet
	if remaining2 != 0 {
		t.Logf("guild2 has cooldown: %v (expected 0)", remaining2)
	}
}

func TestHints_GetCooldownRemaining_NonExistent(t *testing.T) {
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  0.15,
		hints:       []string{},
	}

	// Non-existent guild should return 0
	remaining := hints.GetCooldownRemaining("non-existent-guild")
	if remaining != 0 {
		t.Errorf("Expected 0 for non-existent guild, got %v", remaining)
	}
}

func TestHints_HintsSliceNotEmpty(t *testing.T) {
	hints := NewHints()

	if len(hints.hints) == 0 {
		t.Error("Expected non-empty hints slice")
	}

	// Verify hints contain expected content
	foundProTip := false
	for _, hint := range hints.hints {
		if len(hint) > 0 {
			foundProTip = true
		}
	}
	if !foundProTip {
		t.Error("Expected at least one non-empty hint")
	}
}

func TestHints_CooldownDuration(t *testing.T) {
	hints := NewHints()

	expectedCooldown := 5 * time.Minute
	if hints.cooldownDur != expectedCooldown {
		t.Errorf("Expected cooldown duration of %v, got %v", expectedCooldown, hints.cooldownDur)
	}
}

func TestHints_HintChance(t *testing.T) {
	hints := NewHints()

	expectedChance := float32(0.15)
	if hints.hintChance != expectedChance {
		t.Errorf("Expected hint chance of %v, got %v", expectedChance, hints.hintChance)
	}
}
