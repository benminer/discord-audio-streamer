package applemusic

import (
	"testing"
)

func TestParseAppleMusicURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    AppleMusicRequest
		wantErr bool
	}{
		{
			name: "album us",
			url:  "https://music.apple.com/us/album/the-dark-side-of-the-moon/1441165866",
			want: AppleMusicRequest{Country: "us", AlbumID: "1441165866"},
		},
		{
			name: "playlist pl prefix",
			url:  "https://music.apple.com/us/playlist/90s-alternative/pl.u-8VoLGjY1l8l5l5l5l5",
			want: AppleMusicRequest{Country: "us", PlaylistID: "pl.u-8VoLGjY1l8l5l5l5l5"},
		},
		{
			name: "track with i query",
			url:  "https://music.apple.com/us/album/album-name/123456789?i=1646389445",
			want: AppleMusicRequest{Country: "us", AlbumID: "123456789", TrackID: "1646389445"},
		},
		{
			name: "itunes domain",
			url:  "https://itunes.apple.com/us/album/album-name/123456789",
			want: AppleMusicRequest{Country: "us", AlbumID: "123456789"},
		},
		{
			name: "uk country",
			url:  "https://music.apple.com/gb/album/album-name/123456789",
			want: AppleMusicRequest{Country: "gb", AlbumID: "123456789"},
		},
		{
			name:    "invalid no apple.com",
			url:     "https://example.com/album/id123",
			want:    AppleMusicRequest{},
			wantErr: true,
		},
		{
			name:    "no id",
			url:     "https://music.apple.com/us/album/no-id-here",
			want:    AppleMusicRequest{Country: "us"},
			wantErr: true, // no ID extracted
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAppleMusicURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAppleMusicURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("ParseAppleMusicURL() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
