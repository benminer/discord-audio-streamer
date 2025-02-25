package spotify

import (
	"context"
	"errors"
	"os"
	"strings"

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
		if len(parts) < 3 {
			return SpotifyRequest{}, errors.New("invalid Spotify URL")
		}

		request := SpotifyRequest{}

		if parts[2] == "playlist" {
			request.PlaylistID = parts[3]
		} else if parts[2] == "artist" {
			request.ArtistID = parts[3]
		} else if parts[2] == "track" {
			request.TrackID = parts[3]
		}

		return request, nil
	}

	return SpotifyRequest{}, errors.New("invalid Spotify URL")
}
