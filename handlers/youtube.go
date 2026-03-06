package handlers

import (
	"context"
	"fmt"
	"strings"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/controller"
	"beatbot/sentryhelper"
	"beatbot/youtube"
)

// fallbackSlice returns up to n items from videos[1:], safe for any slice length.
// Used to build the FallbackVideos list for age-restriction retry logic.
func fallbackSlice(videos []youtube.VideoResponse, n int) []youtube.VideoResponse {
	if len(videos) <= 1 {
		return nil
	}
	rest := videos[1:]
	if len(rest) > n {
		rest = rest[:n]
	}
	return rest
}

// searchResult holds the result of a single YouTube search for collection processing
type searchResult struct {
	Position int
	Video    youtube.VideoResponse
	Query    string
	Found    bool
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
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.GetCurrentSong() == nil

	for _, video := range videosToQueue {
		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID, nil)
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
