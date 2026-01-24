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
func (manager *Manager) QueryAndQueue(interaction *Interaction) {
	log.Debugf("Querying and queuing: %+v", interaction.Member.User.ID)
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		log.Errorf("Error getting voice state: %v", err)
		sentry.CaptureException(err)
		manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)
	player.LastTextChannelID = interaction.ChannelID

	if voiceState == nil {
		manager.SendFollowup(interaction, "The user is not in a voice channel and trying to play a song", "Hey dummy, join a voice channel first", true)
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
				manager.SendFollowup(interaction, "You gotta join a voice channel first!", "Error joining voice channel: "+errStr, true)
				return
			}
			sentry.CaptureException(err)
			manager.SendError(interaction, "Error joining voice channel: "+errStr, true)
			return
		}
	}

	query := interaction.Data.Options[0].Value

	if strings.HasPrefix(query, "https://open.spotify.com/") {
		log.Debugf("Detected Spotify URL: %s", query)

		// Check if Spotify is enabled
		if !config.Config.Spotify.Enabled {
			manager.SendFollowup(interaction, "", "Spotify integration is not enabled. Ask the bot admin to set SPOTIFY_ENABLED=true.", true)
			return
		}

		spotifyReq, err := spotify.ParseSpotifyURL(query)
		if err != nil {
			log.Errorf("Error parsing Spotify URL: %v", err)
			sentry.CaptureException(err)
			manager.SendError(interaction, "Invalid Spotify URL: "+err.Error(), true)
			return
		}

		// Handle playlist URLs
		if spotifyReq.PlaylistID != "" {
			manager.handleSpotifyPlaylist(interaction, player, spotifyReq.PlaylistID)
			return
		}

		// Handle artist URLs (still coming soon)
		if spotifyReq.ArtistID != "" {
			manager.SendFollowup(interaction, "", "Spotify artists are coming soon! For now, please use track URLs, playlist URLs, or search queries.", true)
			return
		}

		if spotifyReq.TrackID == "" {
			manager.SendFollowup(interaction, "", "Invalid Spotify URL. Please use a track or playlist URL.", true)
			return
		}

		log.Tracef("Fetching Spotify track: %s", spotifyReq.TrackID)
		trackInfo, err := spotify.GetTrack(spotifyReq.TrackID)
		if err != nil {
			log.Errorf("Error fetching Spotify track: %v", err)
			sentry.CaptureException(err)
			manager.SendError(interaction, "Error fetching track from Spotify: "+err.Error(), true)
			return
		}

		artistsStr := strings.Join(trackInfo.Artists, ", ")
		youtubeQuery := artistsStr + " - " + trackInfo.Title
		log.Debugf("Converted Spotify track '%s' by '%s' to YouTube query: %s", trackInfo.Title, artistsStr, youtubeQuery)

		manager.SendFollowup(interaction,
			"",
			fmt.Sprintf("Found **%s** by **%s** on Spotify, searching YouTube...", trackInfo.Title, artistsStr),
			false)

		videos := youtube.Query(youtubeQuery)
		if len(videos) == 0 {
			log.Warnf("No YouTube results found for Spotify track: %s", youtubeQuery)
			manager.SendFollowup(interaction,
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

		manager.SendFollowup(interaction, followUpMessage, followUpMessage, false)

		player.Add(video, interaction.Member.User.ID, interaction.Token, manager.AppID)
		return
	}

	videoID := youtube.ParseYoutubeUrl(query)

	var video youtube.VideoResponse

	// user passed in a youtube url
	if videoID != "" {
		videoResponse, err := youtube.GetVideoByID(videoID)
		if err != nil {
			sentry.CaptureException(err)
			manager.SendError(interaction, "Error getting video stream: "+err.Error(), true)
			return
		}

		video = videoResponse
	} else {
		videos := youtube.Query(query)

		if len(videos) == 0 {
			manager.SendFollowup(interaction, "There wasn't anything found for "+query, "No videos found for the given query", true)
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

	manager.SendFollowup(interaction, followUpMessage, followUpMessage, false)
	player.Add(video, interaction.Member.User.ID, interaction.Token, manager.AppID)
}

// searchResult holds the result of a single YouTube search for playlist processing
type searchResult struct {
	Position int
	Video    youtube.VideoResponse
	Query    string
	Found    bool
}

func (manager *Manager) handleSpotifyPlaylist(interaction *Interaction, player *controller.GuildPlayer, playlistID string) {
	log.Debugf("Processing Spotify playlist: %s", playlistID)

	// Send immediate acknowledgment
	manager.SendFollowup(interaction, "", "Found a Spotify playlist, fetching tracks...", false)

	// Add breadcrumb for playlist fetch
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "spotify_playlist",
		Message:  "Fetching Spotify playlist: " + playlistID,
		Level:    sentry.LevelInfo,
	})

	// Fetch playlist tracks
	playlistResult, err := spotify.GetPlaylistTracks(playlistID, config.Config.Spotify.PlaylistLimit)
	if err != nil {
		log.Errorf("Error fetching Spotify playlist: %v", err)
		sentry.CaptureException(err)

		// Provide user-friendly error messages
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not found"):
			manager.SendFollowup(interaction, "", "That playlist doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "private") || strings.Contains(errMsg, "not accessible"):
			manager.SendFollowup(interaction, "", "That playlist is private. Make it public or use track URLs instead.", true)
		case strings.Contains(errMsg, "empty"):
			manager.SendFollowup(interaction, "", "That playlist is empty.", true)
		case strings.Contains(errMsg, "no playable tracks"):
			manager.SendFollowup(interaction, "", "That playlist contains no playable tracks (only podcasts or episodes).", true)
		default:
			manager.SendFollowup(interaction, "", "Error fetching playlist from Spotify: "+errMsg, true)
		}
		return
	}

	// Add breadcrumb for successful fetch
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "spotify_playlist",
		Message:  fmt.Sprintf("Fetched %d tracks from playlist '%s'", len(playlistResult.Tracks), playlistResult.Name),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"playlist_id":   playlistID,
			"playlist_name": playlistResult.Name,
			"track_count":   len(playlistResult.Tracks),
			"total_tracks":  playlistResult.TotalTracks,
		},
	})

	// Use context with timeout for graceful cancellation if Discord interaction times out
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start parallel YouTube search span
	searchSpan := sentry.StartSpan(ctx, "youtube.parallel_search")
	searchSpan.Description = "Parallel YouTube search for playlist tracks"
	searchSpan.SetTag("track_count", strconv.Itoa(len(playlistResult.Tracks)))

	// Search YouTube for each track in parallel with concurrency limit
	results := make(chan searchResult, len(playlistResult.Tracks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // Limit to 10 concurrent YouTube API searches

	for i, track := range playlistResult.Tracks {
		wg.Add(1)
		go func(position int, track spotify.PlaylistTrackInfo) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			artistsStr := strings.Join(track.Artists, ", ")
			query := artistsStr + " - " + track.Title

			videos := youtube.Query(query)

			if len(videos) > 0 {
				results <- searchResult{
					Position: position,
					Video:    videos[0],
					Query:    query,
					Found:    true,
				}
			} else {
				log.Warnf("No YouTube results for playlist track: %s", query)
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

	// Sort by position to maintain playlist order
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
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "spotify_playlist",
		Message:  fmt.Sprintf("Parallel YouTube search completed: %d found, %d not found, %d duplicates", len(foundVideos), len(notFoundQueries), duplicateCount),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"playlist_id":      playlistID,
			"tracks_requested": len(playlistResult.Tracks),
			"tracks_found":     len(foundVideos),
			"tracks_not_found": len(notFoundQueries),
			"duplicates":       duplicateCount,
			"queued":           len(videosToQueue),
		},
	})

	// Handle no videos found
	if len(videosToQueue) == 0 {
		if duplicateCount > 0 {
			manager.SendFollowup(interaction, "", fmt.Sprintf("All tracks from **%s** are already in the queue!", playlistResult.Name), true)
		} else {
			manager.SendFollowup(interaction, "", fmt.Sprintf("Couldn't find any tracks from **%s** on YouTube.", playlistResult.Name), true)
		}
		return
	}

	// Queue all found videos
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil

	for _, video := range videosToQueue {
		player.Add(video, interaction.Member.User.ID, interaction.Token, manager.AppID)
	}

	// Add breadcrumb for queued songs
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "queue",
		Message:  fmt.Sprintf("Queued %d songs from Spotify playlist", len(videosToQueue)),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"playlist_id":   playlistID,
			"playlist_name": playlistResult.Name,
			"songs_queued":  len(videosToQueue),
		},
	})

	// Build response message
	var responseBuilder strings.Builder
	responseBuilder.WriteString(fmt.Sprintf("**Queued %d tracks from \"%s\":**\n", len(videosToQueue), playlistResult.Name))

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
	if playlistResult.TotalTracks > len(playlistResult.Tracks) {
		notes = append(notes, fmt.Sprintf("showing first %d of %d total tracks", len(playlistResult.Tracks), playlistResult.TotalTracks))
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

	// Generate AI response with truncated track list to reduce token cost for large playlists
	trackListPreview := backupContent
	if len(videosToQueue) > 10 {
		// Build a shorter preview for the AI prompt
		var previewBuilder strings.Builder
		previewBuilder.WriteString(fmt.Sprintf("**Queued %d tracks from \"%s\"** (showing first 5):\n", len(videosToQueue), playlistResult.Name))
		for i := 0; i < 5 && i < len(videosToQueue); i++ {
			previewBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, videosToQueue[i].Title))
		}
		previewBuilder.WriteString("...")
		trackListPreview = previewBuilder.String()
	}

	aiPrompt := fmt.Sprintf("User %s queued a Spotify playlist called '%s' with %d tracks. Here's a preview:\n%s",
		interaction.Member.User.Username,
		playlistResult.Name,
		len(videosToQueue),
		trackListPreview)

	manager.SendFollowup(interaction, aiPrompt, backupContent, false)
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

func (manager *Manager) SendFollowup(interaction *Interaction, content string, backupContent string, ephemeral bool) {
	userName := interaction.Member.User.Username
	toSend := backupContent

	// pass in an empty string to skip the AI generation
	if content != "" {
		genText := gemini.GenerateResponse("User: " + userName + "\nEvent: " + content)
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

func (manager *Manager) onHelp(interaction *Interaction) {
	response := gemini.GenerateHelpfulResponse("(user issued the help command, return a nicely formatted help menu)")
	if response == "" {
		response = `**Music Control:**
/play (or /queue) - Queue a song. Takes a search query, YouTube URL, or Spotify track URL
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

func (manager *Manager) handleHelp(interaction *Interaction) Response {
	go manager.onHelp(interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleQueue(interaction *Interaction) Response {
	go manager.QueryAndQueue(interaction)

	return Response{
		Type: 5,
	}
}

func (manager *Manager) onView(interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil {
		manager.SendFollowup(interaction, "The queue is empty and nothing is playing", "The queue is empty and nothing is playing", false)
	}

	formatted_queue := ""
	for i, video := range player.Queue.Items {
		formatted_queue += fmt.Sprintf("%d. %s\n", i+1, video.Video.Title)
	}

	if player.CurrentSong != nil {
		formatted_queue += fmt.Sprintf("\nNow playing: **%s**", *player.CurrentSong)
	}

	manager.SendFollowup(interaction, "", formatted_queue, false)
}

func (manager *Manager) handleView(interaction *Interaction) Response {
	go manager.onView(interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) onSkip(interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if !player.Player.IsPlaying() && player.CurrentSong == nil {
		manager.SendFollowup(interaction, "user tried to skip but nothing is playing", "Nothing to skip", true)
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

	manager.SendFollowup(interaction, response, response, false)
}

func (manager *Manager) handlePurge(interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go player.Clear()

	manager.SendFollowup(interaction, "Queue purged", "Queue purged", false)
}

func (manager *Manager) handleSkip(interaction *Interaction) Response {
	go manager.onSkip(interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleReset(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go player.Reset(&controller.GuildQueueItemInteraction{
		UserID:           interaction.Member.User.ID,
		InteractionToken: interaction.Token,
		AppID:            manager.AppID,
	})

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
func (manager *Manager) handlePause(interaction *Interaction) Response {
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

	go player.Player.Pause()

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " paused the current song",
		},
	}
}

func (manager *Manager) handleResume(interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)
	player.LastTextChannelID = interaction.ChannelID
	player.LastActivityAt = time.Now()

	if !player.Player.IsPlaying() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing",
			},
		}
	}

	go player.Player.Resume()

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " resumed the current song",
		},
	}
}

func (manager *Manager) HandleInteraction(interaction *Interaction) (response Response) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Panic in command handling: %v", err)
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

	sentry.CurrentHub().ConfigureScope(func(scope *sentry.Scope) {
		// Set user context for better Sentry user tracking
		scope.SetUser(sentry.User{
			ID:       interaction.Member.User.ID,
			Username: interaction.Member.User.Username,
		})

		// Set interaction context (guild name comes from breadcrumbs in controller.go)
		scope.SetContext("interaction", map[string]interface{}{
			"name":     interaction.Data.Name,
			"options":  interaction.Data.Options,
			"guild_id": interaction.GuildID,
			"user_id":  interaction.Member.User.ID,
			"username": interaction.Member.User.Username,
		})

		// Set tags for filtering
		scope.SetTag("guild_id", interaction.GuildID)
		scope.SetTag("command", interaction.Data.Name)
	})

	switch interaction.Data.Name {
	case "ping":
		return manager.handlePing()
	case "help":
		return manager.handleHelp(interaction)
	case "queue", "play":
		return manager.handleQueue(interaction)
	case "view":
		return manager.handleView(interaction)
	case "remove":
		return manager.handleRemove(interaction)
	case "skip":
		return manager.handleSkip(interaction)
	case "pause", "stop":
		return manager.handlePause(interaction)
	case "volume":
		return manager.handleVolume(interaction)
	case "resume":
		return manager.handleResume(interaction)
	case "reset":
		return manager.handleReset(interaction)
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
