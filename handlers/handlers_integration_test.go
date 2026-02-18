package handlers

import (
	"testing"
	"time"
)

func TestHintsCooldownBehavior(t *testing.T) {
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  1.0, // 100% chance for testing
		hints:       []string{"Test hint"},
	}

	guildID := "test-guild"

	// First call should show hint
	hint1 := hints.ShowIfApplicable(guildID)
	if hint1 == "" {
		t.Error("Expected hint on first call")
	}

	// Immediate second call should not show hint (cooldown)
	hint2 := hints.ShowIfApplicable(guildID)
	if hint2 != "" {
		t.Error("Expected no hint due to cooldown")
	}

	// Clear cooldown and test again
	hints.ClearCooldown(guildID)
	hint3 := hints.ShowIfApplicable(guildID)
	if hint3 == "" {
		t.Error("Expected hint after cooldown clear")
	}
}

func TestHintsIntegrationWithManager(t *testing.T) {
	// Test that Manager properly initializes Hints
	// This is a simple smoke test since full integration testing
	// requires complex mocking of Discord interactions and database

	// We can't easily test full handlers without mocking the entire stack,
	// but we can verify the Manager creates Hints properly
	hints := NewHints()
	if hints == nil {
		t.Error("NewHints should return a valid instance")
	}

	// Test that hints are properly integrated
	guildID := "integration-test-guild"

	// With 100% chance, should show hint
	hints.hintChance = 1.0
	hint := hints.ShowIfApplicable(guildID)
	if hint == "" {
		t.Error("Expected hint with 100% chance")
	}

	// Should contain the emoji and pro tip format
	if len(hint) < 10 || !contains(hint, "💡") {
		t.Errorf("Hint should be properly formatted, got: %s", hint)
	}
}

func TestHintsProbability(t *testing.T) {
	hints := &Hints{
		cooldowns:   make(map[string]time.Time),
		cooldownDur: 5 * time.Minute,
		hintChance:  0.0, // 0% chance
		hints:       []string{"Test hint"},
	}

	guildID := "prob-test-guild"

	// With 0% chance, should never show hint
	for i := 0; i < 10; i++ {
		hint := hints.ShowIfApplicable(guildID)
		if hint != "" {
			t.Errorf("Expected no hint with 0%% chance, got: %s", hint)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
