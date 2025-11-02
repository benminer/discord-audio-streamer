package spotify

import (
	"context"
	"errors"
	"os"
	"strings"

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

func NewSpotifyClient() error {
	ctx := context.Background()
	config := &clientcredentials.Config{
		ClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		ClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		TokenURL:     spotifyauth.TokenURL,
	}
	token, err := config.Token(ctx)
	if err != nil {
		return err
	}

	httpClient := spotifyauth.New().Client(ctx, token)
	client := spotifyclient.New(httpClient)
	Spotify = client
	return nil
}

func Search(query string) (spotifyclient.SearchResult, error) {
	ctx := context.Background()
	results, err := Spotify.Search(ctx, query, spotifyclient.SearchTypeTrack)
	if err != nil {
		return spotifyclient.SearchResult{}, err
	}

	return *results, nil
}

func GetTrack(trackID string) (*TrackInfo, error) {
	log.Tracef("Fetching track from Spotify API: %s", trackID)
	ctx := context.Background()
	track, err := Spotify.GetTrack(ctx, spotifyclient.ID(trackID))
	if err != nil {
		log.Errorf("Failed to fetch Spotify track %s: %v", trackID, err)
		return nil, err
	}

	artists := []string{}
	for _, artist := range track.Artists {
		artists = append(artists, artist.Name)
	}

	log.Debugf("Successfully fetched Spotify track: '%s' by %v", track.Name, artists)
	return &TrackInfo{
		Title:   track.Name,
		Artists: artists,
	}, nil
}

func GetArtistTopSongs(artistID string) ([]string, error) {
	ctx := context.Background()
	results, err := Spotify.GetArtistsTopTracks(ctx, spotifyclient.ID(artistID), "US")
	if err != nil {
		return nil, err
	}

	names := []string{}
	for _, track := range results {
		names = append(names, track.Name)
	}

	return names, nil
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
