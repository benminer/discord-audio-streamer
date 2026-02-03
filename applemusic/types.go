package applemusic

// AppleMusicRequest represents a parsed Apple Music URL
type AppleMusicRequest struct {
	TrackID    string
	AlbumID    string
	PlaylistID string
	ArtistID   string
	Country    string // e.g., "us"
}

// TrackInfo represents basic track metadata
type TrackInfo struct {
	Title   string
	Artists []string
	Album   string
}

// PlaylistTrackInfo represents a track within a playlist context
type PlaylistTrackInfo struct {
	TrackInfo
	Position int
}

// AlbumResult represents album metadata and tracks
type AlbumResult struct {
	Name        string
	Artist      string
	Tracks      []PlaylistTrackInfo
	TotalTracks int
}

// PlaylistResult represents playlist metadata and tracks
type PlaylistResult struct {
	Name        string
	Tracks      []PlaylistTrackInfo
	TotalTracks int
}
