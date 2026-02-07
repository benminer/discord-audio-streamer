package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// BuildPlaybackButtons creates button action rows for now-playing card
func BuildPlaybackButtons(guildID string, isPlaying bool) []discordgo.MessageComponent {
	// Single row with only working playback controls
	primaryRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "",
				Style:    discordgo.PrimaryButton,
				CustomID: fmt.Sprintf("np:playpause:%s", guildID),
				Emoji: &discordgo.ComponentEmoji{
					Name: getPlayPauseEmoji(isPlaying),
				},
			},
			discordgo.Button{
				Label:    "",
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("np:skip:%s", guildID),
				Emoji: &discordgo.ComponentEmoji{
					Name: "⏭️",
				},
			},
			discordgo.Button{
				Label:    "",
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("np:stop:%s", guildID),
				Emoji: &discordgo.ComponentEmoji{
					Name: "⏹️",
				},
			},
		},
	}

	return []discordgo.MessageComponent{primaryRow}
}

func getPlayPauseEmoji(isPlaying bool) string {
	if isPlaying {
		return "⏸️"
	}
	return "▶️"
}

// ParseButtonCustomID extracts action and guildID from button custom ID
// Format: "np:action:guildID"
func ParseButtonCustomID(customID string) (action, guildID string, ok bool) {
	parts := strings.Split(customID, ":")
	if len(parts) != 3 || parts[0] != "np" {
		return "", "", false
	}
	return parts[1], parts[2], true
}
