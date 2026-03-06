package handlers

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/controller"
	"beatbot/sentryhelper"
	"beatbot/spotify"
	"beatbot/youtube"
)

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
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.GetCurrentSong() == nil

	for _, video := range videosToQueue {
		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID, nil)
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
