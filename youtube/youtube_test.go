package youtube

import (
	"testing"
	"time"
)

func TestParseYouTubeURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want YouTubeURLResult
	}{
		{
			name: "watch video",
			url:  "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			want: YouTubeURLResult{VideoID: "dQw4w9WgXcQ"},
		},
		{
			name: "watch video with playlist",
			url:  "https://www.youtube.com/watch?v=abc123&list=PLdef456",
			want: YouTubeURLResult{VideoID: "abc123", PlaylistID: "PLdef456"},
		},
		{
			name: "playlist",
			url:  "https://youtube.com/playlist?list=PL123456",
			want: YouTubeURLResult{PlaylistID: "PL123456"},
		},
		{
			name: "youtu.be short",
			url:  "https://youtu.be/dQw4w9WgXcQ",
			want: YouTubeURLResult{},
		},
		{
			name: "invalid host",
			url:  "https://example.com/watch?v=abc",
			want: YouTubeURLResult{},
		},
		{
			name: "malformed URL",
			url:  "invalid-url",
			want: YouTubeURLResult{},
		},
		{
			name: "empty query",
			url:  "https://www.youtube.com/",
			want: YouTubeURLResult{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseYouTubeURL(tt.url); got != tt.want {
				t.Errorf("ParseYouTubeURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseYoutubeDuration(t *testing.T) {
	tests := []struct {
		name string
		iso  string
		want time.Duration
	}{
		{
			name: "1min 30s",
			iso:  "PT1M30S",
			want: 90 * time.Second,
		},
		{
			name: "1 hour",
			iso:  "PT1H",
			want: 1 * time.Hour,
		},
		{
			name: "30 seconds",
			iso:  "PT30S",
			want: 30 * time.Second,
		},
		{
			name: "1h30m45s",
			iso:  "PT1H30M45S",
			want: 1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name: "1h2m",
			iso:  "PT1H2M",
			want: 1*time.Hour + 2*time.Minute,
		},
		{
			name: "invalid",
			iso:  "invalid",
			want: 0,
		},
		{
			name: "empty",
			iso:  "",
			want: 0,
		},
		{
			name: "only seconds",
			iso:  "PT0S",
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseYoutubeDuration(tt.iso); got != tt.want {
				t.Errorf("parseYoutubeDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
