package helpers

import (
	"testing"
)

func TestDJFallbacks(t *testing.T) {
	// Test that all expected commands have fallbacks
	expectedCommands := []string{
		"clear",
		"skip",
		"remove",
		"shuffle",
		"play",
		"queue",
		"pause",
		"resume",
		"volume",
		"radio",
		"loop",
		"history",
		"leaderboard",
		"favorites",
		"favorite",
		"unfavorite",
		"recommend",
		"reset",
		"view",
		"lyrics",
		"stop",
	}

	for _, cmd := range expectedCommands {
		if _, ok := DJFallbacks[cmd]; !ok {
			t.Errorf("Missing fallback for command: %s", cmd)
		}
	}
}

func TestGetFallback(t *testing.T) {
	tests := []struct {
		command     string
		expectKnown bool // whether we expect a known fallback
	}{
		{"clear", true},
		{"skip", true},
		{"unknown", false}, // Unknown commands should return generic fallback
		{"", false},        // Empty command should return generic fallback
	}

	for _, tt := range tests {
		result := getFallback(tt.command)
		if tt.expectKnown && result == "" {
			t.Errorf("Expected known fallback for %q", tt.command)
		}
		if !tt.expectKnown && result == "Action complete." {
			// This is expected - unknown commands get generic fallback
			continue
		}
		if tt.expectKnown && result != "Action complete." {
			// Known command should return specific fallback, not generic
			_, isKnown := DJFallbacks[tt.command]
			if !isKnown {
				t.Errorf("Expected specific fallback for known command %q, got generic", tt.command)
			}
		}
	}
}

func TestBuildDJPrompt(t *testing.T) {
	tests := []struct {
		command string
		args    []interface{}
		wantLen int // minimum expected length
	}{
		{"clear", []interface{}{0}, 50},
		{"clear", []interface{}{5}, 50},
		{"skip", nil, 40},
		{"remove", []interface{}{"Test Song"}, 30},
		{"shuffle", []interface{}{10}, 40},
		{"play", []interface{}{"Test Query"}, 30},
		{"volume", []interface{}{50}, 30},
		{"radio", []interface{}{true}, 30},
		{"loop", []interface{}{false}, 30},
		{"view", []interface{}{5}, 30},
	}

	for _, tt := range tests {
		prompt := buildDJPrompt(tt.command, tt.args)
		if len(prompt) < tt.wantLen {
			t.Errorf("Prompt too short for %s: got %d, want at least %d", tt.command, len(prompt), tt.wantLen)
		}
	}
}
