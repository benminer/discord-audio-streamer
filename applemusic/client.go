package applemusic

import (
	"context"
	"errors"

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
// Phase 2 - Not implemented yet
func GetAlbumTracks(ctx context.Context, country, albumID string) (*AlbumResult, error) {
	log.Warnf("GetAlbumTracks not yet implemented for album %s", albumID)
	return nil, errors.New("album parsing not implemented yet")
}

// GetPlaylistTracks fetches playlist metadata and tracks from Apple Music
// Phase 2 - Not implemented yet
func GetPlaylistTracks(ctx context.Context, country, playlistID string, limit int) (*PlaylistResult, error) {
	log.Warnf("GetPlaylistTracks not yet implemented for playlist %s", playlistID)
	return nil, errors.New("playlist parsing not implemented yet")
}

// GetArtistTopSongs fetches top songs for an artist from Apple Music
// Phase 3 - Not implemented yet
func GetArtistTopSongs(ctx context.Context, country, artistID string) ([]TrackInfo, error) {
	log.Warnf("GetArtistTopSongs not yet implemented for artist %s", artistID)
	return nil, errors.New("artist top songs not implemented yet")
}
