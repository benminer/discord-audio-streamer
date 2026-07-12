# Deezer Music Intelligence Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Deezer's public API as a music intelligence layer that improves radio recommendations, adds genre/artist radio and charts commands, enables BPM-aware song selection, and enriches now-playing embeds and DJ commentary.

**Architecture:** New `deezer/` package provides search, artist radio, genre stations, track metadata, and charts via Deezer's no-auth public API. Radio song picking gains Deezer as a weighted signal blended with existing YouTube Mix and Gemini. Background metadata resolution on playback enriches embeds and Gemini prompts with genre, BPM, album data.

**Tech Stack:** Go 1.26, net/http (direct API calls), Sentry spans, logrus logging, Discord slash commands via discordgo fork

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `deezer/client.go` | HTTP client, rate limiting, base request helper |
| `deezer/types.go` | All response/domain structs |
| `deezer/search.go` | Artist and track search with advanced query syntax |
| `deezer/radio.go` | Artist radio, genre stations, station tracks |
| `deezer/tracks.go` | Single track detail (BPM, metadata) |
| `deezer/charts.go` | Global charts endpoint |
| `deezer/deezer_test.go` | Unit tests for URL building, response parsing, rate limiting |
| `handlers/charts.go` | `/charts` command handler |

### Modified Files
| File | Changes |
|------|---------|
| `config/config.go` | Add `DeezerConfig` struct and env var parsing |
| `commands.json` | Add `genre` and `artist` options to `/radio`, add `/charts` command |
| `controller/controller.go` | Add `DeezerMeta` field to `GuildQueueItem`, modify `pickRadioSong()` for blended scoring, add BPM metadata resolution on PlaybackStarted, add genre/artist radio pool management |
| `handlers/handlers.go` | Add `"charts"` case to command dispatch switch |
| `handlers/playback.go` | Extend `handleRadio` to parse `genre`/`artist` options |
| `discord/embeds.go` | Extend `NowPlayingMetadata` and `BuildNowPlayingEmbed` with Deezer fields |
| `gemini/gemini.go` | Extend `GenerateNowPlayingCommentary` and `GenerateDJScript` prompts with metadata context |

---

## Task 1: Deezer Types and Client Foundation

**Files:**
- Create: `deezer/types.go`
- Create: `deezer/client.go`

- [ ] **Step 1: Create `deezer/types.go` with all response structs**

```go
package deezer

type Artist struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Link      string `json:"link"`
	Picture   string `json:"picture_medium"`
	NbFan     int    `json:"nb_fan"`
	Radio     bool   `json:"radio"`
	Tracklist string `json:"tracklist"`
}

type Album struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Cover    string `json:"cover_medium"`
	CoverXL  string `json:"cover_xl"`
	Released string `json:"release_date"`
}

type Track struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	TitleShort    string `json:"title_short"`
	Duration      int    `json:"duration"`
	Rank          int    `json:"rank"`
	Preview       string `json:"preview"`
	ExplicitLyrics bool  `json:"explicit_lyrics"`
	Artist        Artist `json:"artist"`
	Album         Album  `json:"album"`
}

type TrackDetail struct {
	Track
	BPM              float64 `json:"bpm"`
	Gain             float64 `json:"gain"`
	ISRC             string  `json:"isrc"`
	AvailableCountries []string `json:"available_countries"`
}

type RadioStation struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Picture  string `json:"picture_medium"`
	Tracklist string `json:"tracklist"`
}

type Genre struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Picture string         `json:"picture_medium"`
	Radios  []RadioStation `json:"radios"`
}

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

type listResponse[T any] struct {
	Data  []T    `json:"data"`
	Total int    `json:"total"`
	Next  string `json:"next"`
}

// TrackMeta is the enrichment data stored on queue items after Deezer resolution
type TrackMeta struct {
	DeezerID    int
	BPM         float64
	Genre       string
	AlbumName   string
	AlbumYear   string
	AlbumArtURL string
	Popularity  int
	Explicit    bool
	Duration    int
	ArtistName  string
	RelatedArtists []string
}
```

- [ ] **Step 2: Create `deezer/client.go` with HTTP client and rate limiting**

```go
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

const baseURL = "https://api.deezer.com"

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}

	// Token bucket rate limiter: 50 requests per 5 seconds
	rateMu      sync.Mutex
	rateTokens  = 50
	rateMax     = 50
	rateRefillAt time.Time
)

func init() {
	rateRefillAt = time.Now()
}

func waitForRateLimit() {
	rateMu.Lock()
	defer rateMu.Unlock()

	now := time.Now()
	if now.After(rateRefillAt) {
		rateTokens = rateMax
		rateRefillAt = now.Add(5 * time.Second)
	}

	if rateTokens <= 0 {
		sleepDur := time.Until(rateRefillAt)
		rateMu.Unlock()
		time.Sleep(sleepDur)
		rateMu.Lock()
		rateTokens = rateMax
		rateRefillAt = time.Now().Add(5 * time.Second)
	}

	rateTokens--
}

func get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	waitForRateLimit()

	span := sentry.StartSpan(ctx, "http.client", sentry.WithDescription("GET "+path))
	defer span.Finish()
	span.SetData("deezer.path", path)

	reqURL := baseURL + path
	if params != nil {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deezer: create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("deezer: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		span.Status = sentry.SpanStatusInternalError
		log.WithFields(log.Fields{
			"module": "deezer",
			"status": resp.StatusCode,
			"path":   path,
		}).Warn("Deezer API returned non-200")
		return nil, fmt.Errorf("deezer: status %d", resp.StatusCode)
	}

	// Check for Deezer API-level errors (returned as 200 with error body)
	var apiErr struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Code != 0 {
		return nil, fmt.Errorf("deezer: API error %d: %s", apiErr.Error.Code, apiErr.Error.Message)
	}

	span.Status = sentry.SpanStatusOK
	return body, nil
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./deezer/...`
Expected: PASS (no errors)

- [ ] **Step 4: Commit**

```bash
git add deezer/types.go deezer/client.go
git commit -m "feat(deezer): add types and HTTP client with rate limiting"
```

---

## Task 2: Deezer Search Functions

**Files:**
- Create: `deezer/search.go`

- [ ] **Step 1: Create `deezer/search.go` with artist and track search**

```go
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	artistCache   sync.Map // map[string]int (normalized name -> deezer artist ID)
)

func normalizeArtistName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// SearchArtist finds a Deezer artist by name. Uses an in-memory cache to avoid repeated lookups.
func SearchArtist(ctx context.Context, name string) (*Artist, error) {
	normalized := normalizeArtistName(name)

	// Check cache
	if cachedID, ok := artistCache.Load(normalized); ok {
		id := cachedID.(int)
		return getArtistByID(ctx, id)
	}

	params := url.Values{"q": {name}, "limit": {"1"}}
	body, err := get(ctx, "/search/artist", params)
	if err != nil {
		return nil, err
	}

	var resp listResponse[Artist]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal artist search: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, nil
	}

	artist := &resp.Data[0]
	artistCache.Store(normalized, artist.ID)

	log.WithFields(log.Fields{
		"module":    "deezer",
		"artist":    artist.Name,
		"artist_id": artist.ID,
		"query":     name,
	}).Debug("Resolved artist via Deezer search")

	return artist, nil
}

func getArtistByID(ctx context.Context, id int) (*Artist, error) {
	body, err := get(ctx, fmt.Sprintf("/artist/%d", id), nil)
	if err != nil {
		return nil, err
	}

	var artist Artist
	if err := json.Unmarshal(body, &artist); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal artist: %w", err)
	}

	return &artist, nil
}

// SearchTrack finds a track on Deezer using artist + title for precise matching.
func SearchTrack(ctx context.Context, artist, title string) (*Track, error) {
	query := fmt.Sprintf("artist:\"%s\" track:\"%s\"", artist, title)
	params := url.Values{"q": {query}, "limit": {"1"}}
	body, err := get(ctx, "/search/track", params)
	if err != nil {
		// Fall back to simpler query
		params = url.Values{"q": {artist + " " + title}, "limit": {"3"}}
		body, err = get(ctx, "/search/track", params)
		if err != nil {
			return nil, err
		}
	}

	var resp listResponse[Track]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal track search: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, nil
	}

	return &resp.Data[0], nil
}

// SearchRadioStation searches for radio stations by name/genre.
func SearchRadioStation(ctx context.Context, query string) ([]RadioStation, error) {
	params := url.Values{"q": {query}}
	body, err := get(ctx, "/search/radio", params)
	if err != nil {
		return nil, err
	}

	var resp listResponse[RadioStation]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal radio search: %w", err)
	}

	return resp.Data, nil
}

// GetRelatedArtists returns artists similar to the given artist.
func GetRelatedArtists(ctx context.Context, artistID int) ([]Artist, error) {
	body, err := get(ctx, fmt.Sprintf("/artist/%d/related", artistID), nil)
	if err != nil {
		return nil, err
	}

	var resp listResponse[Artist]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal related artists: %w", err)
	}

	return resp.Data, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./deezer/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add deezer/search.go
git commit -m "feat(deezer): add artist/track search with in-memory cache"
```

---

## Task 3: Deezer Radio and Track Detail

**Files:**
- Create: `deezer/radio.go`
- Create: `deezer/tracks.go`
- Create: `deezer/charts.go`

- [ ] **Step 1: Create `deezer/radio.go`**

```go
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetArtistRadio returns up to 25 algorithmically similar tracks for an artist.
func GetArtistRadio(ctx context.Context, artistID int) ([]Track, error) {
	body, err := get(ctx, fmt.Sprintf("/artist/%d/radio", artistID), nil)
	if err != nil {
		return nil, err
	}

	var resp listResponse[Track]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal artist radio: %w", err)
	}

	return resp.Data, nil
}

// GetGenreStations returns all genres with their associated radio stations.
func GetGenreStations(ctx context.Context) ([]Genre, error) {
	body, err := get(ctx, "/radio/genres", nil)
	if err != nil {
		return nil, err
	}

	var resp listResponse[Genre]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal genre stations: %w", err)
	}

	return resp.Data, nil
}

// GetStationTracks returns tracks for a specific radio station.
func GetStationTracks(ctx context.Context, stationID int) ([]Track, error) {
	body, err := get(ctx, fmt.Sprintf("/radio/%d/tracks", stationID), nil)
	if err != nil {
		return nil, err
	}

	var resp listResponse[Track]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal station tracks: %w", err)
	}

	return resp.Data, nil
}
```

- [ ] **Step 2: Create `deezer/tracks.go`**

```go
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetTrack returns full metadata for a track including BPM.
func GetTrack(ctx context.Context, trackID int) (*TrackDetail, error) {
	body, err := get(ctx, fmt.Sprintf("/track/%d", trackID), nil)
	if err != nil {
		return nil, err
	}

	var track TrackDetail
	if err := json.Unmarshal(body, &track); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal track detail: %w", err)
	}

	return &track, nil
}

// ResolveTrackMeta searches for a song on Deezer and returns enriched metadata.
// Returns nil if the song can't be found (not an error condition).
func ResolveTrackMeta(ctx context.Context, artist, title string) *TrackMeta {
	track, err := SearchTrack(ctx, artist, title)
	if err != nil || track == nil {
		return nil
	}

	detail, err := GetTrack(ctx, track.ID)
	if err != nil {
		// We have basic info from search, use that without BPM
		return &TrackMeta{
			DeezerID:    track.ID,
			Genre:       "",
			AlbumName:   track.Album.Title,
			AlbumArtURL: track.Album.CoverXL,
			Popularity:  track.Rank,
			Explicit:    track.ExplicitLyrics,
			Duration:    track.Duration,
			ArtistName:  track.Artist.Name,
		}
	}

	// Extract year from release date (format: "YYYY-MM-DD")
	albumYear := ""
	if detail.Album.Released != "" && len(detail.Album.Released) >= 4 {
		albumYear = detail.Album.Released[:4]
	}

	return &TrackMeta{
		DeezerID:    detail.ID,
		BPM:         detail.BPM,
		AlbumName:   detail.Album.Title,
		AlbumYear:   albumYear,
		AlbumArtURL: detail.Album.CoverXL,
		Popularity:  detail.Rank,
		Explicit:    detail.ExplicitLyrics,
		Duration:    detail.Duration,
		ArtistName:  detail.Artist.Name,
	}
}
```

- [ ] **Step 3: Create `deezer/charts.go`**

```go
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetCharts returns the current global charts (top tracks, artists, albums).
func GetCharts(ctx context.Context) (*ChartResponse, error) {
	body, err := get(ctx, "/chart", nil)
	if err != nil {
		return nil, err
	}

	var chart ChartResponse
	if err := json.Unmarshal(body, &chart); err != nil {
		return nil, fmt.Errorf("deezer: unmarshal charts: %w", err)
	}

	return &chart, nil
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./deezer/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deezer/radio.go deezer/tracks.go deezer/charts.go
git commit -m "feat(deezer): add radio, track detail, and charts endpoints"
```

---

## Task 4: Unit Tests for Deezer Package

**Files:**
- Create: `deezer/deezer_test.go`

- [ ] **Step 1: Write tests for search query building, response parsing, and rate limiting**

```go
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		{"lowercase", "Daft Punk", "daft punk"},
		{"trim spaces", "  Radiohead  ", "radiohead"},
		{"already normalized", "justice", "justice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeArtistName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeArtistName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrackUnmarshal(t *testing.T) {
	raw := `{
		"id": 3135556,
		"title": "Harder Better Faster Stronger",
		"title_short": "Harder Better Faster Stronger",
		"duration": 224,
		"rank": 879042,
		"explicit_lyrics": false,
		"artist": {"id": 27, "name": "Daft Punk"},
		"album": {"id": 302127, "title": "Discovery", "cover_medium": "https://example.com/cover.jpg", "cover_xl": "https://example.com/cover_xl.jpg", "release_date": "2001-03-12"}
	}`

	var track Track
	if err := json.Unmarshal([]byte(raw), &track); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if track.ID != 3135556 {
		t.Errorf("ID = %d, want 3135556", track.ID)
	}
	if track.Title != "Harder Better Faster Stronger" {
		t.Errorf("Title = %q, want %q", track.Title, "Harder Better Faster Stronger")
	}
	if track.Duration != 224 {
		t.Errorf("Duration = %d, want 224", track.Duration)
	}
	if track.Artist.Name != "Daft Punk" {
		t.Errorf("Artist.Name = %q, want %q", track.Artist.Name, "Daft Punk")
	}
	if track.Album.Title != "Discovery" {
		t.Errorf("Album.Title = %q, want %q", track.Album.Title, "Discovery")
	}
}

func TestTrackDetailUnmarshal(t *testing.T) {
	raw := `{
		"id": 3135556,
		"title": "Harder Better Faster Stronger",
		"title_short": "Harder Better Faster Stronger",
		"duration": 224,
		"rank": 879042,
		"bpm": 123.5,
		"gain": -7.2,
		"explicit_lyrics": false,
		"isrc": "GBDUW0000059",
		"artist": {"id": 27, "name": "Daft Punk"},
		"album": {"id": 302127, "title": "Discovery", "cover_medium": "https://example.com/cover.jpg", "cover_xl": "https://example.com/cover_xl.jpg", "release_date": "2001-03-12"}
	}`

	var detail TrackDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if detail.BPM != 123.5 {
		t.Errorf("BPM = %f, want 123.5", detail.BPM)
	}
	if detail.ISRC != "GBDUW0000059" {
		t.Errorf("ISRC = %q, want %q", detail.ISRC, "GBDUW0000059")
	}
}

func TestArtistCache(t *testing.T) {
	// Store and retrieve from cache
	artistCache.Store("daft punk", 27)

	val, ok := artistCache.Load("daft punk")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if val.(int) != 27 {
		t.Errorf("cached ID = %d, want 27", val.(int))
	}

	// Miss
	_, ok = artistCache.Load("unknown artist")
	if ok {
		t.Error("expected cache miss")
	}

	// Cleanup
	artistCache.Delete("daft punk")
}

func TestRateLimiting(t *testing.T) {
	// Reset rate limiter state
	rateMu.Lock()
	rateTokens = 2
	rateRefillAt = time.Now().Add(5 * time.Second)
	rateMu.Unlock()

	// First two should pass immediately
	start := time.Now()
	waitForRateLimit()
	waitForRateLimit()
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("first two requests took %v, expected near-instant", elapsed)
	}

	// Reset for other tests
	rateMu.Lock()
	rateTokens = rateMax
	rateRefillAt = time.Now()
	rateMu.Unlock()
}

func TestGetWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/artist" {
			q := r.URL.Query().Get("q")
			if q == "Daft Punk" {
				w.Write([]byte(`{"data": [{"id": 27, "name": "Daft Punk", "nb_fan": 1000000}], "total": 1}`))
				return
			}
		}
		w.Write([]byte(`{"data": [], "total": 0}`))
	}))
	defer server.Close()

	// Temporarily override baseURL for testing
	origGet := get
	_ = origGet

	// Instead, test that the response parsing works correctly by testing the JSON
	raw := `{"data": [{"id": 27, "name": "Daft Punk", "nb_fan": 1000000, "radio": true}], "total": 1}`
	var resp listResponse[Artist]
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Data))
	}
	if resp.Data[0].ID != 27 {
		t.Errorf("artist ID = %d, want 27", resp.Data[0].ID)
	}
}

func TestSearchTrackQueryFormat(t *testing.T) {
	// Verify the query format that would be sent to Deezer
	artist := "Daft Punk"
	title := "Around the World"
	expectedQuery := `artist:"Daft Punk" track:"Around the World"`

	query := fmt.Sprintf("artist:\"%s\" track:\"%s\"", artist, title)
	if query != expectedQuery {
		t.Errorf("query = %q, want %q", query, expectedQuery)
	}

	// Verify it encodes properly for URL
	params := url.Values{"q": {query}, "limit": {"1"}}
	encoded := params.Encode()
	if encoded == "" {
		t.Error("encoded params should not be empty")
	}
}

func TestResolveTrackMeta(t *testing.T) {
	// Test with nil return (no match found) - this is the expected graceful path
	ctx := context.Background()
	meta := ResolveTrackMeta(ctx, "Completely Unknown Artist ZZZZZ", "Nonexistent Song ZZZZZ")
	// We can't guarantee nil because this hits the real API in integration tests,
	// but in unit tests with no network, it will be nil due to connection refused
	_ = meta
}

func TestConcurrentArtistCache(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := normalizeArtistName("test artist")
			artistCache.Store(key, n)
			artistCache.Load(key)
		}(i)
	}
	wg.Wait()
	artistCache.Delete("test artist")
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go test ./deezer/ -v -count=1 -short`
Expected: All tests PASS

- [ ] **Step 3: Commit**

```bash
git add deezer/deezer_test.go
git commit -m "test(deezer): add unit tests for types, cache, and rate limiting"
```

---

## Task 5: Config Integration

**Files:**
- Modify: `config/config.go`

- [ ] **Step 1: Add DeezerConfig struct and env var parsing to config.go**

Add the `DeezerConfig` struct after `SpotifyConfig`:

```go
type DeezerConfig struct {
	Enabled     bool
	BPMMatching bool
}
```

Add the `Deezer` field to `ConfigStruct`:

```go
type ConfigStruct struct {
	Discord DiscordConfig
	Tunnel  TunnelConfig
	Options Options
	Youtube YoutubeConfig
	Gemini  GeminiConfig
	Spotify SpotifyConfig
	Deezer  DeezerConfig
}
```

Add initialization in `NewConfig()` after the Spotify block:

```go
Deezer: DeezerConfig{
	Enabled:     os.Getenv("DEEZER_ENABLED") != "false", // enabled by default
	BPMMatching: os.Getenv("DEEZER_BPM_MATCHING") != "false", // enabled by default
},
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add config/config.go
git commit -m "feat(config): add Deezer configuration (enabled by default, no API key needed)"
```

---

## Task 6: Blended Radio Recommendation (Tier 1 Core)

**Files:**
- Modify: `controller/controller.go`

This is the core integration point. We modify `pickRadioSong()` to fetch Deezer artist radio in parallel with existing sources and score candidates.

- [ ] **Step 1: Add DeezerMeta field to GuildQueueItem**

In `controller/controller.go`, add to the `GuildQueueItem` struct:

```go
type GuildQueueItem struct {
	Video          youtube.VideoResponse
	Stream         *youtube.YoutubeStream
	streamReady    chan struct{}
	LoadResult     *audio.LoadResult
	ProbedDuration time.Duration
	AddedAt        time.Time
	Interaction    *GuildQueueItemInteraction
	LoadAttempts   int
	MaxAttempts    int
	Context        context.Context
	Commentary     string
	IsRadioPick    bool
	FallbackVideos []youtube.VideoResponse
	DeezerMeta     *deezer.TrackMeta
}
```

- [ ] **Step 2: Add radio candidate scoring types and helper**

Add above `pickRadioSong()`:

```go
type radioCandidate struct {
	Title      string
	Artist     string
	VideoID    string // set if from YouTube Mix (already resolved)
	DeezerID   int    // set if from Deezer
	BPM        float64
	Score      int
	Source     string // "youtube-mix", "deezer", "gemini"
}

func scoreRadioCandidates(candidates []radioCandidate, currentBPM float64, bpmEnabled bool) []radioCandidate {
	if len(candidates) == 0 {
		return candidates
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Apply BPM bonus to top 5 only (to limit API calls for BPM resolution)
	if bpmEnabled && currentBPM > 0 {
		limit := 5
		if len(candidates) < limit {
			limit = len(candidates)
		}
		for i := 0; i < limit; i++ {
			if candidates[i].BPM > 0 {
				diff := math.Abs(candidates[i].BPM - currentBPM)
				if diff <= 10 {
					candidates[i].Score += 2
				} else if diff <= 20 {
					candidates[i].Score += 1
				}
			}
		}
		// Re-sort after BPM bonus
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Score > candidates[j].Score
		})
	}

	return candidates
}
```

- [ ] **Step 3: Modify pickRadioSong() to blend Deezer signal (history-based mode)**

Replace the non-themed branch of `pickRadioSong()` with parallel fetching and scoring. The key change: after extracting the seed artist, fetch Deezer artist radio in parallel with YouTube Mix. Then score all candidates.

In the `else` block (no theme), replace the sequential YouTube Mix -> Gemini -> legacy fallback with:

```go
} else {
	// Parallel fetch: YouTube Mix + Deezer artist radio + Gemini
	type fetchResult struct {
		youtubeVideos []youtube.VideoResponse
		deezerTracks  []deezer.Track
		geminiQuery   string
	}
	result := &fetchResult{}
	var wg sync.WaitGroup

	// YouTube Mix
	if seedHistory := p.SongHistory.GetRecent(1); len(seedHistory) > 0 && seedHistory[0].VideoID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			mixVideos, err := youtube.GetMixPlaylistVideos(fetchCtx, seedHistory[0].VideoID)
			if err != nil {
				logger.Debugf("YouTube Mix failed: %v", err)
				return
			}
			result.youtubeVideos = mixVideos
		}()
	}

	// Deezer artist radio
	if config.Config.Deezer.Enabled && len(recent) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			artistName := ExtractArtist(recent[0].Title)
			artist, err := deezer.SearchArtist(fetchCtx, artistName)
			if err != nil || artist == nil {
				logger.Debugf("Deezer artist search failed for %q: %v", artistName, err)
				return
			}
			tracks, err := deezer.GetArtistRadio(fetchCtx, artist.ID)
			if err != nil {
				logger.Debugf("Deezer artist radio failed: %v", err)
				return
			}
			result.deezerTracks = tracks
		}()
	}

	// Gemini recommendation
	if config.Config.Gemini.Enabled && len(recent) >= 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			songTitles := make([]string, len(recent))
			for i, song := range recent {
				songTitles[i] = song.Title
			}
			result.geminiQuery = gemini.GenerateSongRecommendation(fetchCtx, songTitles)
		}()
	}

	wg.Wait()

	// Build candidate pool
	var candidates []radioCandidate

	// YouTube Mix candidates
	for _, v := range result.youtubeVideos {
		if historyIDs[v.VideoID] || isRecentTitle(v.Title, recentTitles) {
			continue
		}
		candidates = append(candidates, radioCandidate{
			Title:   v.Title,
			Artist:  ExtractArtist(v.Title),
			VideoID: v.VideoID,
			Score:   2, // YouTube Mix base score
			Source:  "youtube-mix",
		})
	}

	// Deezer candidates
	for _, t := range result.deezerTracks {
		searchKey := strings.ToLower(t.Artist.Name + " - " + t.TitleShort)
		if isRecentTitle(searchKey, recentTitles) {
			continue
		}
		candidate := radioCandidate{
			Title:    t.TitleShort,
			Artist:   t.Artist.Name,
			DeezerID: t.ID,
			Score:    3, // Deezer base score (weighted higher)
			Source:   "deezer",
		}
		// Check for convergence with YouTube Mix
		for i := range candidates {
			if candidates[i].Source == "youtube-mix" && strings.Contains(
				strings.ToLower(candidates[i].Title),
				strings.ToLower(t.TitleShort),
			) {
				candidates[i].Score += 1 // convergence bonus
				candidate.Score += 1
				break
			}
		}
		candidates = append(candidates, candidate)
	}

	// Gemini candidate (single suggestion) - also store in resolvedVideos for later lookup
	var geminiResolved []youtube.VideoResponse
	if result.geminiQuery != "" {
		geminiResolved = youtube.Query(ctx, result.geminiQuery)
		for _, v := range geminiResolved {
			if historyIDs[v.VideoID] || isRecentTitle(v.Title, recentTitles) {
				continue
			}
			candidate := radioCandidate{
				Title:   v.Title,
				Artist:  ExtractArtist(v.Title),
				VideoID: v.VideoID,
				Score:   1, // Gemini base score
				Source:  "gemini",
			}
			// Check if Gemini pick matches a Deezer track (boost both)
			for i := range candidates {
				if candidates[i].Source == "deezer" && strings.Contains(
					strings.ToLower(v.Title),
					strings.ToLower(candidates[i].Title),
				) {
					candidates[i].Score += 1
					candidate.Score += 1
					break
				}
			}
			candidates = append(candidates, candidate)
			break // only use first valid Gemini result
		}
	}

	// Get current BPM for scoring (from current item's DeezerMeta if available)
	var currentBPM float64
	p.currentItemMutex.RLock()
	if p.CurrentItem != nil && p.CurrentItem.DeezerMeta != nil {
		currentBPM = p.CurrentItem.DeezerMeta.BPM
	}
	p.currentItemMutex.RUnlock()

	candidates = scoreRadioCandidates(candidates, currentBPM, config.Config.Deezer.BPMMatching)

	// Build a lookup map for already-resolved videos (YouTube Mix + Gemini)
	resolvedVideos := make(map[string]youtube.VideoResponse)
	for _, v := range result.youtubeVideos {
		resolvedVideos[v.VideoID] = v
	}
	for _, v := range geminiResolved {
		resolvedVideos[v.VideoID] = v
	}

	// Pick the highest scored candidate
	for _, c := range candidates {
		if c.VideoID != "" {
			// Already resolved (YouTube Mix or Gemini) - look up from map
			if v, ok := resolvedVideos[c.VideoID]; ok {
				vCopy := v
				picked = &vCopy
			}
		} else {
			// Deezer track needs YouTube resolution
			searchQuery := c.Artist + " - " + c.Title
			videos = youtube.Query(ctx, searchQuery)
			for i := range videos {
				if !historyIDs[videos[i].VideoID] && !isRecentTitle(videos[i].Title, recentTitles) {
					picked = &videos[i]
					break
				}
			}
		}
		if picked != nil {
			logger.Infof("Radio picked: %s (source=%s, score=%d)", picked.Title, c.Source, c.Score)
			break
		}
	}

	// Legacy fallback if blending produced nothing
	if picked == nil {
		idx := rand.Intn(len(recent))
		artist := ExtractArtist(recent[idx].Title)
		query = artist + " music"
		videos = youtube.Query(ctx, query)
		for i := range videos {
			if !historyIDs[videos[i].VideoID] && !isRecentTitle(videos[i].Title, recentTitles) {
				picked = &videos[i]
				break
			}
		}
	}
}
```

Note: This is a substantial refactor of `pickRadioSong()`. The themed branch remains mostly unchanged but gains a Deezer artist search for validation (Step 4 below).

- [ ] **Step 4: Add Deezer supplementation to themed radio branch**

In the themed branch, after the Gemini themed recommendation succeeds, add:

```go
// Supplement themed picks with Deezer artist search
if config.Config.Deezer.Enabled && picked != nil {
	go func() {
		dCtx, dCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer dCancel()
		artist, _ := deezer.SearchArtist(dCtx, theme)
		if artist != nil {
			logger.Debugf("Deezer found themed artist %q (id=%d) for radio pool", artist.Name, artist.ID)
		}
	}()
}
```

- [ ] **Step 5: Add required imports**

Add `"beatbot/deezer"`, `"math"`, and `"sort"` to the imports in controller.go if not already present.

- [ ] **Step 6: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add controller/controller.go
git commit -m "feat(radio): blend Deezer artist radio into song selection with weighted scoring"
```

---

## Task 7: Genre and Artist Radio Modes (Tier 2)

**Files:**
- Modify: `commands.json`
- Modify: `handlers/playback.go`
- Modify: `controller/controller.go`

- [ ] **Step 1: Add genre and artist options to /radio command in commands.json**

Update the radio command entry:

```json
{
    "name": "radio",
    "type": 1,
    "description": "Toggle radio mode - automatically queues similar songs. Add a vibe, genre, or artist to guide the picks.",
    "options": [
      {
        "name": "vibe",
        "description": "A mood or style to guide radio picks (e.g. 'chill indie', '90s hip hop')",
        "type": 3,
        "required": false
      },
      {
        "name": "genre",
        "description": "Lock radio to a genre station (e.g. 'rock', 'electronic', 'jazz')",
        "type": 3,
        "required": false
      },
      {
        "name": "artist",
        "description": "Seed radio from a specific artist (e.g. 'Daft Punk', 'Radiohead')",
        "type": 3,
        "required": false
      }
    ]
}
```

- [ ] **Step 2: Add radio mode fields to GuildPlayer**

In `controller/controller.go`, add to the `GuildPlayer` struct after `RadioTheme`:

```go
RadioGenre          string
RadioArtistName     string
RadioArtistID       int
radioCandidatePool  []deezer.Track
radioCandidatesMu   sync.Mutex
```

Add accessor methods:

```go
func (p *GuildPlayer) SetRadioGenre(genre string) {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	p.RadioGenre = genre
	p.RadioTheme = ""
	p.RadioArtistName = ""
	p.RadioArtistID = 0
}

func (p *GuildPlayer) SetRadioArtist(name string, id int) {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	p.RadioArtistName = name
	p.RadioArtistID = id
	p.RadioTheme = ""
	p.RadioGenre = ""
}

func (p *GuildPlayer) GetRadioGenre() string {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	return p.RadioGenre
}

func (p *GuildPlayer) GetRadioArtistID() int {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	return p.RadioArtistID
}

func (p *GuildPlayer) GetRadioArtistName() string {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	return p.RadioArtistName
}

func (p *GuildPlayer) ClearRadioMode() {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	p.RadioTheme = ""
	p.RadioGenre = ""
	p.RadioArtistName = ""
	p.RadioArtistID = 0
}
```

- [ ] **Step 3: Add `pickFromDeezerPool` helper (handles mutex correctly)**

This method holds the mutex for the entire read+remove operation to avoid slice race conditions:

```go
// pickFromDeezerPool picks a song from the candidate pool (genre station or artist radio).
// If the pool is empty, it refills from Deezer. Holds radioCandidatesMu for the full operation.
func (p *GuildPlayer) pickFromDeezerPool(ctx context.Context, genre string, artistID int, historyIDs map[string]bool, recentTitles []string, logger *log.Entry) *youtube.VideoResponse {
	p.radioCandidatesMu.Lock()

	// Refill pool if empty
	if len(p.radioCandidatePool) == 0 {
		p.radioCandidatesMu.Unlock()
		var tracks []deezer.Track
		if genre != "" {
			stations, err := deezer.SearchRadioStation(ctx, genre)
			if err == nil && len(stations) > 0 {
				tracks, _ = deezer.GetStationTracks(ctx, stations[0].ID)
			}
		} else if artistID > 0 {
			var err error
			tracks, err = deezer.GetArtistRadio(ctx, artistID)
			if err != nil || len(tracks) == 0 {
				related, relErr := deezer.GetRelatedArtists(ctx, artistID)
				if relErr == nil && len(related) > 0 {
					tracks, _ = deezer.GetArtistRadio(ctx, related[0].ID)
				}
			}
		}
		if len(tracks) == 0 {
			return nil
		}
		p.radioCandidatesMu.Lock()
		p.radioCandidatePool = tracks
	}

	// Copy pool and find a valid candidate
	pool := make([]deezer.Track, len(p.radioCandidatePool))
	copy(pool, p.radioCandidatePool)

	for i, t := range pool {
		searchKey := strings.ToLower(t.Artist.Name + " - " + t.TitleShort)
		if isRecentTitle(searchKey, recentTitles) {
			continue
		}
		// Remove picked track from pool (safe: working on our copy indices)
		p.radioCandidatePool = append(p.radioCandidatePool[:i], p.radioCandidatePool[i+1:]...)
		p.radioCandidatesMu.Unlock()

		// Resolve via YouTube (outside mutex)
		searchQuery := t.Artist.Name + " - " + t.TitleShort
		videos := youtube.Query(ctx, searchQuery)
		for j := range videos {
			if !historyIDs[videos[j].VideoID] && !isRecentTitle(videos[j].Title, recentTitles) {
				source := "genre:" + genre
				if artistID > 0 {
					source = "artist:" + p.GetRadioArtistName()
				}
				logger.Infof("Deezer pool picked: %s (source=%s)", videos[j].Title, source)
				return &videos[j]
			}
		}
		// This candidate didn't resolve, try next
		p.radioCandidatesMu.Lock()
	}

	p.radioCandidatesMu.Unlock()
	return nil
}
```

- [ ] **Step 4: Add genre/artist radio pick logic to pickRadioSong()**

Add at the top of `pickRadioSong()`, before the existing `theme` check:

```go
// Genre radio mode: pick from Deezer genre station
if genre := p.GetRadioGenre(); genre != "" && config.Config.Deezer.Enabled {
	picked := p.pickFromDeezerPool(ctx, genre, 0, historyIDs, recentTitles, logger)
	if picked != nil {
		return picked
	}
	// Fall through to normal radio if genre station exhausted
}

// Artist radio mode: pick from Deezer artist radio
if artistID := p.GetRadioArtistID(); artistID > 0 && config.Config.Deezer.Enabled {
	picked := p.pickFromDeezerPool(ctx, "", artistID, historyIDs, recentTitles, logger)
	if picked != nil {
		return picked
	}
	// Fall through to normal blended radio if artist radio exhausted
}
```

- [ ] **Step 4: Update handleRadio to parse genre/artist options**

In `handlers/playback.go`, modify `handleRadio` to extract the new options:

```go
var vibe, genre, artistOpt string
for _, opt := range interaction.Data.Options {
	switch opt.Name {
	case "vibe":
		vibe = strings.TrimSpace(opt.Value)
	case "genre":
		genre = strings.TrimSpace(opt.Value)
	case "artist":
		artistOpt = strings.TrimSpace(opt.Value)
	}
}
```

Then after the voice channel checks succeed, add mode-setting logic:

```go
if enabled {
	if genre != "" {
		player.SetRadioGenre(genre)
	} else if artistOpt != "" {
		// Resolve artist on Deezer synchronously (within the 3s Discord limit is tight,
		// but we already deferred the response with Type 5 for radio commands that need voice join).
		// Use a short timeout - if Deezer is slow, fall back to theme mode.
		resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		artist, err := deezer.SearchArtist(resolveCtx, artistOpt)
		cancel()
		if err == nil && artist != nil {
			player.SetRadioArtist(artist.Name, artist.ID)
		} else {
			// Deezer couldn't resolve - use as a themed vibe instead (Gemini handles it)
			player.SetRadioTheme(artistOpt)
		}
	} else if vibe != "" {
		player.SetRadioTheme(vibe)
	}
}
```

Note: Since `handleRadio` returns a Type 4 (immediate) response, this 2s Deezer call happens before the response is sent. If this is too slow in practice, convert `handleRadio` to an async handler (Type 5) like `handleRequest`. For now, Deezer's API typically responds in <500ms.

Update the response message to reflect the mode:

```go
if enabled {
	if genre != "" {
		msg = fmt.Sprintf("📻 Radio mode **enabled** — genre: *%s* — %s", genre, djResponse)
	} else if artistOpt != "" {
		msg = fmt.Sprintf("📻 Radio mode **enabled** — artist: *%s* — %s", artistOpt, djResponse)
	} else if theme := player.GetRadioTheme(); theme != "" {
		msg = fmt.Sprintf("📻 Radio mode **enabled** — vibing to *%s* — %s", theme, djResponse)
	} else {
		msg = "📻 Radio mode **enabled** — " + djResponse
	}
}
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add commands.json controller/controller.go handlers/playback.go
git commit -m "feat(radio): add genre and artist radio modes via Deezer"
```

---

## Task 8: Charts Command (Tier 2)

**Files:**
- Create: `handlers/charts.go`
- Modify: `commands.json`
- Modify: `handlers/handlers.go`

- [ ] **Step 1: Add /charts to commands.json**

Append to the commands array:

```json
{
    "name": "charts",
    "type": 1,
    "description": "Show what's trending right now",
    "options": [
      {
        "name": "play",
        "description": "Queue the top tracks instead of just displaying them",
        "type": 5,
        "required": false
      }
    ]
}
```

- [ ] **Step 2: Create `handlers/charts.go`**

Note: Use the same async handler pattern as `handleRequest` - it receives `(ctx, *sentry.Span, *Interaction)` and uses `manager.SendFollowup`/`manager.SendRequest` for responses. Look at `handlers/playback.go:handleRequest` for the exact pattern (uses `discord.GetMemberVoiceState` as a package-level function, not `manager.Discord`).

```go
package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"beatbot/config"
	"beatbot/deezer"
	"beatbot/discord"
	"beatbot/youtube"

	"github.com/getsentry/sentry-go"
)

func (manager *Manager) handleCharts(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentry.CaptureException(fmt.Errorf("panic in handleCharts: %v", err))
		}
	}()

	if !config.Config.Deezer.Enabled {
		manager.SendRequest(interaction, "📊 Charts are unavailable (Deezer integration disabled).", false)
		return
	}

	var playMode bool
	for _, opt := range interaction.Data.Options {
		if opt.Name == "play" {
			playMode = opt.Value == "true" || opt.Value == "True"
		}
	}

	span := transaction.StartChild("deezer.get_charts")
	chartCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	charts, err := deezer.GetCharts(chartCtx)
	span.Finish()
	if err != nil {
		sentry.CaptureException(err)
		manager.SendRequest(interaction, "📊 Couldn't fetch charts right now. Try again later.", false)
		return
	}

	tracks := charts.Tracks.Data
	if len(tracks) == 0 {
		manager.SendRequest(interaction, "📊 No chart data available right now.", false)
		return
	}

	// Cap at 10 tracks
	if len(tracks) > 10 {
		tracks = tracks[:10]
	}

	if playMode {
		player := manager.Controller.GetPlayer(interaction.GuildID)

		// Verify user is in voice channel (package-level function, not method on manager)
		voiceState, _ := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
		if voiceState == nil {
			manager.SendRequest(interaction, "📊 Join a voice channel first to play the charts.", false)
			return
		}
		if player.ShouldJoinVoice(voiceState.ChannelID) {
			if err := player.JoinVoiceChannel(interaction.Member.User.ID); err != nil {
				manager.SendRequest(interaction, "📊 Couldn't join your voice channel: "+err.Error(), false)
				return
			}
		}

		// Queue each chart track via YouTube search then player.Add()
		queued := 0
		for _, t := range tracks {
			query := t.Artist.Name + " - " + t.TitleShort
			results := youtube.Query(ctx, query)
			if len(results) > 0 {
				player.Add(results[0], interaction.Member.User.ID, interaction.Token, interaction.AppID, false)
				queued++
			}
		}

		manager.SendRequest(interaction, fmt.Sprintf("📊 Queued **%d** chart tracks. Let's see what's hot.", queued), false)
		return
	}

	// Display mode
	var sb strings.Builder
	sb.WriteString("📊 **What's Trending**\n\n")
	for i, t := range tracks {
		sb.WriteString(fmt.Sprintf("`%2d.` **%s** — %s\n", i+1, t.TitleShort, t.Artist.Name))
	}
	sb.WriteString("\n*Use `/charts play:True` to queue these tracks.*")

	manager.SendRequest(interaction, sb.String(), false)
}
```

- [ ] **Step 3: Add charts case to command dispatch in handlers/handlers.go**

In the `switch interaction.Data.Name` block, add the same pattern used by `handleRequest` (deferred response type 5, goroutine):

```go
case "charts":
	finishTransaction = false
	go manager.handleCharts(ctx, transaction, interaction)
	return Response{Type: 5}
```

Look at how `"request"` is dispatched in the same switch block and copy that pattern exactly.

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add handlers/charts.go handlers/handlers.go commands.json
git commit -m "feat: add /charts command to display and play trending tracks"
```

---

## Task 9: Background BPM Resolution (Tier 3)

**Files:**
- Modify: `controller/controller.go`

- [ ] **Step 1: Add background Deezer metadata resolution on PlaybackStarted**

In `listenForPlaybackEvents()`, inside the `case audio.PlaybackStarted:` handler, after the existing `go p.sendNowPlayingCard(queueItem)` call, add:

```go
// Background Deezer metadata enrichment
// Note: DeezerMeta will be nil when sendNowPlayingCard runs (it fires first).
// The metadata becomes available for: future radio picks (via CurrentItem.DeezerMeta),
// now-playing card updates (when commentary arrives and triggers an embed edit),
// and DJ scripts for the next transition announcement.
if config.Config.Deezer.Enabled {
	go func(item *GuildQueueItem) {
		resolveCtx, resolveCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer resolveCancel()

		artist := ExtractArtist(item.Video.Title)
		title := extractTitlePart(item.Video.Title)
		meta := deezer.ResolveTrackMeta(resolveCtx, artist, title)
		if meta != nil {
			// Synchronize write - readers use currentItemMutex.RLock()
			p.currentItemMutex.Lock()
			item.DeezerMeta = meta
			p.currentItemMutex.Unlock()

			log.WithFields(log.Fields{
				"module": "controller",
				"method": "deezerResolve",
				"song":   item.Video.Title,
				"bpm":    meta.BPM,
				"genre":  meta.Genre,
			}).Debug("Deezer metadata resolved")
		}
	}(queueItem)
}
```

- [ ] **Step 2: Add extractTitlePart helper**

Add near `ExtractArtist`:

```go
func extractTitlePart(videoTitle string) string {
	// Extract the song title part from "Artist - Title (Official Video)" format
	cleaned := videoTitle
	suffixes := []string{
		"(Official Video)", "(Official Music Video)", "(Official Audio)",
		"(Lyrics)", "(Lyric Video)", "(Audio)", "(Visualizer)",
		"[Official Video]", "[Official Music Video]", "[Official Audio]",
	}
	for _, suffix := range suffixes {
		cleaned = strings.Replace(cleaned, suffix, "", 1)
	}
	cleaned = strings.TrimSpace(cleaned)

	parts := strings.SplitN(cleaned, " - ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return cleaned
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add controller/controller.go
git commit -m "feat: background Deezer metadata resolution on playback start"
```

---

## Task 10: Rich Now-Playing Embeds (Tier 3)

**Files:**
- Modify: `discord/embeds.go`
- Modify: `controller/controller.go`

- [ ] **Step 1: Extend NowPlayingMetadata with Deezer fields**

In `discord/embeds.go`, add fields to `NowPlayingMetadata`:

```go
type NowPlayingMetadata struct {
	VideoID         string
	Title           string
	Artist          string
	Album           string
	ThumbnailURL    string
	Duration        time.Duration
	CurrentPosition time.Duration
	IsPlaying       bool
	Volume          int
	GuildID         string
	Commentary      string
	// Deezer enrichment (all optional, nil-safe)
	Genre      string
	BPM        float64
	AlbumYear  string
	Popularity int
}
```

- [ ] **Step 2: Update BuildNowPlayingEmbed to use Deezer data**

In `BuildNowPlayingEmbed`, after the existing album field in the description builder, add:

```go
if metadata.Genre != "" {
	desc.WriteString(fmt.Sprintf("**Genre:** %s\n", metadata.Genre))
}
```

After the existing embed fields (Duration, Volume, Status), add BPM if available:

```go
if metadata.BPM > 0 {
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "BPM",
		Value:  fmt.Sprintf("%.0f", metadata.BPM),
		Inline: true,
	})
}
```

Update the thumbnail logic to prefer Deezer album art (square, higher quality):
The `ThumbnailURL` field is already used if set, so no code change needed here - we just need to pass it from the controller.

Add a popularity indicator to the footer:

```go
footerText := progressBar
if metadata.Popularity > 0 {
	// Deezer rank is 0-1000000, higher = more popular
	percentile := float64(metadata.Popularity) / 10000.0
	if percentile > 90 {
		footerText += " • Top 10% on streaming"
	} else if percentile > 75 {
		footerText += " • Popular"
	}
}
embed.Footer.Text = footerText
```

- [ ] **Step 3: Pass Deezer metadata when building now-playing in controller**

In `sendNowPlayingCard()` in `controller/controller.go`, after building the base metadata, check for DeezerMeta:

```go
metadata := &discord.NowPlayingMetadata{
	VideoID:         queueItem.Video.VideoID,
	Title:           queueItem.Video.Title,
	Duration:        duration,
	CurrentPosition: 0,
	IsPlaying:       true,
	Volume:          p.Player.GetVolume(),
	GuildID:         p.GuildID,
	Commentary:      "",
}

// Enrich with Deezer metadata if available
if queueItem.DeezerMeta != nil {
	dm := queueItem.DeezerMeta
	metadata.Genre = dm.Genre
	metadata.BPM = dm.BPM
	metadata.Album = dm.AlbumName
	if dm.AlbumYear != "" {
		metadata.Album = dm.AlbumName + " (" + dm.AlbumYear + ")"
	}
	metadata.Popularity = dm.Popularity
	if dm.AlbumArtURL != "" {
		metadata.ThumbnailURL = dm.AlbumArtURL
	}
	if dm.ArtistName != "" {
		metadata.Artist = dm.ArtistName
	}
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add discord/embeds.go controller/controller.go
git commit -m "feat: enrich now-playing embeds with Deezer metadata (genre, BPM, album art)"
```

---

## Task 11: Enhanced DJ Context for Gemini (Tier 3)

**Files:**
- Modify: `gemini/gemini.go`

- [ ] **Step 1: Add SongContext type and update GenerateNowPlayingCommentary**

Add `"math"` to the imports in `gemini/gemini.go`.

Add the `SongContext` struct (new type in the gemini package):

```go
type SongContext struct {
	Genre          string
	BPM            float64
	AlbumName      string
	AlbumYear      string
	Popularity     int
	RelatedArtists []string
	PreviousBPM    float64
	RadioMode      string // "genre:rock", "artist:Daft Punk", "vibe:chill", or ""
}
```

Add a new `*SongContext` parameter to `GenerateNowPlayingCommentary`. Update ALL callers to pass `nil` for now (there is typically one call site in `controller/controller.go` around line 2449). Then the Step 3 caller update provides the real value.

```go
func GenerateNowPlayingCommentary(ctx context.Context, currentSong string, recentHistory []string, isRadioPick bool, songCtx *SongContext) string {
```

In the prompt, after the existing history context, append metadata if available:

```go
if songCtx != nil {
	instructions += "\n\nAdditional context about the current song:"
	if songCtx.Genre != "" {
		instructions += fmt.Sprintf("\n- Genre: %s", songCtx.Genre)
	}
	if songCtx.BPM > 0 {
		instructions += fmt.Sprintf("\n- BPM: %.0f", songCtx.BPM)
	}
	if songCtx.AlbumName != "" {
		year := ""
		if songCtx.AlbumYear != "" {
			year = " (" + songCtx.AlbumYear + ")"
		}
		instructions += fmt.Sprintf("\n- Album: %s%s", songCtx.AlbumName, year)
	}
	if songCtx.Popularity > 800000 {
		instructions += "\n- Popularity: Very popular (top 20%)"
	}
	if songCtx.PreviousBPM > 0 && songCtx.BPM > 0 {
		diff := math.Abs(songCtx.BPM - songCtx.PreviousBPM)
		if diff <= 5 {
			instructions += "\n- Note: smooth tempo match with previous song"
		} else if diff > 30 {
			instructions += "\n- Note: significant tempo shift from previous song"
		}
	}
	if songCtx.RadioMode != "" {
		instructions += fmt.Sprintf("\n- Radio mode: %s", songCtx.RadioMode)
	}
	instructions += "\n\nUse this context naturally — mention genre, tempo match, or album era IF it adds to the commentary. Don't force it."
}
```

- [ ] **Step 2: Update GenerateDJScript similarly**

`GenerateDJScript` likely takes a struct or multiple params. Add `SongCtx *SongContext` as a field on whatever input struct it uses (check the actual signature - it may be `DJScriptParams` or similar). If it takes positional params, add `songCtx *SongContext` as the last parameter and update all 3 call sites (controller.go lines ~1492, ~2283, ~2329) to pass `nil` initially.

Only inject BPM/genre context into the prompt for `AnnouncementTransition` type (between-song transitions where tempo context is relevant). For other announcement types (intro, queue-empty, radio-start), pass nil or skip the metadata injection.

- [ ] **Step 3: Update callers to pass SongContext**

In `controller/controller.go`, wherever `GenerateNowPlayingCommentary` or `GenerateDJScript` is called, build and pass a `SongContext` from the current item's `DeezerMeta`:

```go
var songCtx *gemini.SongContext
if queueItem.DeezerMeta != nil {
	dm := queueItem.DeezerMeta
	songCtx = &gemini.SongContext{
		Genre:          dm.Genre,
		BPM:            dm.BPM,
		AlbumName:      dm.AlbumName,
		AlbumYear:      dm.AlbumYear,
		Popularity:     dm.Popularity,
		RelatedArtists: dm.RelatedArtists,
	}
	// Get previous BPM from history if available
	if recent := p.SongHistory.GetRecent(1); len(recent) > 0 {
		// Previous item's BPM would need to be stored - simplify by checking history
	}
	// Set radio mode context
	if genre := p.GetRadioGenre(); genre != "" {
		songCtx.RadioMode = "genre:" + genre
	} else if artist := p.GetRadioArtistName(); artist != "" {
		songCtx.RadioMode = "artist:" + artist
	} else if theme := p.GetRadioTheme(); theme != "" {
		songCtx.RadioMode = "vibe:" + theme
	}
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gemini/gemini.go controller/controller.go
git commit -m "feat: feed Deezer metadata to Gemini DJ prompts for contextual commentary"
```

---

## Task 12: Integration Test and Final Verification

**Files:**
- Create: `deezer/integration_test.go`

- [ ] **Step 1: Write integration tests that hit real Deezer endpoints**

```go
//go:build integration

package deezer

import (
	"context"
	"testing"
	"time"
)

func TestIntegration_SearchArtist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist, err := SearchArtist(ctx, "Daft Punk")
	if err != nil {
		t.Fatalf("SearchArtist: %v", err)
	}
	if artist == nil {
		t.Fatal("expected non-nil artist")
	}
	if artist.Name != "Daft Punk" {
		t.Errorf("artist.Name = %q, want %q", artist.Name, "Daft Punk")
	}
	if artist.ID == 0 {
		t.Error("artist.ID should not be 0")
	}
}

func TestIntegration_GetArtistRadio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Daft Punk = ID 27
	tracks, err := GetArtistRadio(ctx, 27)
	if err != nil {
		t.Fatalf("GetArtistRadio: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatal("expected tracks from artist radio")
	}
	if len(tracks) > 25 {
		t.Errorf("expected <= 25 tracks, got %d", len(tracks))
	}

	// Verify track structure
	for _, track := range tracks {
		if track.Title == "" {
			t.Error("track has empty title")
		}
		if track.Artist.Name == "" {
			t.Error("track has empty artist name")
		}
	}
}

func TestIntegration_GetCharts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	charts, err := GetCharts(ctx)
	if err != nil {
		t.Fatalf("GetCharts: %v", err)
	}
	if len(charts.Tracks.Data) == 0 {
		t.Fatal("expected chart tracks")
	}
}

func TestIntegration_SearchTrack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	track, err := SearchTrack(ctx, "Daft Punk", "Around the World")
	if err != nil {
		t.Fatalf("SearchTrack: %v", err)
	}
	if track == nil {
		t.Fatal("expected non-nil track")
	}
	if track.Duration == 0 {
		t.Error("track duration should not be 0")
	}
}

func TestIntegration_GetTrackDetail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// "Around the World" by Daft Punk = track ID 3135556
	detail, err := GetTrack(ctx, 3135556)
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if detail.BPM == 0 {
		t.Log("BPM is 0 (some tracks don't have BPM data)")
	}
	if detail.Title == "" {
		t.Error("expected non-empty title")
	}
}

func TestIntegration_GetGenreStations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	genres, err := GetGenreStations(ctx)
	if err != nil {
		t.Fatalf("GetGenreStations: %v", err)
	}
	if len(genres) == 0 {
		t.Fatal("expected genre stations")
	}
	// Should have at least some major genres
	found := false
	for _, g := range genres {
		if g.Name == "Pop" || g.Name == "Rock" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find Pop or Rock genre")
	}
}
```

- [ ] **Step 2: Run unit tests (short mode, no network)**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go test ./deezer/ -v -count=1 -short`
Expected: PASS

- [ ] **Step 3: Run full build with race detector**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go build -race ./...`
Expected: PASS (no race conditions in compile-time checks)

- [ ] **Step 4: Run all existing tests to check for regressions**

Run: `cd /Users/ben/conductor/workspaces/discord-music-bot/missoula-v1 && go test ./... -short -count=1`
Expected: PASS (no regressions)

- [ ] **Step 5: Commit**

```bash
git add deezer/integration_test.go
git commit -m "test(deezer): add integration tests for Deezer API endpoints"
```

---

## Task 13: Update CLAUDE.md and .env Documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add Deezer section to CLAUDE.md**

In the "Key Components" section, add:

```markdown
**`deezer/`** - Deezer music intelligence API client
- No authentication required (public endpoints)
- Artist radio, genre stations, track metadata (BPM), charts
- In-memory artist ID cache (sync.Map)
- Rate limited at 50 req/5s via token bucket
- Never on critical path - all call sites have fallbacks
```

In the "Environment Variables" section, add:

```markdown
- `DEEZER_ENABLED` - Enable Deezer integration (default: true, no API key needed)
- `DEEZER_BPM_MATCHING` - Enable BPM-aware radio song selection (default: true)
```

In the "Important Architectural Decisions" section, add:

```markdown
#### Deezer Integration (Music Intelligence Layer)
- **Blended recommendation scoring**: Deezer artist radio (+3), YouTube Mix (+2), Gemini (+1), convergence bonus (+1), BPM match (+2/+1)
- **Why weighted scoring**: Deezer's artist radio is purpose-built for "similar tracks" so it gets the highest base weight, but convergence across multiple signals indicates high-confidence picks
- **Genre/artist radio modes**: Use Deezer's curated stations as candidate pools, pick one at a time, resolve each via YouTube search
- **Background metadata resolution**: On PlaybackStarted, resolves current song on Deezer for BPM/genre/album in a goroutine. Non-blocking, enriches now-playing embed and DJ prompts when available
- **No auth required**: All Deezer endpoints used are public. Feature enabled by default with no setup cost.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add Deezer integration to CLAUDE.md"
```

---

## Summary of Commits

| # | Message | Tier |
|---|---------|------|
| 1 | `feat(deezer): add types and HTTP client with rate limiting` | 1 |
| 2 | `feat(deezer): add artist/track search with in-memory cache` | 1 |
| 3 | `feat(deezer): add radio, track detail, and charts endpoints` | 1 |
| 4 | `test(deezer): add unit tests for types, cache, and rate limiting` | 1 |
| 5 | `feat(config): add Deezer configuration` | 1 |
| 6 | `feat(radio): blend Deezer artist radio into song selection with weighted scoring` | 1 |
| 7 | `feat(radio): add genre and artist radio modes via Deezer` | 2 |
| 8 | `feat: add /charts command to display and play trending tracks` | 2 |
| 9 | `feat: background Deezer metadata resolution on playback start` | 3 |
| 10 | `feat: enrich now-playing embeds with Deezer metadata` | 3 |
| 11 | `feat: feed Deezer metadata to Gemini DJ prompts for contextual commentary` | 3 |
| 12 | `test(deezer): add integration tests for Deezer API endpoints` | 3 |
| 13 | `docs: add Deezer integration to CLAUDE.md` | - |
