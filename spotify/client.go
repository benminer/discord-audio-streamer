package spotify

import (
	"context"
	"errors"
	"os"
	"strings"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	spotifyclient "github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
)

var Spotify *spotifyclient.Client

type SpotifyRequest struct {
	TrackID    string
	PlaylistID string
	ArtistID   string
}

type TrackInfo struct {
	Title   string
	Artists []string
}

type PlaylistTrackInfo struct {
	TrackInfo
	Position int
}

type PlaylistResult struct {
	Name        string
	Tracks      []PlaylistTrackInfo
	TotalTracks int
}

func NewSpotifyClient() error {
	ctx := context.Background()
	config := &clientcredentials.Config{
		ClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		ClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		TokenURL:     spotifyauth.TokenURL,
	}
	token, err := config.Token(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}

	httpClient := spotifyauth.New().Client(ctx, token)
	client := spotifyclient.New(httpClient)
	Spotify = client
	return nil
}

func Search(query string) (spotifyclient.SearchResult, error) {
	ctx := context.Background()

	// Start span for Spotify search
	span := sentry.StartSpan(ctx, "spotify.search")
	span.Description = "Search Spotify API"
	span.SetTag("query", query)
	defer span.Finish()

	results, err := Spotify.Search(ctx, query, spotifyclient.SearchTypeTrack)
	if err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return spotifyclient.SearchResult{}, err
	}

	span.Status = sentry.SpanStatusOK
	return *results, nil
}

func GetTrack(trackID string) (*TrackInfo, error) {
	log.Tracef("Fetching track from Spotify API: %s", trackID)
	ctx := context.Background()

	// Start span for Spotify API call
	span := sentry.StartSpan(ctx, "spotify.get_track")
	span.Description = "Get track from Spotify API"
	span.SetTag("track_id", trackID)
	defer span.Finish()

	track, err := Spotify.GetTrack(ctx, spotifyclient.ID(trackID))
	if err != nil {
		log.Errorf("Failed to fetch Spotify track %s: %v", trackID, err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	artists := []string{}
	for _, artist := range track.Artists {
		artists = append(artists, artist.Name)
	}

	log.Debugf("Successfully fetched Spotify track: '%s' by %v", track.Name, artists)
	span.Status = sentry.SpanStatusOK
	return &TrackInfo{
		Title:   track.Name,
		Artists: artists,
	}, nil
}

func GetArtistTopSongs(artistID string) ([]string, error) {
	ctx := context.Background()

	// Start span for Spotify artist top tracks
	span := sentry.StartSpan(ctx, "spotify.get_artist_top_songs")
	span.Description = "Get artist top songs from Spotify API"
	span.SetTag("artist_id", artistID)
	defer span.Finish()

	results, err := Spotify.GetArtistsTopTracks(ctx, spotifyclient.ID(artistID), "US")
	if err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	names := []string{}
	for _, track := range results {
		names = append(names, track.Name)
	}

	span.Status = sentry.SpanStatusOK
	return names, nil
}

func GetPlaylistTracks(playlistID string, limit int) (*PlaylistResult, error) {
	log.Tracef("Fetching playlist tracks from Spotify API: %s (limit: %d)", playlistID, limit)
	ctx := context.Background()

	span := sentry.StartSpan(ctx, "spotify.get_playlist_tracks")
	span.Description = "Get playlist tracks from Spotify API"
	span.SetTag("playlist_id", playlistID)
	defer span.Finish()

	// Fetch the playlist to get name and total track count
	playlist, err := Spotify.GetPlaylist(ctx, spotifyclient.ID(playlistID))
	if err != nil {
		log.Errorf("Failed to fetch Spotify playlist %s: %v", playlistID, err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError

		// Note: zmb3/spotify client doesn't provide typed errors, so we parse error strings.
		// This is fragile but necessary for user-friendly error messages.
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "Not Found") {
			return nil, errors.New("playlist not found")
		}
		if strings.Contains(errStr, "403") || strings.Contains(errStr, "Forbidden") {
			return nil, errors.New("playlist is private or not accessible")
		}
		return nil, err
	}

	playlistName := playlist.Name
	totalTracks := int(playlist.Tracks.Total)

	if totalTracks == 0 {
		log.Warnf("Spotify playlist %s is empty", playlistID)
		span.Status = sentry.SpanStatusOK
		return nil, errors.New("playlist is empty")
	}

	// Fetch playlist items with the specified limit
	items, err := Spotify.GetPlaylistItems(ctx, spotifyclient.ID(playlistID), spotifyclient.Limit(limit))
	if err != nil {
		log.Errorf("Failed to fetch Spotify playlist items %s: %v", playlistID, err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	tracks := make([]PlaylistTrackInfo, 0, limit)
	for i, item := range items.Items {
		// Skip non-track items (podcasts, episodes, etc.)
		if item.Track.Track == nil {
			continue
		}

		track := item.Track.Track
		artists := make([]string, 0, len(track.Artists))
		for _, artist := range track.Artists {
			artists = append(artists, artist.Name)
		}

		tracks = append(tracks, PlaylistTrackInfo{
			TrackInfo: TrackInfo{
				Title:   track.Name,
				Artists: artists,
			},
			Position: i,
		})
	}

	if len(tracks) == 0 {
		log.Warnf("Spotify playlist %s has no playable tracks (only podcasts or episodes)", playlistID)
		span.Status = sentry.SpanStatusOK
		return nil, errors.New("playlist contains no playable tracks (only podcasts or episodes)")
	}

	log.Debugf("Successfully fetched %d tracks from Spotify playlist '%s' (total: %d)", len(tracks), playlistName, totalTracks)
	span.Status = sentry.SpanStatusOK
	span.SetData("tracks_count", len(tracks))
	span.SetData("total_tracks", totalTracks)
	span.SetData("playlist_name", playlistName)

	return &PlaylistResult{
		Name:        playlistName,
		Tracks:      tracks,
		TotalTracks: totalTracks,
	}, nil
}

func ParseSpotifyURL(url string) (SpotifyRequest, error) {
	if strings.HasPrefix(url, "https://open.spotify.com/") {
		parts := strings.Split(url, "/")
		if len(parts) < 5 {
			log.Warnf("Invalid Spotify URL format (too few parts): %s", url)
			return SpotifyRequest{}, errors.New("invalid Spotify URL")
		}

		request := SpotifyRequest{}

		// Strip query parameters from ID (e.g., ?si=tracking_id)
		id := strings.Split(parts[4], "?")[0]

		switch parts[3] {
		case "playlist":
			request.PlaylistID = id
			log.Tracef("Parsed Spotify playlist URL: %s", id)
		case "artist":
			request.ArtistID = id
			log.Tracef("Parsed Spotify artist URL: %s", id)
		case "track":
			request.TrackID = id
			log.Tracef("Parsed Spotify track URL: %s", id)
		}

		return request, nil
	}

	log.Warnf("URL does not start with https://open.spotify.com/: %s", url)
	return SpotifyRequest{}, errors.New("invalid Spotify URL")
}
