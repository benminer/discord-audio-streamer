package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bwmarrin/discordgo"
	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/discord"
	"beatbot/gemini"
	"beatbot/lyrics"
	"beatbot/sentryhelper"
	"beatbot/spotify"
)

func (manager *Manager) handlePing() Response {
	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "Pong! 🏓",
		},
	}
}

func (manager *Manager) onHelp(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in onHelp: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	response := gemini.GenerateHelpfulResponse(ctx, "(user issued the help command, return a nicely formatted help menu)")
	if response == "" {
		response = `**Music Control:**
/play (or /queue) - Queue a song. Takes a search query, YouTube URL/playlist, or Spotify URL. Note: YouTube links with ?list= will queue the whole playlist
/skip - Skip the current song and play the next in queue
/pause (or /stop) - Pause the current song
/resume - Resume playback
/volume - Set playback volume (0-100)

**Queue Management:**
/view - View the current queue
/remove - Remove a song from the queue by index number
/reset - Clear everything and reset the player

**Other:**
/help - Show this help menu
/ping - Check if the bot is alive`
	}
	manager.SendRequest(interaction, response, false)
}

func (manager *Manager) handleHelp(ctx context.Context, transaction *sentry.Span, interaction *Interaction) Response {
	go manager.onHelp(ctx, transaction, interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleTopSongs(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in handleTopSongs: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	artistQuery := interaction.Data.Options[0].Value

	if !config.Config.Spotify.Enabled {
		manager.SendFollowup(ctx, interaction, "", "Spotify integration is not enabled. Ask the bot admin to set SPOTIFY_ENABLED=true.", true)
		return
	}

	// Check voice state
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}
	if voiceState == nil {
		manager.SendFollowup(ctx, interaction, "The user is not in a voice channel", "Join a voice channel first!", true)
		return
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.ShouldJoinVoice(voiceState.ChannelID) {
		if err := player.JoinVoiceChannel(interaction.Member.User.ID); err != nil {
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error joining voice channel: "+err.Error(), true)
			return
		}
	}

	// Search for artist
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "spotify_topsongs",
		Message:  "Searching for artist: " + artistQuery,
		Level:    sentry.LevelInfo,
	})

	artistID, artistName, err := spotify.SearchArtist(ctx, artistQuery)
	if err != nil {
		sentryhelper.CaptureException(ctx, err)
		manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("Couldn't find artist **%s** on Spotify.", artistQuery), true)
		return
	}

	manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("Found **%s** on Spotify, fetching top songs...", artistName), false)

	// Get top songs
	tracks, err := spotify.GetArtistTopSongs(ctx, artistID)
	if err != nil {
		sentryhelper.CaptureException(ctx, err)
		manager.SendFollowup(ctx, interaction, "", "Error fetching top songs from Spotify.", true)
		return
	}

	if len(tracks) == 0 {
		manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("No top songs found for **%s**.", artistName), true)
		return
	}

	// Limit to 5
	if len(tracks) > 5 {
		tracks = tracks[:5]
	}

	// Convert to PlaylistTrackInfo for the collection handler
	playlistTracks := make([]spotify.PlaylistTrackInfo, len(tracks))
	for i, t := range tracks {
		playlistTracks[i] = spotify.PlaylistTrackInfo{
			TrackInfo: t,
			Position:  i,
		}
	}

	manager.handleSpotifyCollection(ctx, interaction, player, SpotifyCollection{
		Type:        "artist top songs",
		ID:          artistID,
		Name:        artistName,
		Artist:      artistName,
		Tracks:      playlistTracks,
		TotalTracks: len(playlistTracks),
	})
}

func (manager *Manager) onLyrics(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in onLyrics: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	player := manager.Controller.GetPlayer(interaction.GuildID)

	currentSong := player.GetCurrentSong()
	if currentSong == nil {
		manager.SendFollowup(ctx, interaction, "", "Nothing is currently playing.", true)
		return
	}

	title := *currentSong

	artist := discord.ExtractArtistFromTitle(title)

	query := artist + " " + title

	lc := lyrics.New()

	lyricsText, trackInfo, err := lc.Search(query)

	if err != nil || lyricsText == "" {
		manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("Couldn't find lyrics for **%s**.", title), false)
		return
	}

	if len(lyricsText) > 3800 {
		lyricsText = lyricsText[:3800]
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Lyrics: " + trackInfo,
		Description: lyricsText,
		Color:       0x7289DA,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Lyrics provided by lrclib.net",
		},
	}

	manager.sendEmbedFollowup(interaction, embed, false)
}

func (manager *Manager) handleLyrics(ctx context.Context, transaction *sentry.Span, interaction *Interaction) Response {
	go manager.onLyrics(ctx, transaction, interaction)
	return Response{Type: 5}
}

// handleMessageComponent handles button click interactions (Type 3)
func (manager *Manager) handleMessageComponent(interaction *Interaction) Response {
	ctx := context.Background()

	// Parse the custom ID to get the action
	action, guildID, ok := discord.ParseButtonCustomID(interaction.Data.CustomID)
	if !ok {
		log.Errorf("Invalid button custom_id: %s", interaction.Data.CustomID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Invalid button interaction",
				Flags:   64,
			},
		}
	}

	// Verify guild ID matches
	if guildID != interaction.GuildID {
		log.Errorf("Guild ID mismatch: button for %s, interaction from %s", guildID, interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Guild mismatch",
				Flags:   64,
			},
		}
	}

	log.Debugf("Button clicked: %s in guild %s", action, guildID)

	// Route to appropriate handler based on action
	switch action {
	case "playpause":
		// Toggle between pause and resume based on current state
		player := manager.Controller.GetPlayer(guildID)
		if player.Player.IsPaused() {
			return manager.handleResume(ctx, interaction)
		}
		return manager.handlePause(ctx, interaction)
	case "skip":
		// Create a minimal transaction for skip
		ctx, transaction := sentryhelper.StartCommandTransaction(
			ctx,
			"button_skip",
			interaction.GuildID,
			interaction.Member.User.ID,
		)
		defer transaction.Finish()
		return manager.handleSkip(ctx, transaction, interaction)
	case "stop":
		return manager.handlePause(ctx, interaction)
	default:
		log.Errorf("Unknown button action: %s", action)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Unknown button action",
				Flags:   64,
			},
		}
	}
}

func (manager *Manager) sendEmbedFollowup(interaction *Interaction, embed *discordgo.MessageEmbed, ephemeral bool) {
	payload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}

	if ephemeral {
		payload["flags"] = 64
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("Error marshalling embed payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/"+manager.AppID+"/"+interaction.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		log.Errorf("Error sending embed followup: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
}
