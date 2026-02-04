package discord

import (
	"strings"
	"testing"
	"time"
)

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		name    string
		current time.Duration
		total   time.Duration
		width   int
		want    string
	}{
		{
			name:    "zero duration",
			current: 0,
			total:   0,
			width:   15,
			want:    "░░░░░░░░░░░░░░░ 0:00 / 0:00",
		},
		{
			name:    "50% progress",
			current: 30 * time.Second,
			total:   60 * time.Second,
			width:   10,
			want:    "▓▓▓▓▓░░░░░ 0:30 / 1:00",
		},
		{
			name:    "100% progress",
			current: 60 * time.Second,
			total:   60 * time.Second,
			width:   10,
			want:    "▓▓▓▓▓▓▓▓▓▓ 1:00 / 1:00",
		},
		{
			name:    "0% progress",
			current: 0,
			total:   120 * time.Second,
			width:   15,
			want:    "░░░░░░░░░░░░░░░ 0:00 / 2:00",
		},
		{
			name:    "33% progress",
			current: 40 * time.Second,
			total:   120 * time.Second,
			width:   15,
			want:    "▓▓▓▓▓░░░░░░░░░░ 0:40 / 2:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderProgressBar(tt.current, tt.total, tt.width)
			if got != tt.want {
				t.Errorf("RenderProgressBar() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "zero",
			duration: 0,
			want:     "0:00",
		},
		{
			name:     "30 seconds",
			duration: 30 * time.Second,
			want:     "0:30",
		},
		{
			name:     "1 minute",
			duration: 60 * time.Second,
			want:     "1:00",
		},
		{
			name:     "1 minute 30 seconds",
			duration: 90 * time.Second,
			want:     "1:30",
		},
		{
			name:     "59 minutes 59 seconds",
			duration: 59*time.Minute + 59*time.Second,
			want:     "59:59",
		},
		{
			name:     "1 hour",
			duration: 60 * time.Minute,
			want:     "1:00:00",
		},
		{
			name:     "1 hour 30 minutes 45 seconds",
			duration: 1*time.Hour + 30*time.Minute + 45*time.Second,
			want:     "1:30:45",
		},
		{
			name:     "10 hours",
			duration: 10 * time.Hour,
			want:     "10:00:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("FormatDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractArtistFromTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "standard format",
			title: "Rick Astley - Never Gonna Give You Up",
			want:  "Rick Astley",
		},
		{
			name:  "with official video",
			title: "Queen - Bohemian Rhapsody (Official Video)",
			want:  "Queen",
		},
		{
			name:  "with official music video",
			title: "The Weeknd - Blinding Lights (Official Music Video)",
			want:  "The Weeknd",
		},
		{
			name:  "with lyrics",
			title: "Ed Sheeran - Shape of You (Lyrics)",
			want:  "Ed Sheeran",
		},
		{
			name:  "with brackets",
			title: "Imagine Dragons - Radioactive [Official Music Video]",
			want:  "Imagine Dragons",
		},
		{
			name:  "single word title",
			title: "Despacito",
			want:  "Despacito",
		},
		{
			name:  "no separator",
			title: "Some Random Video Title",
			want:  "Some Random Video Title",
		},
		{
			name:  "with featuring",
			title: "Dua Lipa ft. DaBaby - Levitating",
			want:  "Dua Lipa",
		},
		{
			name:  "with feat",
			title: "Drake feat. Rihanna - Take Care",
			want:  "Drake",
		},
		{
			name:  "multiple suffixes",
			title: "Adele - Hello (Official Video) (Lyrics)",
			want:  "Adele",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractArtistFromTitle(tt.title)
			if got != tt.want {
				t.Errorf("ExtractArtistFromTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildNowPlayingEmbed(t *testing.T) {
	metadata := &NowPlayingMetadata{
		VideoID:         "dQw4w9WgXcQ",
		Title:           "Rick Astley - Never Gonna Give You Up",
		Duration:        3*time.Minute + 32*time.Second,
		CurrentPosition: 1*time.Minute + 45*time.Second,
		IsPlaying:       true,
		Volume:          100,
		GuildID:         "123456789",
	}

	embed := BuildNowPlayingEmbed(metadata)

	// Check basic fields
	if embed.Title != metadata.Title {
		t.Errorf("Expected title %q, got %q", metadata.Title, embed.Title)
	}

	expectedURL := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	if embed.URL != expectedURL {
		t.Errorf("Expected URL %q, got %q", expectedURL, embed.URL)
	}

	// Check thumbnail URL
	expectedThumbnail := "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg"
	if embed.Thumbnail.URL != expectedThumbnail {
		t.Errorf("Expected thumbnail %q, got %q", expectedThumbnail, embed.Thumbnail.URL)
	}

	// Check color (playing = green)
	expectedColor := 0x1DB954
	if embed.Color != expectedColor {
		t.Errorf("Expected color %d, got %d", expectedColor, embed.Color)
	}

	// Check footer contains progress bar
	if !strings.Contains(embed.Footer.Text, "▓") && !strings.Contains(embed.Footer.Text, "░") {
		t.Error("Expected footer to contain progress bar")
	}

	// Check fields exist
	if len(embed.Fields) < 2 {
		t.Errorf("Expected at least 2 fields, got %d", len(embed.Fields))
	}

	// Check description contains artist
	if !strings.Contains(embed.Description, "Rick Astley") {
		t.Error("Expected description to contain artist name")
	}
}

func TestBuildNowPlayingEmbedPaused(t *testing.T) {
	metadata := &NowPlayingMetadata{
		VideoID:         "test123",
		Title:           "Test Song",
		Duration:        180 * time.Second,
		CurrentPosition: 60 * time.Second,
		IsPlaying:       false, // Paused
		Volume:          80,
		GuildID:         "987654321",
	}

	embed := BuildNowPlayingEmbed(metadata)

	// Check color (paused = gray)
	expectedColor := 0x808080
	if embed.Color != expectedColor {
		t.Errorf("Expected color %d (gray), got %d", expectedColor, embed.Color)
	}

	// Check status field shows paused
	foundStatus := false
	for _, field := range embed.Fields {
		if field.Name == "Status" && strings.Contains(field.Value, "⏸️") {
			foundStatus = true
			break
		}
	}
	if !foundStatus {
		t.Error("Expected status field to show paused emoji")
	}
}

func TestUpdateNowPlayingProgress(t *testing.T) {
	// Create initial embed
	metadata := &NowPlayingMetadata{
		VideoID:         "test123",
		Title:           "Test Song",
		Duration:        120 * time.Second,
		CurrentPosition: 30 * time.Second,
		IsPlaying:       true,
		Volume:          100,
		GuildID:         "123",
	}

	embed := BuildNowPlayingEmbed(metadata)
	initialFooter := embed.Footer.Text

	// Update progress
	updatedEmbed := UpdateNowPlayingProgress(embed, 60*time.Second, 120*time.Second)

	// Check footer changed
	if updatedEmbed.Footer.Text == initialFooter {
		t.Error("Expected footer to be updated")
	}

	// Check new progress is reflected
	if !strings.Contains(updatedEmbed.Footer.Text, "1:00") {
		t.Error("Expected footer to contain updated time 1:00")
	}
}
