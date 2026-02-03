package applemusic

import (
	"context"
	"errors"
	"strconv"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

// GetTrack fetches track metadata from Apple Music
func GetTrack(ctx context.Context, country, albumID, trackID string) (*TrackInfo, error) {
	log.Tracef("Fetching track from Apple Music: country=%s, album=%s, track=%s", country, albumID, trackID)

	// Start span for Sentry performance monitoring
	span := sentry.StartSpan(ctx, "applemusic.get_track")
	span.Description = "Get track from Apple Music via web scraping"
	span.SetTag("country", country)
	span.SetTag("track_id", trackID)
	span.SetTag("album_id", albumID)
	defer span.Finish()

	// Validate inputs
	if country == "" {
		country = "us" // Default to US if not specified
	}
	if albumID == "" || trackID == "" {
		err := errors.New("albumID and trackID are required")
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInvalidArgument
		return nil, err
	}

	trackInfo, err := scrapeTrackInfo(ctx, country, albumID, trackID)
	if err != nil {
		log.Errorf("Failed to fetch Apple Music track: %v", err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	log.Debugf("Successfully fetched Apple Music track: '%s' by %v", trackInfo.Title, trackInfo.Artists)
	span.Status = sentry.SpanStatusOK
	span.SetData("track_title", trackInfo.Title)
	span.SetData("track_artists", trackInfo.Artists)
	span.SetData("track_album", trackInfo.Album)

	return trackInfo, nil
}

// GetAlbumTracks fetches album metadata and tracks from Apple Music
func GetAlbumTracks(ctx context.Context, country, albumID string) (*AlbumResult, error) {
	log.Tracef("Fetching album from Apple Music: country=%s, album=%s", country, albumID)

	span := sentry.StartSpan(ctx, "applemusic.get_album_tracks")
	span.Description = "Get album tracks from Apple Music via web scraping"
	span.SetTag("country", country)
	span.SetTag("album_id", albumID)
	defer span.Finish()

	if country == "" {
		country = "us"
	}
	if albumID == "" {
		err := errors.New("albumID is required")
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInvalidArgument
		return nil, err
	}

	albumResult, err := scrapeAlbumTracks(ctx, country, albumID)
	if err != nil {
		log.Errorf("Failed to fetch Apple Music album: %v", err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	if len(albumResult.Tracks) == 0 {
		err := errors.New("album has no playable tracks")
		log.Warnf("Album %s has no tracks", albumID)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusNotFound
		return nil, err
	}

	log.Debugf("Successfully fetched Apple Music album: '%s' by %s (%d tracks)",
		albumResult.Name, albumResult.Artist, len(albumResult.Tracks))
	span.Status = sentry.SpanStatusOK
	span.SetData("album_name", albumResult.Name)
	span.SetData("artist", albumResult.Artist)
	span.SetData("tracks_count", len(albumResult.Tracks))
	span.SetData("total_tracks", albumResult.TotalTracks)

	return albumResult, nil
}

// GetPlaylistTracks fetches playlist metadata and tracks from Apple Music
func GetPlaylistTracks(ctx context.Context, country, playlistID string, limit int) (*PlaylistResult, error) {
	log.Tracef("Fetching playlist from Apple Music: country=%s, playlist=%s, limit=%d",
		country, playlistID, limit)

	span := sentry.StartSpan(ctx, "applemusic.get_playlist_tracks")
	span.Description = "Get playlist tracks from Apple Music via web scraping"
	span.SetTag("country", country)
	span.SetTag("playlist_id", playlistID)
	span.SetTag("limit", strconv.Itoa(limit))
	defer span.Finish()

	if country == "" {
		country = "us"
	}
	if playlistID == "" {
		err := errors.New("playlistID is required")
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInvalidArgument
		return nil, err
	}
	if limit <= 0 {
		limit = 15
	}

	playlistResult, err := scrapePlaylistTracks(ctx, country, playlistID, limit)
	if err != nil {
		log.Errorf("Failed to fetch Apple Music playlist: %v", err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	if len(playlistResult.Tracks) == 0 {
		err := errors.New("playlist has no playable tracks")
		log.Warnf("Playlist %s has no tracks", playlistID)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusNotFound
		return nil, err
	}

	log.Debugf("Successfully fetched Apple Music playlist: '%s' (%d tracks)",
		playlistResult.Name, len(playlistResult.Tracks))
	span.Status = sentry.SpanStatusOK
	span.SetData("playlist_name", playlistResult.Name)
	span.SetData("tracks_count", len(playlistResult.Tracks))
	span.SetData("total_tracks", playlistResult.TotalTracks)

	return playlistResult, nil
}

// GetArtistTopSongs fetches top songs for an artist from Apple Music
// Phase 3 - Not implemented yet
func GetArtistTopSongs(ctx context.Context, country, artistID string) ([]TrackInfo, error) {
	log.Warnf("GetArtistTopSongs not yet implemented for artist %s", artistID)
	return nil, errors.New("artist top songs not implemented yet")
}
