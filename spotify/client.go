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

type SpotifyTrack struct {
	URI       string
	Name      string
	Artist    string
	ArtistURI string
	Album     string
}

type SearchResult struct {
	Tracks []SpotifyTrack
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

func Search(query string) (SearchResult, error) {
	ctx := context.Background()
	results, err := Spotify.Search(ctx, query, spotifyclient.SearchTypeTrack)
	if err != nil {
		return SearchResult{}, err
	}

	tracks := []SpotifyTrack{}
	for _, track := range results.Tracks.Tracks {
		tracks = append(tracks, SpotifyTrack{
			URI:       string(track.URI),
			Name:      track.Name,
			Artist:    track.Artists[0].Name,
			ArtistURI: string(track.Artists[0].URI),
			Album:     track.Album.Name,
		})
	}

	return SearchResult{Tracks: tracks}, nil
}

func GetRecommendedTracks(seedTracks []string) ([]SpotifyTrack, error) {
	trackIds := []spotifyclient.ID{}
	for _, track := range seedTracks {
		trackIds = append(trackIds, spotifyclient.ID(track))
	}
	ctx := context.Background()
	results, err := Spotify.GetRecommendations(ctx, spotifyclient.Seeds{
		Tracks: trackIds,
	}, nil, spotifyclient.Limit(5))

	if err != nil {
		return []SpotifyTrack{}, err
	}

	tracks := []SpotifyTrack{}
	for _, track := range results.Tracks {
		tracks = append(tracks, SpotifyTrack{
			URI:       string(track.URI),
			Name:      track.Name,
			Artist:    track.Artists[0].Name,
			ArtistURI: string(track.Artists[0].URI),
			Album:     track.Album.Name,
		})
	}

	return tracks, nil
}

func SearchAndRecommend(query string) ([]SpotifyTrack, error) {
	results, err := Search(query)
	if err != nil {
		return []SpotifyTrack{}, err
	}

	uris := []string{}
	for _, track := range results.Tracks {
		uris = append(uris, track.URI)
	}

	recommended, err := GetRecommendedTracks(uris)
	if err != nil {
		return []SpotifyTrack{}, err
	}

	return recommended, nil
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
