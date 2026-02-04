package discord

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseButtonCustomID(t *testing.T) {
	tests := []struct {
		name     string
		customID string
		wantAction string
		wantGuildID string
		wantOK   bool
	}{
		{
			name:     "valid play/pause",
			customID: "np:playpause:123456789",
			wantAction: "playpause",
			wantGuildID: "123456789",
			wantOK:   true,
		},
		{
			name:     "valid skip",
			customID: "np:skip:987654321",
			wantAction: "skip",
			wantGuildID: "987654321",
			wantOK:   true,
		},
		{
			name:     "valid stop",
			customID: "np:stop:111222333",
			wantAction: "stop",
			wantGuildID: "111222333",
			wantOK:   true,
		},
		{
			name:     "valid volume down",
			customID: "np:voldown:555666777",
			wantAction: "voldown",
			wantGuildID: "555666777",
			wantOK:   true,
		},
		{
			name:     "valid volume up",
			customID: "np:volup:888999000",
			wantAction: "volup",
			wantGuildID: "888999000",
			wantOK:   true,
		},
		{
			name:     "valid queue",
			customID: "np:queue:123123123",
			wantAction: "queue",
			wantGuildID: "123123123",
			wantOK:   true,
		},
		{
			name:     "valid shuffle",
			customID: "np:shuffle:456456456",
			wantAction: "shuffle",
			wantGuildID: "456456456",
			wantOK:   true,
		},
		{
			name:     "invalid prefix",
			customID: "invalid:skip:123456789",
			wantAction: "",
			wantGuildID: "",
			wantOK:   false,
		},
		{
			name:     "missing parts",
			customID: "np:skip",
			wantAction: "",
			wantGuildID: "",
			wantOK:   false,
		},
		{
			name:     "too many parts",
			customID: "np:skip:123:456",
			wantAction: "",
			wantGuildID: "",
			wantOK:   false,
		},
		{
			name:     "empty string",
			customID: "",
			wantAction: "",
			wantGuildID: "",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAction, gotGuildID, gotOK := ParseButtonCustomID(tt.customID)
			if gotAction != tt.wantAction {
				t.Errorf("ParseButtonCustomID() action = %q, want %q", gotAction, tt.wantAction)
			}
			if gotGuildID != tt.wantGuildID {
				t.Errorf("ParseButtonCustomID() guildID = %q, want %q", gotGuildID, tt.wantGuildID)
			}
			if gotOK != tt.wantOK {
				t.Errorf("ParseButtonCustomID() ok = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}

func TestBuildPlaybackButtons(t *testing.T) {
	tests := []struct {
		name      string
		guildID   string
		isPlaying bool
	}{
		{
			name:      "playing state",
			guildID:   "123456789",
			isPlaying: true,
		},
		{
			name:      "paused state",
			guildID:   "987654321",
			isPlaying: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			components := BuildPlaybackButtons(tt.guildID, tt.isPlaying)

			// Should have 2 action rows
			if len(components) != 2 {
				t.Fatalf("Expected 2 action rows, got %d", len(components))
			}

			// Check first row (primary controls)
			primaryRow, ok := components[0].(discordgo.ActionsRow)
			if !ok {
				t.Fatal("First component is not an ActionsRow")
			}

			// Should have 4 buttons in primary row
			if len(primaryRow.Components) != 4 {
				t.Errorf("Expected 4 buttons in primary row, got %d", len(primaryRow.Components))
			}

			// Check each button in primary row
			expectedPrimaryButtons := []struct {
				action   string
				emoji    string
				disabled bool
			}{
				{"prev", "⏮️", true},
				{"playpause", getPlayPauseEmoji(tt.isPlaying), false},
				{"skip", "⏭️", false},
				{"stop", "⏹️", false},
			}

			for i, expected := range expectedPrimaryButtons {
				btn, ok := primaryRow.Components[i].(discordgo.Button)
				if !ok {
					t.Errorf("Component %d is not a Button", i)
					continue
				}

				expectedCustomID := "np:" + expected.action + ":" + tt.guildID
				if btn.CustomID != expectedCustomID {
					t.Errorf("Button %d: expected CustomID %q, got %q", i, expectedCustomID, btn.CustomID)
				}

				if btn.Emoji.Name != expected.emoji {
					t.Errorf("Button %d: expected emoji %q, got %q", i, expected.emoji, btn.Emoji.Name)
				}

				if btn.Disabled != expected.disabled {
					t.Errorf("Button %d: expected disabled=%v, got %v", i, expected.disabled, btn.Disabled)
				}
			}

			// Check second row (secondary controls)
			secondaryRow, ok := components[1].(discordgo.ActionsRow)
			if !ok {
				t.Fatal("Second component is not an ActionsRow")
			}

			// Should have 4 buttons in secondary row
			if len(secondaryRow.Components) != 4 {
				t.Errorf("Expected 4 buttons in secondary row, got %d", len(secondaryRow.Components))
			}

			// Check each button in secondary row
			expectedSecondaryButtons := []struct {
				action string
				emoji  string
				label  string
			}{
				{"voldown", "🔉", "Vol -"},
				{"volup", "🔊", "Vol +"},
				{"queue", "📜", "Queue"},
				{"shuffle", "🔀", "Shuffle"},
			}

			for i, expected := range expectedSecondaryButtons {
				btn, ok := secondaryRow.Components[i].(discordgo.Button)
				if !ok {
					t.Errorf("Component %d is not a Button", i)
					continue
				}

				expectedCustomID := "np:" + expected.action + ":" + tt.guildID
				if btn.CustomID != expectedCustomID {
					t.Errorf("Button %d: expected CustomID %q, got %q", i, expectedCustomID, btn.CustomID)
				}

				if btn.Emoji.Name != expected.emoji {
					t.Errorf("Button %d: expected emoji %q, got %q", i, expected.emoji, btn.Emoji.Name)
				}

				if btn.Label != expected.label {
					t.Errorf("Button %d: expected label %q, got %q", i, expected.label, btn.Label)
				}
			}
		})
	}
}

func TestGetPlayPauseEmoji(t *testing.T) {
	tests := []struct {
		name      string
		isPlaying bool
		want      string
	}{
		{
			name:      "playing",
			isPlaying: true,
			want:      "⏸️",
		},
		{
			name:      "paused",
			isPlaying: false,
			want:      "▶️",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPlayPauseEmoji(tt.isPlaying)
			if got != tt.want {
				t.Errorf("getPlayPauseEmoji() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestButtonStructure(t *testing.T) {
	// Test that button structure matches Discord API spec
	guildID := "test123"
	components := BuildPlaybackButtons(guildID, true)

	for rowIdx, component := range components {
		row, ok := component.(discordgo.ActionsRow)
		if !ok {
			t.Errorf("Row %d is not an ActionsRow", rowIdx)
			continue
		}

		if len(row.Components) > 5 {
			t.Errorf("Row %d has %d buttons, Discord API limit is 5", rowIdx, len(row.Components))
		}

		for btnIdx, btnComponent := range row.Components {
			btn, ok := btnComponent.(discordgo.Button)
			if !ok {
				t.Errorf("Row %d, Button %d is not a Button", rowIdx, btnIdx)
				continue
			}

			// Check CustomID format
			if !strings.HasPrefix(btn.CustomID, "np:") {
				t.Errorf("Row %d, Button %d: CustomID should start with 'np:', got %q", rowIdx, btnIdx, btn.CustomID)
			}

			parts := strings.Split(btn.CustomID, ":")
			if len(parts) != 3 {
				t.Errorf("Row %d, Button %d: CustomID should have 3 parts, got %d", rowIdx, btnIdx, len(parts))
			}

			if parts[2] != guildID {
				t.Errorf("Row %d, Button %d: CustomID should end with guildID %q, got %q", rowIdx, btnIdx, guildID, parts[2])
			}

			// Check button has either label or emoji
			if btn.Label == "" && btn.Emoji == nil {
				t.Errorf("Row %d, Button %d: Button should have either label or emoji", rowIdx, btnIdx)
			}
		}
	}
}
