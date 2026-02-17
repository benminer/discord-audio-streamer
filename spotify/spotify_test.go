package spotify

import (
	"testing"
)

func TestParseSpotifyURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    SpotifyRequest
		wantErr bool
	}{
		{
			name: "track",
			url:  "https://open.spotify.com/track/0VjIjW4GlUZAMYd2vXMi3b",
			want: SpotifyRequest{TrackID: "0VjIjW4GlUZAMYd2vXMi3b"},
		},
		{
			name: "track with si query",
			url:  "https://open.spotify.com/track/0VjIjW4GlUZAMYd2vXMi3b?si=abc123",
			want: SpotifyRequest{TrackID: "0VjIjW4GlUZAMYd2vXMi3b"},
		},
		{
			name: "playlist",
			url:  "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M",
			want: SpotifyRequest{PlaylistID: "37i9dQZF1DXcBWIGoYBM5M"},
		},
		{
			name: "album",
			url:  "https://open.spotify.com/album/4yP0hdKOZPNshxUOjY0cZj",
			want: SpotifyRequest{AlbumID: "4yP0hdKOZPNshxUOjY0cZj"},
		},
		{
			name: "artist",
			url:  "https://open.spotify.com/artist/4NHQPlJsbc7kbJTwq0B3lD",
			want: SpotifyRequest{ArtistID: "4NHQPlJsbc7kbJTwq0B3lD"},
		},
		{
			name:    "invalid domain",
			url:     "https://example.com/track/abc",
			want:    SpotifyRequest{},
			wantErr: true,
		},
		{
			name:    "missing id",
			url:     "https://open.spotify.com/track/",
			want:    SpotifyRequest{TrackID: ""},
			wantErr: false,
		},
		{
			name:    "wrong path",
			url:     "https://open.spotify.com/wrong/abc",
			want:    SpotifyRequest{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSpotifyURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSpotifyURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("ParseSpotifyURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
