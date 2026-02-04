package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ProgressBarWidth is the number of characters in the progress bar
const ProgressBarWidth = 15

// NowPlayingMetadata contains all info for a now-playing card
type NowPlayingMetadata struct {
	VideoID         string
	Title           string
	Artist          string
	Album           string
	ThumbnailURL    string
	Duration        time.Duration
	CurrentPosition time.Duration
	IsPlaying       bool
	Volume          int
	GuildID         string
}

// BuildNowPlayingEmbed creates a rich embed for now-playing
func BuildNowPlayingEmbed(metadata *NowPlayingMetadata) *discordgo.MessageEmbed {
	// Extract artist from title if not provided
	artist := metadata.Artist
	if artist == "" {
		artist = ExtractArtistFromTitle(metadata.Title)
	}

	// Build thumbnail URL from video ID if not provided
	thumbnailURL := metadata.ThumbnailURL
	if thumbnailURL == "" {
		thumbnailURL = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", metadata.VideoID)
	}

	// Create progress bar
	progressBar := RenderProgressBar(metadata.CurrentPosition, metadata.Duration, ProgressBarWidth)

	// Determine embed color based on playback state
	color := 0x1DB954 // Spotify green for playing
	if !metadata.IsPlaying {
		color = 0x808080 // Gray for paused
	}

	// Build description
	var desc strings.Builder
	if artist != metadata.Title {
		desc.WriteString(fmt.Sprintf("**Artist:** %s\n", artist))
	}
	if metadata.Album != "" {
		desc.WriteString(fmt.Sprintf("**Album:** %s\n", metadata.Album))
	}

	embed := &discordgo.MessageEmbed{
		Title:       metadata.Title,
		URL:         fmt.Sprintf("https://www.youtube.com/watch?v=%s", metadata.VideoID),
		Description: desc.String(),
		Color:       color,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: thumbnailURL,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: progressBar,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Duration",
				Value:  FormatDuration(metadata.Duration),
				Inline: true,
			},
			{
				Name:   "Volume",
				Value:  fmt.Sprintf("%d%%", metadata.Volume),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add status indicator
	if metadata.IsPlaying {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Status",
			Value:  "▶️ Playing",
			Inline: true,
		})
	} else {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Status",
			Value:  "⏸️ Paused",
			Inline: true,
		})
	}

	return embed
}

// UpdateNowPlayingProgress updates just the progress bar (efficient)
func UpdateNowPlayingProgress(embed *discordgo.MessageEmbed, currentPosition, duration time.Duration) *discordgo.MessageEmbed {
	if embed == nil || embed.Footer == nil {
		return embed
	}

	// Update progress bar in footer
	embed.Footer.Text = RenderProgressBar(currentPosition, duration, ProgressBarWidth)

	return embed
}

// RenderProgressBar creates a Unicode progress bar
func RenderProgressBar(current, total time.Duration, width int) string {
	if total == 0 {
		return "░░░░░░░░░░░░░░░ 0:00 / 0:00"
	}

	percentage := float64(current) / float64(total)
	if percentage > 1.0 {
		percentage = 1.0
	}

	filled := int(percentage * float64(width))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
	currentStr := FormatDuration(current)
	totalStr := FormatDuration(total)

	return fmt.Sprintf("%s %s / %s", bar, currentStr, totalStr)
}

// FormatDuration formats duration as MM:SS or HH:MM:SS
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// ExtractArtistFromTitle parses artist from title
func ExtractArtistFromTitle(title string) string {
	// Remove common suffixes
	cleaned := title
	suffixes := []string{
		"(Official Video)", "(Official Music Video)", "(Official Audio)",
		"(Lyrics)", "(Lyric Video)", "(Audio)", "(Visualizer)",
		"[Official Video]", "[Official Music Video]", "[Official Audio]",
		"[Lyrics]", "[Lyric Video]", "[Audio]",
	}

	for _, suffix := range suffixes {
		cleaned = strings.Replace(cleaned, suffix, "", 1)
	}
	cleaned = strings.TrimSpace(cleaned)

	// Try to split on " - "
	parts := strings.SplitN(cleaned, " - ", 2)
	if len(parts) == 2 {
		artist := strings.TrimSpace(parts[0])
		// Remove featuring info
		feats := []string{" ft.", " feat.", " ft ", " feat ", " featuring "}
		for _, feat := range feats {
			if idx := strings.Index(strings.ToLower(artist), feat); idx != -1 {
				artist = strings.TrimSpace(artist[:idx])
			}
		}
		if artist != "" {
			return artist
		}
	}

	return cleaned
}
