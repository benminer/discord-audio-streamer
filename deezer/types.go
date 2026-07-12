package deezer

// Artist represents a Deezer artist from search/radio responses
type Artist struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Link      string `json:"link"`
	Picture   string `json:"picture_medium"`
	NbFan     int    `json:"nb_fan"`
	Radio     bool   `json:"radio"`
	Tracklist string `json:"tracklist"`
}

// Album represents album data nested in track responses
type Album struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Cover    string `json:"cover_medium"`
	CoverXL  string `json:"cover_xl"`
	Released string `json:"release_date"`
}

// Track represents a track from search/radio/chart responses
type Track struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	TitleShort     string `json:"title_short"`
	Duration       int    `json:"duration"`
	Rank           int    `json:"rank"`
	Preview        string `json:"preview"`
	ExplicitLyrics bool   `json:"explicit_lyrics"`
	Artist         Artist `json:"artist"`
	Album          Album  `json:"album"`
}

// TrackDetail has full metadata including BPM (from /track/{id} endpoint)
type TrackDetail struct {
	Track
	BPM  float64 `json:"bpm"`
	Gain float64 `json:"gain"`
	ISRC string  `json:"isrc"`
}

// RadioStation represents a curated radio station
type RadioStation struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Picture   string `json:"picture_medium"`
	Tracklist string `json:"tracklist"`
}

// Genre represents a genre with its associated radio stations
type Genre struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Picture string         `json:"picture_medium"`
	Radios  []RadioStation `json:"radios"`
}

// ChartResponse is the top-level response from /chart
type ChartResponse struct {
	Tracks struct {
		Data []Track `json:"data"`
	} `json:"tracks"`
	Artists struct {
		Data []Artist `json:"data"`
	} `json:"artists"`
	Albums struct {
		Data []Album `json:"data"`
	} `json:"albums"`
}

// listResponse is the generic paginated list wrapper Deezer uses
type listResponse[T any] struct {
	Data  []T    `json:"data"`
	Total int    `json:"total"`
	Next  string `json:"next"`
}

// TrackMeta is enrichment data stored on queue items after Deezer resolution
type TrackMeta struct {
	DeezerID       int
	BPM            float64
	Genre          string
	AlbumName      string
	AlbumYear      string
	AlbumArtURL    string
	Popularity     int
	Explicit       bool
	Duration       int
	ArtistName     string
	RelatedArtists []string
}
