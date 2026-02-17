package discord

import (
	"testing"
)

func TestParseButtonCustomID(t *testing.T) {
	tests := []struct {
		name        string
		customID    string
		wantAction  string
		wantGuildID string
		wantOK      bool
	}{
		{
			name:        "valid play/pause",
			customID:    "np:playpause:123456789",
			wantAction:  "playpause",
			wantGuildID: "123456789",
			wantOK:      true,
		},
		{
			name:        "valid skip",
			customID:    "np:skip:987654321",
			wantAction:  "skip",
			wantGuildID: "987654321",
			wantOK:      true,
		},
		{
			name:        "valid stop",
			customID:    "np:stop:111222333",
			wantAction:  "stop",
			wantGuildID: "111222333",
			wantOK:      true,
		},
		{
			name:        "valid volume down",
			customID:    "np:voldown:555666777",
			wantAction:  "voldown",
			wantGuildID: "555666777",
			wantOK:      true,
		},
		{
			name:        "valid volume up",
			customID:    "np:volup:888999000",
			wantAction:  "volup",
			wantGuildID: "888999000",
			wantOK:      true,
		},
		{
			name:        "valid queue",
			customID:    "np:queue:123123123",
			wantAction:  "queue",
			wantGuildID: "123123123",
			wantOK:      true,
		},
		{
			name:        "valid shuffle",
			customID:    "np:shuffle:456456456",
			wantAction:  "shuffle",
			wantGuildID: "456456456",
			wantOK:      true,
		},
		{
			name:        "invalid prefix",
			customID:    "invalid:skip:123456789",
			wantAction:  "",
			wantGuildID: "",
			wantOK:      false,
		},
		{
			name:        "missing parts",
			customID:    "np:skip",
			wantAction:  "",
			wantGuildID: "",
			wantOK:      false,
		},
		{
			name:        "too many parts",
			customID:    "np:skip:123:456",
			wantAction:  "",
			wantGuildID: "",
			wantOK:      false,
		},
		{
			name:        "empty string",
			customID:    "",
			wantAction:  "",
			wantGuildID: "",
			wantOK:      false,
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
