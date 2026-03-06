package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/applemusic"
	"beatbot/config"
	"beatbot/controller"
	"beatbot/discord"
	"beatbot/helpers"
	"beatbot/sentryhelper"
	"beatbot/spotify"
	"beatbot/youtube"
)

// TODO: I should probably just load in the members info on interaction
// and cache it temporarily
// could use this to assure permissions, etc.
func (manager *Manager) QueryAndQueue(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in QueryAndQueue: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	log.Debugf("Querying and queuing: %+v", interaction.Member.User.ID)
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		log.Errorf("Error getting voice state: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)

	if voiceState == nil {
		manager.SendFollowup(ctx, interaction, "The user is not in a voice channel and trying to play a song", "Hey dummy, join a voice channel first", true)
		return
	}

	// join vc if not in one, or move to requester's vc if stopped
	if player.ShouldJoinVoice(voiceState.ChannelID) {
		err := player.JoinVoiceChannel(interaction.Member.User.ID)
		if err != nil {
			errStr := err.Error()
			if errStr != "" && errStr == "voice state not found" {
				manager.SendFollowup(ctx, interaction, "You gotta join a voice channel first!", "Error joining voice channel: "+errStr, true)
				return
			}
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error joining voice channel: "+errStr, true)
			return
		}
	}

	query := interaction.Data.Options[0].Value

	if strings.HasPrefix(query, "https://open.spotify.com/") {
		log.Debugf("Detected Spotify URL: %s", query)

		// Check if Spotify is enabled
		if !config.Config.Spotify.Enabled {
			manager.SendFollowup(ctx, interaction, "", "Spotify integration is not enabled. Ask the bot admin to set SPOTIFY_ENABLED=true.", true)
			return
		}

		spotifyReq, err := spotify.ParseSpotifyURL(query)
		if err != nil {
			log.Errorf("Error parsing Spotify URL: %v", err)
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Invalid Spotify URL: "+err.Error(), true)
			return
		}

		// Handle playlist URLs
		if spotifyReq.PlaylistID != "" {
			manager.handleSpotifyPlaylist(ctx, interaction, player, spotifyReq.PlaylistID)
			return
		}

		// Handle album URLs
		if spotifyReq.AlbumID != "" {
			manager.handleSpotifyAlbum(ctx, interaction, player, spotifyReq.AlbumID)
			return
		}

		// Handle artist URLs (still coming soon)
		if spotifyReq.ArtistID != "" {
			manager.SendFollowup(ctx, interaction, "", "Spotify artists are coming soon! For now, please use track URLs, playlist URLs, or search queries.", true)
			return
		}

		if spotifyReq.TrackID == "" {
			manager.SendFollowup(ctx, interaction, "", "Invalid Spotify URL. Please use a track, playlist, or album URL.", true)
			return
		}

		log.Tracef("Fetching Spotify track: %s", spotifyReq.TrackID)
		trackInfo, err := spotify.GetTrack(ctx, spotifyReq.TrackID)
		if err != nil {
			log.Errorf("Error fetching Spotify track: %v", err)
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error fetching track from Spotify: "+err.Error(), true)
			return
		}

		artistsStr := strings.Join(trackInfo.Artists, ", ")
		youtubeQuery := artistsStr + " - " + trackInfo.Title
		log.Debugf("Converted Spotify track '%s' by '%s' to YouTube query: %s", trackInfo.Title, artistsStr, youtubeQuery)

		manager.SendFollowup(ctx, interaction,
			"",
			fmt.Sprintf("Found **%s** by **%s** on Spotify, searching YouTube...", trackInfo.Title, artistsStr),
			false)

		videos := youtube.Query(ctx, youtubeQuery)
		if len(videos) == 0 {
			log.Warnf("No YouTube results found for Spotify track: %s", youtubeQuery)
			manager.SendFollowup(ctx, interaction,
				"",
				fmt.Sprintf("Couldn't find **%s** by **%s** on YouTube", trackInfo.Title, artistsStr),
				true)
			return
		}

		video := videos[0]
		fallbacks := fallbackSlice(videos, 2)
		log.Debugf("Found YouTube match: %s (ID: %s)", video.Title, video.VideoID)

		firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.GetCurrentSong() == nil

		var followUpMessage string
		if firstSongQueued {
			followUpMessage = fmt.Sprintf("Now playing the YouTube video titled: **%s** (also mention politely that playback could take a few seconds to start, since it's the first song)", video.Title)
		} else {
			followUpMessage = fmt.Sprintf("Now playing the YouTube video titled: **%s**", video.Title)
		}

		manager.SendFollowup(ctx, interaction, followUpMessage, followUpMessage, false)

		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID, fallbacks)
		return
	}

	// Check for Apple Music URL
	if strings.HasPrefix(query, "https://music.apple.com/") ||
		strings.HasPrefix(query, "https://itunes.apple.com/") {
		log.Debugf("Detected Apple Music URL: %s", query)

		appleMusicReq, err := applemusic.ParseAppleMusicURL(query)
		if err != nil {
			log.Errorf("Error parsing Apple Music URL: %v", err)
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Invalid Apple Music URL: "+err.Error(), true)
			return
		}

		// Handle playlists
		if appleMusicReq.PlaylistID != "" {
			manager.handleAppleMusicPlaylist(ctx, interaction, player, appleMusicReq)
			return
		}

		// Handle albums
		if appleMusicReq.AlbumID != "" && appleMusicReq.TrackID == "" {
			manager.handleAppleMusicAlbum(ctx, interaction, player, appleMusicReq)
			return
		}

		// Handle artists (Phase 3 - not implemented yet)
		if appleMusicReq.ArtistID != "" {
			manager.SendFollowup(ctx, interaction, "", "Apple Music artists are coming soon!", true)
			return
		}

		// Handle tracks
		if appleMusicReq.TrackID != "" {
			manager.handleAppleMusicTrack(ctx, interaction, player, appleMusicReq)
			return
		}

		manager.SendFollowup(ctx, interaction, "", "Invalid Apple Music URL type.", true)
		return
	}

	// Check for YouTube playlist URL first
	youtubeURL := youtube.ParseYouTubeURL(query)
	if youtubeURL.PlaylistID != "" {
		log.Debugf("Detected YouTube playlist URL: %s", youtubeURL.PlaylistID)
		manager.handleYouTubePlaylist(ctx, interaction, player, youtubeURL.PlaylistID)
		return
	}

	videoID := youtube.ParseYoutubeUrl(query)

	var video youtube.VideoResponse
	var fallbacks []youtube.VideoResponse

	// user passed in a youtube url
	if videoID != "" {
		videoResponse, err := youtube.GetVideoByID(ctx, videoID)
		if err != nil {
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error getting video stream: "+err.Error(), true)
			return
		}

		video = videoResponse
		// No fallbacks for direct URL requests — the user asked for a specific video
	} else {
		videos := youtube.Query(ctx, query)

		if len(videos) == 0 {
			manager.SendFollowup(ctx, interaction, "There wasn't anything found for "+query, "No videos found for the given query", true)
			return
		}

		video = videos[0]
		fallbacks = fallbackSlice(videos, 2)
	}

	var followUpMessage string
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.GetCurrentSong() == nil

	if firstSongQueued {
		followUpMessage = "Now playing the YouTube video titled: **" + video.Title + "** (also mention politely that playback could take a few seconds to start, since it's the first song and needs to load)"
	} else {
		followUpMessage = "Now playing the YouTube video titled: **" + video.Title + "**"
	}

	manager.SendFollowup(ctx, interaction, followUpMessage, followUpMessage, false)
	player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID, fallbacks)
}

func (manager *Manager) handleQueue(ctx context.Context, transaction *sentry.Span, interaction *Interaction) Response {
	go manager.QueryAndQueue(ctx, transaction, interaction)

	return Response{
		Type: 5,
	}
}

func (manager *Manager) onView(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in onView: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() && !player.Player.IsPlaying() && player.GetCurrentSong() == nil {
		manager.SendFollowup(ctx, interaction, "The queue is empty and nothing is playing", "The queue is empty and nothing is playing", false)
	}

	// Take a snapshot under the queue lock to avoid races during iteration.
	queueSnapshot := player.GetQueueSnapshot()
	formatted_queue := ""
	for i, video := range queueSnapshot {
		formatted_queue += fmt.Sprintf("%d. %s\n", i+1, video.Video.Title)
	}

	// Capture the pointer once; nil-check and dereference are in the same expression.
	if song := player.GetCurrentSong(); song != nil {
		formatted_queue += fmt.Sprintf("\nNow playing: **%s**", *song)
	}

	manager.SendFollowup(ctx, interaction, "", formatted_queue, false)
}

func (manager *Manager) handleView(ctx context.Context, transaction *sentry.Span, interaction *Interaction) Response {
	go manager.onView(ctx, transaction, interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) onSkip(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in onSkip: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	player := manager.Controller.GetPlayer(interaction.GuildID)

	currentSong := player.GetCurrentSong()
	if !player.Player.IsPlaying() && currentSong == nil {
		manager.SendFollowup(ctx, interaction, "user tried to skip but nothing is playing", "Nothing to skip", true)
		return
	}

	userName := interaction.Member.User.Username

	go player.Skip()

	next := player.GetNext()

	songTitle := ""
	if currentSong != nil {
		songTitle = *currentSong
	}
	response := "@" + userName + " skipped **" + songTitle + "**"
	if next != nil {
		response += "\n\nNow playing **" + next.Video.Title + "**"
	} else {
		response += "\n\nNo more songs in queue"
	}

	manager.SendFollowup(ctx, interaction, response, response, false)
}

func (manager *Manager) handlePurge(ctx context.Context, interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go player.Clear()

	manager.SendFollowup(ctx, interaction, "Queue purged", "Queue purged", false)
}

// handleClear clears the queue but keeps the current song playing
func (manager *Manager) handleClear(ctx context.Context, interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	// Get queue length before clearing
	queueLen := 0
	player.Queue.Mutex.Lock()
	queueLen = len(player.Queue.Items)
	player.Queue.Mutex.Unlock()

	if queueLen == 0 {
		// Empty queue - show hint and return
		hint := manager.Hints.ShowIfApplicable(interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Nothing to clear!" + hint,
			},
		}
	}

	// Clear the queue (async, doesn't stop current track)
	go player.Clear()

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateClearDJResponse(djCtx, queueLen)

	// Add hint with 15% chance
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	log.WithFields(log.Fields{
		"module":   "handlers",
		"guild_id": interaction.GuildID,
		"cleared":  queueLen,
		"user_id":  interaction.Member.User.ID,
	}).Info("Queue cleared")

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: djResponse + hint,
		},
	}
}

func (manager *Manager) handleSkip(ctx context.Context, transaction *sentry.Span, interaction *Interaction) Response {
	go manager.onSkip(ctx, transaction, interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleReset(ctx context.Context, transaction *sentry.Span, interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				sentryhelper.CaptureException(ctx, fmt.Errorf("panic in handleReset: %v", err))
				transaction.Status = sentry.SpanStatusInternalError
			}
			transaction.Finish()
		}()
		player.Reset(ctx, &controller.GuildQueueItemInteraction{
			UserID:           interaction.Member.User.ID,
			InteractionToken: interaction.Token,
			AppID:            manager.AppID,
		})
	}()

	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleRemove(ctx context.Context, interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() {
		hint := manager.Hints.ShowIfApplicable(interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "the queue is empty" + hint,
			},
		}
	}

	var index int = 1 // Default to first song if no index provided, .Remove substracts 1
	if len(interaction.Data.Options) > 0 {
		var err error
		index, err = strconv.Atoi(interaction.Data.Options[0].Value)
		if err != nil {
			return Response{
				Type: 4,
				Data: ResponseData{
					Content: "Invalid index",
				},
			}
		}
	}

	removed_title := player.Remove(index)

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "remove", removed_title)
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	if removed_title != "" {
		djResponse = "@" + interaction.Member.User.Username + " removed **" + removed_title + "** - " + djResponse
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: djResponse + hint,
		},
	}
}

func (manager *Manager) handleShuffle(ctx context.Context, interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	count := player.Shuffle()

	if count == 0 {
		hint := manager.Hints.ShowIfApplicable(interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "the queue is empty, nothing to shuffle" + hint,
			},
		}
	}

	if count == 1 {
		hint := manager.Hints.ShowIfApplicable(interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "only one song in the queue, nothing to shuffle" + hint,
			},
		}
	}

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "shuffle", count)
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: djResponse + hint,
		},
	}
}
