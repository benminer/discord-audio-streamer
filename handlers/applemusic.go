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

	"beatbot/applemusic"
	"beatbot/controller"
	"beatbot/sentryhelper"
	"beatbot/youtube"
)

// AppleMusicCollection represents an album or playlist for processing
type AppleMusicCollection struct {
	Type        string // "album" or "playlist"
	ID          string
	Name        string
	Artist      string // Only for albums
	Tracks      []applemusic.PlaylistTrackInfo
	TotalTracks int
}

func (manager *Manager) handleAppleMusicTrack(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, req applemusic.AppleMusicRequest) {
	log.Debugf("Processing Apple Music track: country=%s, album=%s, track=%s", req.Country, req.AlbumID, req.TrackID)

	// Use default country if not specified
	country := req.Country
	if country == "" {
		country = "us"
	}

	// Fetch track metadata from Apple Music
	trackInfo, err := applemusic.GetTrack(ctx, country, req.AlbumID, req.TrackID)
	if err != nil {
		log.Errorf("Error fetching Apple Music track: %v", err)
		sentryhelper.CaptureException(ctx, err)

		// Provide user-friendly error messages
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "404"):
			manager.SendFollowup(ctx, interaction, "", "That track doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "403"):
			manager.SendFollowup(ctx, interaction, "", "That content is not accessible.", true)
		default:
			manager.SendFollowup(ctx, interaction, "", "Error fetching from Apple Music: "+errMsg, true)
		}
		return
	}

	artistsStr := strings.Join(trackInfo.Artists, ", ")
	youtubeQuery := artistsStr + " - " + trackInfo.Title
	log.Debugf("Converted Apple Music track '%s' by '%s' to YouTube query: %s", trackInfo.Title, artistsStr, youtubeQuery)

	manager.SendFollowup(ctx, interaction,
		"",
		fmt.Sprintf("Found **%s** by **%s** on Apple Music, searching YouTube...", trackInfo.Title, artistsStr),
		false)

	videos := youtube.Query(ctx, youtubeQuery)
	if len(videos) == 0 {
		log.Warnf("No YouTube results found for Apple Music track: %s", youtubeQuery)
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
}

// handleAppleMusicAlbum processes an Apple Music album
func (manager *Manager) handleAppleMusicAlbum(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, req applemusic.AppleMusicRequest) {
	log.Debugf("Processing Apple Music album: country=%s, album=%s", req.Country, req.AlbumID)

	manager.SendFollowup(ctx, interaction, "", "Found an Apple Music album, fetching tracks...", false)

	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "applemusic_album",
		Message:  "Fetching Apple Music album: " + req.AlbumID,
		Level:    sentry.LevelInfo,
	})

	country := req.Country
	if country == "" {
		country = "us"
	}

	albumResult, err := applemusic.GetAlbumTracks(ctx, country, req.AlbumID)
	if err != nil {
		log.Errorf("Error fetching Apple Music album: %v", err)
		sentryhelper.CaptureException(ctx, err)

		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "404"):
			manager.SendFollowup(ctx, interaction, "", "That album doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "403"):
			manager.SendFollowup(ctx, interaction, "", "That album is not accessible.", true)
		case strings.Contains(errMsg, "no playable tracks"):
			manager.SendFollowup(ctx, interaction, "", "That album contains no playable tracks.", true)
		default:
			manager.SendFollowup(ctx, interaction, "", "Error fetching album from Apple Music: "+errMsg, true)
		}
		return
	}

	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "applemusic_album",
		Message:  fmt.Sprintf("Fetched %d tracks from album '%s' by %s", len(albumResult.Tracks), albumResult.Name, albumResult.Artist),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"album_id":     req.AlbumID,
			"album_name":   albumResult.Name,
			"artist":       albumResult.Artist,
			"track_count":  len(albumResult.Tracks),
			"total_tracks": albumResult.TotalTracks,
		},
	})

	manager.handleAppleMusicCollection(ctx, interaction, player, AppleMusicCollection{
		Type:        "album",
		ID:          req.AlbumID,
		Name:        albumResult.Name,
		Artist:      albumResult.Artist,
		Tracks:      albumResult.Tracks,
		TotalTracks: albumResult.TotalTracks,
	})
}

// handleAppleMusicPlaylist processes an Apple Music playlist
func (manager *Manager) handleAppleMusicPlaylist(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, req applemusic.AppleMusicRequest) {
	log.Debugf("Processing Apple Music playlist: country=%s, playlist=%s", req.Country, req.PlaylistID)

	manager.SendFollowup(ctx, interaction, "", "Found an Apple Music playlist, fetching tracks...", false)

	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "applemusic_playlist",
		Message:  "Fetching Apple Music playlist: " + req.PlaylistID,
		Level:    sentry.LevelInfo,
	})

	country := req.Country
	if country == "" {
		country = "us"
	}

	limit := 15

	playlistResult, err := applemusic.GetPlaylistTracks(ctx, country, req.PlaylistID, limit)
	if err != nil {
		log.Errorf("Error fetching Apple Music playlist: %v", err)
		sentryhelper.CaptureException(ctx, err)

		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "404"):
			manager.SendFollowup(ctx, interaction, "", "That playlist doesn't exist or has been deleted.", true)
		case strings.Contains(errMsg, "403") || strings.Contains(errMsg, "private"):
			manager.SendFollowup(ctx, interaction, "", "That playlist is private. Make it public or use track URLs instead.", true)
		case strings.Contains(errMsg, "no playable tracks"):
			manager.SendFollowup(ctx, interaction, "", "That playlist contains no playable tracks.", true)
		default:
			manager.SendFollowup(ctx, interaction, "", "Error fetching playlist from Apple Music: "+errMsg, true)
		}
		return
	}

	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "applemusic_playlist",
		Message:  fmt.Sprintf("Fetched %d tracks from playlist '%s'", len(playlistResult.Tracks), playlistResult.Name),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"playlist_id":   req.PlaylistID,
			"playlist_name": playlistResult.Name,
			"track_count":   len(playlistResult.Tracks),
			"total_tracks":  playlistResult.TotalTracks,
		},
	})

	manager.handleAppleMusicCollection(ctx, interaction, player, AppleMusicCollection{
		Type:        "playlist",
		ID:          req.PlaylistID,
		Name:        playlistResult.Name,
		Tracks:      playlistResult.Tracks,
		TotalTracks: playlistResult.TotalTracks,
	})
}

// handleAppleMusicCollection processes tracks from an Apple Music album or playlist
func (manager *Manager) handleAppleMusicCollection(ctx context.Context, interaction *Interaction, player *controller.GuildPlayer, collection AppleMusicCollection) {
	category := "applemusic_" + collection.Type

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

	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	searchSpan := sentry.StartSpan(searchCtx, "youtube.parallel_search")
	searchSpan.Description = fmt.Sprintf("Parallel YouTube search for Apple Music %s tracks", collection.Type)
	searchSpan.SetTag("track_count", strconv.Itoa(len(collection.Tracks)))

	results := make(chan searchResult, len(collection.Tracks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i, track := range collection.Tracks {
		wg.Add(1)
		go func(position int, track applemusic.PlaylistTrackInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

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
				log.Warnf("No YouTube results for Apple Music %s track: %s", collection.Type, query)
				results <- searchResult{
					Position: position,
					Query:    query,
					Found:    false,
				}
			}
		}(i, track)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var searchResults []searchResult
	for result := range results {
		searchResults = append(searchResults, result)
	}

	searchSpan.Finish()

	sort.Slice(searchResults, func(i, j int) bool {
		return searchResults[i].Position < searchResults[j].Position
	})

	var foundVideos []youtube.VideoResponse
	var notFoundQueries []string
	for _, r := range searchResults {
		if r.Found {
			foundVideos = append(foundVideos, r.Video)
		} else {
			notFoundQueries = append(notFoundQueries, r.Query)
		}
	}

	log.Debugf("Found %d/%d tracks on YouTube for Apple Music %s '%s'",
		len(foundVideos), len(collection.Tracks), collection.Type, collection.Name)

	if len(foundVideos) == 0 {
		manager.SendFollowup(ctx, interaction, "",
			fmt.Sprintf("Couldn't find any tracks from **%s** on YouTube", collection.Name),
			true)
		return
	}

	var collectionDescription string
	if collection.Type == "album" {
		collectionDescription = fmt.Sprintf("**%s** by **%s**", collection.Name, collection.Artist)
	} else {
		collectionDescription = fmt.Sprintf("**%s**", collection.Name)
	}

	summaryMsg := fmt.Sprintf("Adding %d/%d tracks from %s %s to the queue",
		len(foundVideos), collection.TotalTracks, collection.Type, collectionDescription)

	if len(notFoundQueries) > 0 {
		summaryMsg += fmt.Sprintf("\n\n⚠️ Couldn't find %d tracks on YouTube", len(notFoundQueries))
	}

	manager.SendFollowup(ctx, interaction, "", summaryMsg, false)

	for _, video := range foundVideos {
		player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID, nil)
	}

	log.Infof("Queued %d tracks from Apple Music %s '%s' for user %s",
		len(foundVideos), collection.Type, collection.Name, interaction.Member.User.ID)
}
