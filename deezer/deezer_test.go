package deezer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestNormalizeArtistName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "already normalized",
			input: "daft punk",
			want:  "daft punk",
		},
		{
			name:  "needs lowercasing",
			input: "Daft Punk",
			want:  "daft punk",
		},
		{
			name:  "needs trimming",
			input: "  Daft Punk  ",
			want:  "daft punk",
		},
		{
			name:  "mixed case and whitespace",
			input: "\tDAFT Punk\n",
			want:  "daft punk",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeArtistName(tt.input); got != tt.want {
				t.Errorf("normalizeArtistName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrackUnmarshal(t *testing.T) {
	raw := `{
		"id": 3135556,
		"title": "One More Time",
		"title_short": "One More Time",
		"duration": 320,
		"rank": 799396,
		"preview": "https://cdn-preview.deezer.com/one-more-time.mp3",
		"explicit_lyrics": false,
		"artist": {
			"id": 27,
			"name": "Daft Punk",
			"link": "https://www.deezer.com/artist/27",
			"picture_medium": "https://cdn-images.dzcdn.net/images/artist/daft-punk-medium.jpg",
			"nb_fan": 4500000,
			"radio": true,
			"tracklist": "https://api.deezer.com/artist/27/top"
		},
		"album": {
			"id": 302127,
			"title": "Discovery",
			"cover_medium": "https://cdn-images.dzcdn.net/images/cover/discovery-medium.jpg",
			"cover_xl": "https://cdn-images.dzcdn.net/images/cover/discovery-xl.jpg",
			"release_date": "2001-03-07"
		}
	}`

	var track Track
	if err := json.Unmarshal([]byte(raw), &track); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	want := Track{
		ID:             3135556,
		Title:          "One More Time",
		TitleShort:     "One More Time",
		Duration:       320,
		Rank:           799396,
		Preview:        "https://cdn-preview.deezer.com/one-more-time.mp3",
		ExplicitLyrics: false,
		Artist: Artist{
			ID:        27,
			Name:      "Daft Punk",
			Link:      "https://www.deezer.com/artist/27",
			Picture:   "https://cdn-images.dzcdn.net/images/artist/daft-punk-medium.jpg",
			NbFan:     4500000,
			Radio:     true,
			Tracklist: "https://api.deezer.com/artist/27/top",
		},
		Album: Album{
			ID:       302127,
			Title:    "Discovery",
			Cover:    "https://cdn-images.dzcdn.net/images/cover/discovery-medium.jpg",
			CoverXL:  "https://cdn-images.dzcdn.net/images/cover/discovery-xl.jpg",
			Released: "2001-03-07",
		},
	}

	if track != want {
		t.Errorf("Track = %+v, want %+v", track, want)
	}
}

func TestTrackDetailUnmarshal(t *testing.T) {
	raw := `{
		"id": 3135556,
		"title": "One More Time",
		"duration": 320,
		"bpm": 123.4,
		"gain": -8.9,
		"isrc": "GBDUW0000059",
		"artist": {
			"id": 27,
			"name": "Daft Punk"
		},
		"album": {
			"id": 302127,
			"title": "Discovery",
			"release_date": "2001-03-07"
		}
	}`

	var detail TrackDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if detail.ID != 3135556 {
		t.Errorf("ID = %d, want 3135556", detail.ID)
	}
	if detail.Title != "One More Time" {
		t.Errorf("Title = %q, want %q", detail.Title, "One More Time")
	}
	if detail.BPM != 123.4 {
		t.Errorf("BPM = %v, want 123.4", detail.BPM)
	}
	if detail.Gain != -8.9 {
		t.Errorf("Gain = %v, want -8.9", detail.Gain)
	}
	if detail.ISRC != "GBDUW0000059" {
		t.Errorf("ISRC = %q, want %q", detail.ISRC, "GBDUW0000059")
	}
	if detail.Artist.Name != "Daft Punk" {
		t.Errorf("Artist.Name = %q, want %q", detail.Artist.Name, "Daft Punk")
	}
	if detail.Album.Title != "Discovery" {
		t.Errorf("Album.Title = %q, want %q", detail.Album.Title, "Discovery")
	}
}

func TestArtistCache(t *testing.T) {
	key := normalizeArtistName("Daft Punk")
	t.Cleanup(func() { artistCache.Delete(key) })

	if _, ok := artistCache.Load(key); ok {
		t.Fatalf("artistCache.Load(%q) hit before store, want miss", key)
	}

	artist := &Artist{ID: 27, Name: "Daft Punk"}
	artistCache.Store(key, artist)

	got, ok := artistCache.Load(key)
	if !ok {
		t.Fatalf("artistCache.Load(%q) miss after store, want hit", key)
	}
	cached, ok := got.(*Artist)
	if !ok {
		t.Fatalf("artistCache.Load(%q) type = %T, want *Artist", key, got)
	}
	if cached.ID != artist.ID || cached.Name != artist.Name {
		t.Errorf("cached artist = %+v, want %+v", cached, artist)
	}

	artistCache.Delete(key)
	if _, ok := artistCache.Load(key); ok {
		t.Fatalf("artistCache.Load(%q) hit after delete, want miss", key)
	}
}

func TestConcurrentArtistCache(t *testing.T) {
	const goroutines = 100

	keys := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		keys[i] = normalizeArtistName("Artist " + string(rune('A'+i%26)) + string(rune('0'+i/26)))
	}
	t.Cleanup(func() {
		for _, key := range keys {
			artistCache.Delete(key)
		}
	})

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := keys[idx]
			artist := &Artist{ID: idx, Name: key}
			artistCache.Store(key, artist)
			if got, ok := artistCache.Load(key); ok {
				if cached, ok := got.(*Artist); ok && cached.ID != idx {
					t.Errorf("artistCache.Load(%q) ID = %d, want %d", key, cached.ID, idx)
				}
			}
		}(i)
	}
	wg.Wait()

	for i, key := range keys {
		got, ok := artistCache.Load(key)
		if !ok {
			t.Errorf("artistCache.Load(%q) miss after concurrent stores", key)
			continue
		}
		cached, ok := got.(*Artist)
		if !ok || cached.ID != i {
			t.Errorf("artistCache.Load(%q) = %+v, want ID %d", key, got, i)
		}
	}
}

// captureTransport is a fake http.RoundTripper that records the outgoing
// request URL and returns a canned response, so we can assert on the query
// Deezer would have received without making a real network call.
type captureTransport struct {
	mu       sync.Mutex
	lastURL  *url.URL
	body     []byte
	callback func(*http.Request)
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.lastURL = req.URL
	c.mu.Unlock()
	if c.callback != nil {
		c.callback(req)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(c.body)),
	}, nil
}

func (c *captureTransport) URL() *url.URL {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastURL
}

func TestSearchTrackQueryFormat(t *testing.T) {
	tests := []struct {
		name      string
		artist    string
		title     string
		wantQuery string
	}{
		{
			name:      "simple artist and title",
			artist:    "Daft Punk",
			title:     "One More Time",
			wantQuery: `artist:"Daft Punk" track:"One More Time"`,
		},
		{
			name:      "title with punctuation",
			artist:    "Justice",
			title:     "D.A.N.C.E.",
			wantQuery: `artist:"Justice" track:"D.A.N.C.E."`,
		},
		{
			name:      "artist with ampersand",
			artist:    "Simon & Garfunkel",
			title:     "The Sound of Silence",
			wantQuery: `artist:"Simon & Garfunkel" track:"The Sound of Silence"`,
		},
	}

	trackJSON := `{
		"data": [{
			"id": 3135556,
			"title": "One More Time",
			"artist": {"id": 27, "name": "Daft Punk"},
			"album": {"id": 302127, "title": "Discovery"}
		}],
		"total": 1
	}`

	origClient := httpClient
	t.Cleanup(func() { httpClient = origClient })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &captureTransport{body: []byte(trackJSON)}
			httpClient = &http.Client{Transport: transport, Timeout: 10 * time.Second}

			if _, err := SearchTrack(context.Background(), tt.artist, tt.title); err != nil {
				t.Fatalf("SearchTrack() unexpected error: %v", err)
			}

			reqURL := transport.URL()
			if reqURL == nil {
				t.Fatalf("SearchTrack() made no HTTP request")
			}

			gotQuery := reqURL.Query().Get("q")
			if gotQuery != tt.wantQuery {
				t.Errorf("query param q = %q, want %q", gotQuery, tt.wantQuery)
			}
		})
	}
}

func TestListResponseUnmarshal(t *testing.T) {
	raw := `{
		"data": [
			{
				"id": 3135556,
				"title": "One More Time",
				"artist": {"id": 27, "name": "Daft Punk"},
				"album": {"id": 302127, "title": "Discovery"}
			},
			{
				"id": 3135475,
				"title": "Harder, Better, Faster, Stronger",
				"artist": {"id": 27, "name": "Daft Punk"},
				"album": {"id": 302127, "title": "Discovery"}
			}
		],
		"total": 2,
		"next": "https://api.deezer.com/search/track?q=daft+punk&index=25"
	}`

	var results listResponse[Track]
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if results.Total != 2 {
		t.Errorf("Total = %d, want 2", results.Total)
	}
	if len(results.Data) != 2 {
		t.Fatalf("len(Data) = %d, want 2", len(results.Data))
	}
	if results.Data[0].Title != "One More Time" {
		t.Errorf("Data[0].Title = %q, want %q", results.Data[0].Title, "One More Time")
	}
	if results.Data[1].Title != "Harder, Better, Faster, Stronger" {
		t.Errorf("Data[1].Title = %q, want %q", results.Data[1].Title, "Harder, Better, Faster, Stronger")
	}
	if results.Data[0].Artist.Name != "Daft Punk" {
		t.Errorf("Data[0].Artist.Name = %q, want %q", results.Data[0].Artist.Name, "Daft Punk")
	}
	if results.Next != "https://api.deezer.com/search/track?q=daft+punk&index=25" {
		t.Errorf("Next = %q, want non-empty next-page URL", results.Next)
	}
}
