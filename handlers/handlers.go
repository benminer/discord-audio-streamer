package handlers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"net/http"
	"os"
	"strconv"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/controller"
	"beatbot/discord"
	"beatbot/gemini"
	"beatbot/sentryhelper"
	"beatbot/spotify"
	"beatbot/youtube"
)

type Response struct {
	Type int          `json:"type"`
	Data ResponseData `json:"data"`
}

type ResponseData struct {
	Content string `json:"content"`
	Flags   int    `json:"flags"`
}

type InteractionOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type InteractionData struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Type    int                 `json:"type"`
	Options []InteractionOption `json:"options"`
}

type UserData struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Avatar     string `json:"avatar"`
	GlobalName string `json:"global_name"`
}

type MemberData struct {
	User     UserData `json:"user"`
	Roles    []string `json:"roles"`
	JoinedAt string   `json:"joined_at"`
	Nick     *string  `json:"nick"`
}

type Interaction struct {
	ApplicationID string          `json:"application_id"`
	Type          int             `json:"type"`
	Data          InteractionData `json:"data"`
	Token         string          `json:"token"`
	Member        MemberData      `json:"member"`
	Version       int             `json:"version"`
	GuildID       string          `json:"guild_id"`
	ChannelID     string          `json:"channel_id"`
}

type Options struct {
	EnforceVoiceChannel bool
}

type Manager struct {
	AppID      string
	PublicKey  string
	BotToken   string
	Controller *controller.Controller
}

func NewManager(appID string, controller *controller.Controller) *Manager {
	publicKey := config.Config.Discord.PublicKey
	botToken := config.Config.Discord.BotToken

	if publicKey == "" || botToken == "" {
		log.Fatal("DISCORD_PUBLIC_KEY and DISCORD_BOT_TOKEN must be set")
		os.Exit(1)
	}

	return &Manager{
		AppID:      appID,
		PublicKey:  publicKey,
		BotToken:   botToken,
		Controller: controller,
	}
}

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

	shouldJoin := player.VoiceChannelID == nil ||
		player.VoiceConnection == nil ||
		(player.VoiceChannelID != nil &&
			player.IsEmpty() && !player.Player.IsPlaying() &&
			*player.VoiceChannelID != voiceState.ChannelID)

	// join vc if not in one
	// or move to requester's vc if stopped
	if shouldJoin {
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
		log.Debugf("Found YouTube match: %s (ID: %s)", video.Title, video.VideoID)

		firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil

		var followUpMessage string
		if firstSongQueued {
			followUpMessage = fmt.Sprintf("Now playing the YouTube video titled: **%s** (also mention politely that playback could take a few seconds to start, since it's the first song)", video.Title)
		} else {
			followUpMessage = fmt.Sprintf("Now playing the YouTube video titled: **%s**", video.Title)
		}

		manager.SendFollowup(ctx, interaction, followUpMessage, followUpMessage, false)

		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID)
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

	// user passed in a youtube url
	if videoID != "" {
		videoResponse, err := youtube.GetVideoByID(ctx, videoID)
		if err != nil {
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error getting video stream: "+err.Error(), true)
			return
		}

		video = videoResponse
	} else {
		videos := youtube.Query(ctx, query)

		if len(videos) == 0 {
			manager.SendFollowup(ctx, interaction, "There wasn't anything found for "+query, "No videos found for the given query", true)
			return
		}

		video = videos[0]
	}

	var followUpMessage string
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil

	if firstSongQueued {
		followUpMessage = "Now playing the YouTube video titled: **" + video.Title + "** (also mention politely that playback could take a few seconds to start, since it's the first song and needs to load)"
	} else {
		followUpMessage = "Now playing the YouTube video titled: **" + video.Title + "**"
	}

	manager.SendFollowup(ctx, interaction, followUpMessage, followUpMessage, false)
	player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID)
}

// searchResult holds the result of a single YouTube search for collection processing
type searchResult struct {
	Position int
	Video    youtube.VideoResponse
	Query    string
	Found    bool
}

// SpotifyCollection represents a playlist or album from Spotify
type SpotifyCollection struct {
	Type        string // "playlist" or "album"
	ID          string
	Name        string
	Artist      string // only populated for albums
	Tracks      []spotify.PlaylistTrackInfo
	TotalTracks int
}

// DisplayName returns a formatted name for the collection (includes artist for albums)
func (c SpotifyCollection) DisplayName() string {
	if c.Artist != "" {
		return fmt.Sprintf("%s by %s", c.Name, c.Artist)
	}
	return c.Name
}

// handleSpotifyCollection processes tracks from a Spotify playlist or album
func (manager *Manager) handleSpotifyCollection(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, collection SpotifyCollection) {
	category := "spotify_" + collection.Type

	// Add breadcrumb for successful fetch
	breadcrumbData := map[string]interface{}{
		collection.Type + "_id":   collection.ID,
		collection.Type + "_name": collection.Name,
		"track_count":             len(collection.Tracks),
		"total_tracks":            collection.TotalTracks,
	}
	if collection.Artist != "" {
		breadcrumbData["artist"] = collection.Artist
	}

	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: category,
		Message:  fmt.Sprintf("Fetched %d tracks from %s '%s'", len(collection.Tracks), collection.Type, collection.Name),
		Level:    sentry.LevelInfo,
		Data:     breadcrumbData,
	})

	// Use context with timeout for graceful cancellation if Discord interaction times out
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Start parallel YouTube search span
	searchSpan := sentry.StartSpan(searchCtx, "youtube.parallel_search")
	searchSpan.Description = fmt.Sprintf("Parallel YouTube search for %s tracks", collection.Type)
	searchSpan.SetTag("track_count", strconv.Itoa(len(collection.Tracks)))

	// Search YouTube for each track in parallel with concurrency limit
	results := make(chan searchResult, len(collection.Tracks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // Limit to 10 concurrent YouTube API searches

	for i, track := range collection.Tracks {
		wg.Add(1)
		go func(position int, track spotify.PlaylistTrackInfo) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			artistsStr := strings.Join(track.Artists, ", ")
			query := artistsStr + " - " + track.Title

			videos := youtube.Query(searchCtx, query)

			if len(videos) > 0 {
				results <- searchResult{
					Position: position,
					Video:    videos[0],
					Query:    query,
					Found:    true,
				}
			} else {
				log.Warnf("No YouTube results for %s track: %s", collection.Type, query)
				results <- searchResult{
					Position: position,
					Query:    query,
					Found:    false,
				}
			}
		}(i, track)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var searchResults []searchResult
	for result := range results {
		searchResults = append(searchResults, result)
	}

	// Sort by position to maintain collection order
	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Position < searchResults[j].Position
	})

	// Separate found and not found
	var foundVideos []youtube.VideoResponse
	var notFoundQueries []string
	for _, r := range searchResults {
		if r.Found {
			foundVideos = append(foundVideos, r.Video)
		} else {
			notFoundQueries = append(notFoundQueries, r.Query)
		}
	}

	// Check for duplicates in queue
	var videosToQueue []youtube.VideoResponse
	var duplicateCount int

	// Get current queue video IDs
	queueVideoIDs := make(map[string]bool)
	player.Queue.Mutex.Lock()
	for _, item := range player.Queue.Items {
		queueVideoIDs[item.Video.VideoID] = true
	}
	player.Queue.Mutex.Unlock()

	for _, video := range foundVideos {
		if queueVideoIDs[video.VideoID] {
			duplicateCount++
			log.Debugf("Skipping duplicate: %s (already in queue)", video.Title)
		} else {
			videosToQueue = append(videosToQueue, video)
			// Mark as in queue for subsequent checks
			queueVideoIDs[video.VideoID] = true
		}
	}

	// Finish search span
	searchSpan.Status = sentry.SpanStatusOK
	searchSpan.SetData("found_count", len(foundVideos))
	searchSpan.SetData("not_found_count", len(notFoundQueries))
	searchSpan.SetData("duplicate_count", duplicateCount)
	searchSpan.SetData("queued_count", len(videosToQueue))
	searchSpan.Finish()

	// Add breadcrumb for search results
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: category,
		Message:  fmt.Sprintf("Parallel YouTube search completed: %d found, %d not found, %d duplicates", len(foundVideos), len(notFoundQueries), duplicateCount),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			collection.Type + "_id": collection.ID,
			"tracks_requested":      len(collection.Tracks),
			"tracks_found":          len(foundVideos),
			"tracks_not_found":      len(notFoundQueries),
			"duplicates":            duplicateCount,
			"queued":                len(videosToQueue),
		},
	})

	displayName := collection.DisplayName()

	// Handle no videos found
	if len(videosToQueue) == 0 {
		if duplicateCount > 0 {
			manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("All tracks from **%s** are already in the queue!", displayName), true)
		} else {
			manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("Couldn't find any tracks from **%s** on YouTube.", displayName), true)
		}
		return
	}

	// Queue all found videos
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil

	for _, video := range videosToQueue {
		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID)
	}

	// Add breadcrumb for queued songs
	queueBreadcrumbData := map[string]interface{}{
		collection.Type + "_id":   collection.ID,
		collection.Type + "_name": collection.Name,
		"songs_queued":            len(videosToQueue),
	}
	if collection.Artist != "" {
		queueBreadcrumbData["artist"] = collection.Artist
	}

	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "queue",
		Message:  fmt.Sprintf("Queued %d songs from Spotify %s", len(videosToQueue), collection.Type),
		Level:    sentry.LevelInfo,
		Data:     queueBreadcrumbData,
	})

	// Build response message
	var responseBuilder strings.Builder
	responseBuilder.WriteString(fmt.Sprintf("**Queued %d tracks from \"%s\":**\n", len(videosToQueue), displayName))

	for i, video := range videosToQueue {
		responseBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, video.Title))
	}

	// Add notes about skipped tracks
	var notes []string
	if len(notFoundQueries) > 0 {
		notes = append(notes, fmt.Sprintf("%d tracks couldn't be found on YouTube", len(notFoundQueries)))
	}
	if duplicateCount > 0 {
		notes = append(notes, fmt.Sprintf("%d tracks were already in queue", duplicateCount))
	}
	if collection.TotalTracks > len(collection.Tracks) {
		notes = append(notes, fmt.Sprintf("showing first %d of %d total tracks", len(collection.Tracks), collection.TotalTracks))
	}

	if len(notes) > 0 {
		responseBuilder.WriteString("\n(")
		responseBuilder.WriteString(strings.Join(notes, ", "))
		responseBuilder.WriteString(")")
	}

	// Add first song note
	if firstSongQueued {
		responseBuilder.WriteString("\n\n(Playback will start shortly - first song needs to load)")
	}

	backupContent := responseBuilder.String()

	// Generate AI response with truncated track list to reduce token cost for large collections
	trackListPreview := backupContent
	if len(videosToQueue) > 10 {
		// Build a shorter preview for the AI prompt
		var previewBuilder strings.Builder
		previewBuilder.WriteString(fmt.Sprintf("**Queued %d tracks from \"%s\"** (showing first 5):\n", len(videosToQueue), displayName))
		for i := 0; i < 5 && i < len(videosToQueue); i++ {
			previewBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, videosToQueue[i].Title))
		}
		previewBuilder.WriteString("...")
		trackListPreview = previewBuilder.String()
	}

	aiPrompt := fmt.Sprintf("User %s queued a Spotify %s called '%s' with %d tracks. Here's a preview:\n%s",
		interaction.Member.User.Username,
		collection.Type,
		displayName,
		len(videosToQueue),
		trackListPreview)

	manager.SendFollowup(ctx, interaction, aiPrompt, backupContent, false)
}

func (manager *Manager) handleSpotifyPlaylist(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, playlistID string) {
	log.Debugf("Processing Spotify playlist: %s", playlistID)

	// Send immediate acknowledgment
	manager.SendFollowup(ctx, interaction, "", "Found a Spotify playlist, fetching tracks...", false)

	// Add breadcrumb for playlist fetch
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "spotify_playlist",
		Message:  "Fetching Spotify playlist: " + playlistID,
		Level:    sentry.LevelInfo,
	})

	// Fetch playlist tracks
	playlistResult, err := spotify.GetPlaylistTracks(ctx, playlistID, config.Config.Spotify.PlaylistLimit)
	if err != nil {
		log.Errorf("Error fetching Spotify playlist: %v", err)
		sentryhelper.CaptureException(ctx, err)

		// Provide user-friendly error messages
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not found"):
			manager.SendFollowup(ctx, interaction, "", "That playlist doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "private") || strings.Contains(errMsg, "not accessible"):
			manager.SendFollowup(ctx, interaction, "", "That playlist is private. Make it public or use track URLs instead.", true)
		case strings.Contains(errMsg, "empty"):
			manager.SendFollowup(ctx, interaction, "", "That playlist is empty.", true)
		case strings.Contains(errMsg, "no playable tracks"):
			manager.SendFollowup(ctx, interaction, "", "That playlist contains no playable tracks (only podcasts or episodes).", true)
		default:
			manager.SendFollowup(ctx, interaction, "", "Error fetching playlist from Spotify: "+errMsg, true)
		}
		return
	}

	// Process the collection using the shared handler
	manager.handleSpotifyCollection(ctx, interaction, player, SpotifyCollection{
		Type:        "playlist",
		ID:          playlistID,
		Name:        playlistResult.Name,
		Tracks:      playlistResult.Tracks,
		TotalTracks: playlistResult.TotalTracks,
	})
}

func (manager *Manager) handleSpotifyAlbum(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, albumID string) {
	log.Debugf("Processing Spotify album: %s", albumID)

	// Send immediate acknowledgment
	manager.SendFollowup(ctx, interaction, "", "Found a Spotify album, fetching tracks...", false)

	// Add breadcrumb for album fetch
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "spotify_album",
		Message:  "Fetching Spotify album: " + albumID,
		Level:    sentry.LevelInfo,
	})

	// Fetch album tracks
	albumResult, err := spotify.GetAlbumTracks(ctx, albumID)
	if err != nil {
		log.Errorf("Error fetching Spotify album: %v", err)
		sentryhelper.CaptureException(ctx, err)

		// Provide user-friendly error messages
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not found"):
			manager.SendFollowup(ctx, interaction, "", "That album doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "not accessible"):
			manager.SendFollowup(ctx, interaction, "", "That album is not accessible.", true)
		case strings.Contains(errMsg, "empty"):
			manager.SendFollowup(ctx, interaction, "", "That album is empty.", true)
		case strings.Contains(errMsg, "no playable tracks"):
			manager.SendFollowup(ctx, interaction, "", "That album contains no playable tracks.", true)
		default:
			manager.SendFollowup(ctx, interaction, "", "Error fetching album from Spotify: "+errMsg, true)
		}
		return
	}

	// Process the collection using the shared handler
	manager.handleSpotifyCollection(ctx, interaction, player, SpotifyCollection{
		Type:        "album",
		ID:          albumID,
		Name:        albumResult.Name,
		Artist:      albumResult.Artist,
		Tracks:      albumResult.Tracks,
		TotalTracks: albumResult.TotalTracks,
	})
}

func (manager *Manager) handleYouTubePlaylist(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, playlistID string) {
	log.Debugf("Processing YouTube playlist: %s", playlistID)

	// Send immediate acknowledgment
	manager.SendFollowup(ctx, interaction, "", "Found a YouTube playlist, fetching videos...", false)

	// Add Sentry breadcrumb
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "youtube_playlist",
		Message:  "Fetching YouTube playlist: " + playlistID,
		Level:    sentry.LevelInfo,
	})

	// Fetch playlist videos
	playlistResult, err := youtube.GetPlaylistVideos(ctx, playlistID, config.Config.Youtube.PlaylistLimit)
	if err != nil {
		log.Errorf("Error fetching YouTube playlist: %v", err)
		sentryhelper.CaptureException(ctx, err)

		// Provide user-friendly error messages
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not found"):
			manager.SendFollowup(ctx, interaction, "", "That playlist doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "private"):
			manager.SendFollowup(ctx, interaction, "", "That playlist is private.", true)
		case strings.Contains(errMsg, "empty"):
			manager.SendFollowup(ctx, interaction, "", "That playlist is empty or contains no accessible videos.", true)
		default:
			manager.SendFollowup(ctx, interaction, "", "Error fetching playlist from YouTube: "+errMsg, true)
		}
		return
	}

	// Convert playlist videos to VideoResponse slice
	videos := make([]youtube.VideoResponse, 0, len(playlistResult.Videos))
	for _, v := range playlistResult.Videos {
		videos = append(videos, youtube.VideoResponse{
			Title:   v.Title,
			VideoID: v.VideoID,
		})
	}

	// Check for duplicates against current queue
	var videosToQueue []youtube.VideoResponse
	var duplicateCount int

	queueVideoIDs := make(map[string]bool)
	player.Queue.Mutex.Lock()
	for _, item := range player.Queue.Items {
		queueVideoIDs[item.Video.VideoID] = true
	}
	player.Queue.Mutex.Unlock()

	for _, video := range videos {
		if queueVideoIDs[video.VideoID] {
			duplicateCount++
			log.Debugf("Skipping duplicate: %s (already in queue)", video.Title)
		} else {
			videosToQueue = append(videosToQueue, video)
			queueVideoIDs[video.VideoID] = true
		}
	}

	// Add Sentry breadcrumb for results
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "youtube_playlist",
		Message:  fmt.Sprintf("YouTube playlist processed: %d to queue, %d duplicates", len(videosToQueue), duplicateCount),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"playlist_id":    playlistID,
			"playlist_name":  playlistResult.Name,
			"videos_fetched": len(videos),
			"duplicates":     duplicateCount,
			"queued":         len(videosToQueue),
		},
	})

	// Handle no videos to queue
	if len(videosToQueue) == 0 {
		if duplicateCount > 0 {
			manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("All videos from **%s** are already in the queue!", playlistResult.Name), true)
		} else {
			manager.SendFollowup(ctx, interaction, "", fmt.Sprintf("Couldn't find any videos in **%s**.", playlistResult.Name), true)
		}
		return
	}

	// Queue all videos
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil

	for _, video := range videosToQueue {
		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID)
	}

	// Add Sentry breadcrumb for queued songs
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "queue",
		Message:  fmt.Sprintf("Queued %d videos from YouTube playlist", len(videosToQueue)),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"playlist_id":   playlistID,
			"playlist_name": playlistResult.Name,
			"songs_queued":  len(videosToQueue),
		},
	})

	// Build response message
	var responseBuilder strings.Builder
	responseBuilder.WriteString(fmt.Sprintf("**Queued %d videos from \"%s\":**\n", len(videosToQueue), playlistResult.Name))

	for i, video := range videosToQueue {
		responseBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, video.Title))
	}

	// Add notes about skipped videos
	var notes []string
	if duplicateCount > 0 {
		notes = append(notes, fmt.Sprintf("%d videos were already in queue", duplicateCount))
	}
	if playlistResult.TotalVideos > len(playlistResult.Videos) {
		notes = append(notes, fmt.Sprintf("showing first %d of %d total videos", len(playlistResult.Videos), playlistResult.TotalVideos))
	}

	if len(notes) > 0 {
		responseBuilder.WriteString("\n(")
		responseBuilder.WriteString(strings.Join(notes, ", "))
		responseBuilder.WriteString(")")
	}

	// Add first song note
	if firstSongQueued {
		responseBuilder.WriteString("\n\n(Playback will start shortly - first video needs to load)")
	}

	backupContent := responseBuilder.String()

	// Generate AI response with truncated track list to reduce token cost for large playlists
	trackListPreview := backupContent
	if len(videosToQueue) > 10 {
		var previewBuilder strings.Builder
		previewBuilder.WriteString(fmt.Sprintf("**Queued %d videos from \"%s\"** (showing first 5):\n", len(videosToQueue), playlistResult.Name))
		for i := 0; i < 5 && i < len(videosToQueue); i++ {
			previewBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, videosToQueue[i].Title))
		}
		previewBuilder.WriteString("...")
		trackListPreview = previewBuilder.String()
	}

	aiPrompt := fmt.Sprintf("User %s queued a YouTube playlist called '%s' with %d videos. Here's a preview:\n%s",
		interaction.Member.User.Username,
		playlistResult.Name,
		len(videosToQueue),
		trackListPreview)

	manager.SendFollowup(ctx, interaction, aiPrompt, backupContent, false)
}

func (manager *Manager) SendRequest(interaction *Interaction, content string, ephemeral bool) {
	payload := map[string]interface{}{
		"content": content,
	}

	if ephemeral {
		payload["flags"] = 64
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("Error marshalling payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/"+manager.AppID+"/"+interaction.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		log.Errorf("Error sending followup: %v", err)
	}
	defer resp.Body.Close()
}

func (manager *Manager) SendError(interaction *Interaction, content string, ephemeral bool) {
	manager.SendRequest(interaction, content, ephemeral)
}

func (manager *Manager) SendFollowup(ctx context.Context, interaction *Interaction, content string, backupContent string, ephemeral bool) {
	userName := interaction.Member.User.Username
	toSend := backupContent

	// pass in an empty string to skip the AI generation
	if content != "" {
		genText := gemini.GenerateResponse(ctx, "User: "+userName+"\nEvent: "+content)
		if genText != "" {
			toSend = genText
		}
	}
	manager.SendRequest(interaction, toSend, ephemeral)
}

func (manager *Manager) ParseInteraction(body []byte) (*Interaction, error) {
	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		log.Errorf("Error unmarshalling interaction: %v", err)
		return nil, err
	}
	return &interaction, nil
}

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

	if player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil {
		manager.SendFollowup(ctx, interaction, "The queue is empty and nothing is playing", "The queue is empty and nothing is playing", false)
	}

	formatted_queue := ""
	for i, video := range player.Queue.Items {
		formatted_queue += fmt.Sprintf("%d. %s\n", i+1, video.Video.Title)
	}

	if player.CurrentSong != nil {
		formatted_queue += fmt.Sprintf("\nNow playing: **%s**", *player.CurrentSong)
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

	if !player.Player.IsPlaying() && player.CurrentSong == nil {
		manager.SendFollowup(ctx, interaction, "user tried to skip but nothing is playing", "Nothing to skip", true)
		return
	}

	current := player.CurrentSong
	userName := interaction.Member.User.Username

	go player.Skip()

	next := player.GetNext()

	response := "@" + userName + " skipped **" + *current + "**"
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

func (manager *Manager) handleRemove(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "the queue is empty",
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
	msg := "Removed the song from the queue"
	userName := interaction.Member.User.Username

	if removed_title != "" {
		msg = "@" + userName + " removed **" + removed_title + "** from the queue"
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg,
		},
	}
}

func (manager *Manager) handleVolume(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	volume, err := strconv.Atoi(interaction.Data.Options[0].Value)
	if err != nil {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Invalid volume",
			},
		}
	}

	player.Player.SetVolume(volume)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "Volume set to " + interaction.Data.Options[0].Value,
		},
	}
}

// todo: need to assure the user is in the voice channel
func (manager *Manager) handlePause(ctx context.Context, interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if !player.Player.IsPlaying() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing",
			},
		}
	}

	go player.Player.Pause(ctx)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " paused the current song",
		},
	}
}

func (manager *Manager) handleResume(ctx context.Context, interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)
	player.LastActivityAt = time.Now()

	if !player.Player.IsPlaying() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing",
			},
		}
	}

	go player.Player.Resume(ctx)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " resumed the current song",
		},
	}
}

func (manager *Manager) handleShuffle(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	count := player.Shuffle()

	if count == 0 {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "the queue is empty, nothing to shuffle",
			},
		}
	}

	if count == 1 {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "only one song in the queue, nothing to shuffle",
			},
		}
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + interaction.Member.User.Username + " shuffled the queue (" + strconv.Itoa(count) + " songs)",
		},
	}
}

func (manager *Manager) handleRadio(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	enabled := player.ToggleRadio()

	var msg string
	if enabled {
		msg = "📻 Radio mode **enabled** — I'll automatically queue similar songs when the queue runs out"
	} else {
		msg = "📻 Radio mode **disabled**"
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg,
		},
	}
}

func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func parseLimitOption(interaction *Interaction) int {
	limit := 10
	for _, opt := range interaction.Data.Options {
		if opt.Name == "limit" {
			if v, err := strconv.Atoi(opt.Value); err == nil && v >= 1 && v <= 25 {
				limit = v
			}
		}
	}
	return limit
}

func (manager *Manager) handleHistory(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	limit := parseLimitOption(interaction)
	records, err := db.GetHistory(interaction.GuildID, limit)
	if err != nil {
		log.Errorf("Error fetching history: %v", err)
		return Response{Type: 4, Data: ResponseData{Content: "Failed to fetch history.", Flags: 64}}
	}

	if len(records) == 0 {
		return Response{Type: 4, Data: ResponseData{Content: "🎵 No songs played yet!"}}
	}

	var sb strings.Builder
	sb.WriteString("🎵 **Recently Played**\n\n")
	for i, r := range records {
		requester := r.RequestedByUsername
		if requester == "" {
			requester = "Unknown"
		}
		sb.WriteString(fmt.Sprintf("**%d.** %s\n　　↳ requested by **%s** · %s\n", i+1, r.Title, requester, formatRelativeTime(r.PlayedAt)))
	}

	content := sb.String()
	if len(content) > 1900 {
		content = content[:1900]
		if idx := strings.LastIndex(content, "\n"); idx > 0 {
			content = content[:idx]
		}
		content += "\n..."
	}

	return Response{Type: 4, Data: ResponseData{Content: content}}
}

func (manager *Manager) handleLeaderboard(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	limit := parseLimitOption(interaction)
	records, err := db.GetMostPlayed(interaction.GuildID, limit)
	if err != nil {
		log.Errorf("Error fetching leaderboard: %v", err)
		return Response{Type: 4, Data: ResponseData{Content: "Failed to fetch leaderboard.", Flags: 64}}
	}

	if len(records) == 0 {
		return Response{Type: 4, Data: ResponseData{Content: "🏆 No songs played yet!"}}
	}

	medals := []string{"🥇", "🥈", "🥉"}
	var sb strings.Builder
	sb.WriteString("🏆 **Most Played Songs**\n\n")
	for i, r := range records {
		prefix := fmt.Sprintf("**%d.**", i+1)
		if i < 3 {
			prefix = medals[i]
		}
		plays := "play"
		if r.PlayCount != 1 {
			plays = "plays"
		}
		sb.WriteString(fmt.Sprintf("%s %s\n　　↳ **%d** %s · last played %s\n", prefix, r.Title, r.PlayCount, plays, formatRelativeTime(r.LastPlayed)))
	}

	content := sb.String()
	if len(content) > 1900 {
		content = content[:1900]
		if idx := strings.LastIndex(content, "\n"); idx > 0 {
			content = content[:idx]
		}
		content += "\n..."
	}

	return Response{Type: 4, Data: ResponseData{Content: content}}
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

	shouldJoin := player.VoiceChannelID == nil ||
		player.VoiceConnection == nil ||
		(player.VoiceChannelID != nil &&
			player.IsEmpty() && !player.Player.IsPlaying() &&
			*player.VoiceChannelID != voiceState.ChannelID)

	if shouldJoin {
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

func (manager *Manager) HandleInteraction(interaction *Interaction) (response Response) {
	// Create transaction with cloned hub for scope isolation (breadcrumbs per-command)
	ctx, transaction := sentryhelper.StartCommandTransaction(
		context.Background(),
		interaction.Data.Name,
		interaction.GuildID,
		interaction.Member.User.ID,
	)

	// For sync responses (Type: 4), finish transaction when handler returns.
	// For async responses (Type: 5), the goroutine will finish the transaction.
	finishTransaction := true
	defer func() {
		if finishTransaction {
			transaction.Finish()
		}
	}()

	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Panic in command handling: %v", err)
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in command %s: %v", interaction.Data.Name, err))
			transaction.Status = sentry.SpanStatusInternalError
			response = Response{
				Type: 4,
				Data: ResponseData{
					Content: "An error occurred while processing your command",
					Flags:   64,
				},
			}
		}
	}()

	log.Debugf("Received command: %+v", interaction.Data.Name)

	// Configure scope on the cloned hub (isolated to this command)
	sentryhelper.ConfigureScope(ctx, func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{
			ID:       interaction.Member.User.ID,
			Username: interaction.Member.User.Username,
		})
		scope.SetContext("interaction", map[string]interface{}{
			"name":     interaction.Data.Name,
			"options":  interaction.Data.Options,
			"guild_id": interaction.GuildID,
			"user_id":  interaction.Member.User.ID,
			"username": interaction.Member.User.Username,
		})
	})

	// Always track the last text channel so we can send messages (e.g. radio announcements)
	if interaction.GuildID != "" && interaction.ChannelID != "" {
		player := manager.Controller.GetPlayer(interaction.GuildID)
		player.LastTextChannelID = interaction.ChannelID
	}

	switch interaction.Data.Name {
	case "ping":
		return manager.handlePing()
	case "help":
		finishTransaction = false // goroutine will finish
		return manager.handleHelp(ctx, transaction, interaction)
	case "queue", "play":
		finishTransaction = false // goroutine will finish
		return manager.handleQueue(ctx, transaction, interaction)
	case "view":
		finishTransaction = false // goroutine will finish
		return manager.handleView(ctx, transaction, interaction)
	case "remove":
		return manager.handleRemove(interaction)
	case "skip":
		finishTransaction = false // goroutine will finish
		return manager.handleSkip(ctx, transaction, interaction)
	case "pause", "stop":
		return manager.handlePause(ctx, interaction)
	case "volume":
		return manager.handleVolume(interaction)
	case "resume":
		return manager.handleResume(ctx, interaction)
	case "reset":
		finishTransaction = false // goroutine will finish
		return manager.handleReset(ctx, transaction, interaction)
	case "shuffle":
		return manager.handleShuffle(interaction)
	case "radio":
		return manager.handleRadio(interaction)
	case "history":
		return manager.handleHistory(interaction)
	case "leaderboard":
		return manager.handleLeaderboard(interaction)
	case "topsongs":
		finishTransaction = false
		go manager.handleTopSongs(ctx, transaction, interaction)
		return Response{Type: 5}
	// case "purge":
	// 	return manager.handlePurge(interaction)
	default:
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Sorry, I don't know how to handle this type of interaction",
				Flags:   64,
			},
		}
	}
}

func (manager *Manager) VerifyDiscordRequest(signature, timestamp string, body []byte) bool {
	pubKeyBytes, err := hex.DecodeString(manager.PublicKey)
	if err != nil {
		log.Errorf("Error decoding public key: %v", err)
		return false
	}

	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		log.Errorf("Error decoding signature: %v", err)
		return false
	}

	message := []byte(timestamp + string(body))
	return ed25519.Verify(pubKeyBytes, message, signatureBytes)
}
