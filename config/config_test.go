package config

import "testing"

func TestGetIdleTimeout(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"empty", "", 20},
		{"invalid", "abc", 20},
		{"zero", "0", 20},
		{"negative", "-1", 20},
		{"valid_small", "10", 10},
		{"valid_default", "20", 20},
		{"valid_large", "30", 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("IDLE_TIMEOUT_MINUTES", tt.env)
			if got := getIdleTimeout(); got != tt.want {
				t.Errorf("getIdleTimeout() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestGetPlaylistLimit(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"empty", "", 10},
		{"invalid", "foo", 10},
		{"zero", "0", 10},
		{"negative", "-10", 10},
		{"min", "1", 1},
		{"mid", "25", 25},
		{"max", "50", 50},
		{"over", "51", 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SPOTIFY_PLAYLIST_LIMIT", tt.env)
			if got := getPlaylistLimit(); got != tt.want {
				t.Errorf("getPlaylistLimit() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestGetYouTubePlaylistLimit(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"empty", "", 15},
		{"invalid", "foo", 15},
		{"zero", "0", 15},
		{"negative", "-10", 15},
		{"min", "1", 1},
		{"mid", "25", 25},
		{"max", "50", 50},
		{"over", "51", 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YOUTUBE_PLAYLIST_LIMIT", tt.env)
			if got := getYouTubePlaylistLimit(); got != tt.want {
				t.Errorf("getYouTubePlaylistLimit() = %d; want %d", got, tt.want)
			}
		})
	}
}

func TestGetAudioBitrate(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"empty", "", 128000},
		{"invalid", "foo", 128000},
		{"zero", "0", 128000},
		{"negative", "-100", 128000},
		{"below_min", "7000", 8000},
		{"min", "8000", 8000},
		{"default", "128000", 128000},
		{"high", "300000", 300000},
		{"above_max", "600000", 512000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUDIO_BITRATE", tt.env)
			if got := getAudioBitrate(); got != tt.want {
				t.Errorf("getAudioBitrate() = %d; want %d", got, tt.want)
			}
		})
	}
}
