# Deezer Music Intelligence Layer

## Context

The bot's radio mode currently picks songs via three signals: YouTube Mix playlists (primary), Gemini AI recommendations (secondary), and legacy artist search (fallback). These work but lack music-domain intelligence. YouTube Mix is opaque (no metadata about why a song was picked), and Gemini hallucinates occasionally.

Deezer's public API provides purpose-built music intelligence with no authentication required: artist-based radio (algorithmically similar tracks), genre-organized stations, track metadata (BPM, genre, popularity), and charts. Integrating it as a blended signal improves recommendation quality, enables new discovery features, and gives the DJ personality richer context for commentary.

## Architecture Overview

```
                    ┌─────────────────────────────────────────────┐
                    │              pickRadioSong()                  │
                    │                                               │
                    │  ┌───────────┐ ┌───────────┐ ┌───────────┐  │
                    │  │ YouTube   │ │  Deezer   │ │  Gemini   │  │
                    │  │   Mix     │ │Artist Radio│ │   Reco    │  │
                    │  └─────┬─────┘ └─────┬─────┘ └─────┬─────┘  │
                    │        │             │             │          │
                    │        └──────┬──────┘─────────────┘          │
                    │               ▼                                │
                    │        Score & Blend                           │
                    │        (BPM bonus if available)                │
                    │               │                                │
                    │               ▼                                │
                    │        "Artist - Title"                        │
                    │               │                                │
                    │               ▼                                │
                    │        YouTube Search → Queue                  │
                    └─────────────────────────────────────────────┘

    ┌─────────────────────────────────────────────────────────────────┐
    │                    Background Metadata Enrichment                 │
    │                                                                   │
    │  On PlaybackStarted:                                              │
    │    → Search Deezer for current song                               │
    │    → Fetch /track/{id} for BPM, genre, album, popularity          │
    │    → Store on GuildQueueItem.DeezerMeta                           │
    │    → Feed to now-playing embed + Gemini DJ prompts                │
    │                                                                   │
    └─────────────────────────────────────────────────────────────────┘
```

## Package: `deezer/`

New package following the same conventions as `spotify/` and `applemusic/`.

### Files

- **`client.go`** - HTTP client with token-bucket rate limiting (~50 req/5s). Base URL `https://api.deezer.com`. No auth. Timeout 10s. Sentry spans on all requests.
- **`search.go`** - `SearchArtist(name) (*Artist, error)`, `SearchTrack(artist, title) (*Track, error)`. Handles Deezer's advanced query syntax (`artist:"X" track:"Y"`).
- **`radio.go`** - `GetArtistRadio(artistID) ([]Track, error)`, `GetGenreStations() ([]Genre, error)`, `GetStationTracks(stationID) ([]Track, error)`, `SearchRadioStation(query) ([]RadioStation, error)`.
- **`tracks.go`** - `GetTrack(trackID) (*TrackDetail, error)` for full metadata (BPM, duration, rank, explicit, album).
- **`charts.go`** - `GetCharts() (*ChartResponse, error)` returning top tracks, albums, artists.
- **`types.go`** - All response structs: `Track`, `TrackDetail`, `Artist`, `Album`, `RadioStation`, `Genre`, `ChartResponse`.

### Artist Resolution Cache

In-memory LRU map (`sync.Map` or similar) of `normalizedArtistName → deezerArtistID`. Avoids repeated `/search/artist` calls for the same artist across radio picks. Not persisted to disk (acceptable cold start cost).

### Error Philosophy

Deezer is never on the critical path. Every call site has a fallback. On error: log at WARN, capture to Sentry with `area: "deezer"`, return nil/empty, let caller proceed without Deezer data.

## Recommendation Blending

### History-Based Radio (no theme)

```
1. Extract artist name from most recent SongHistory entry
2. Fetch IN PARALLEL (context with 5s timeout each):
   a) YouTube Mix: youtube.GetMixPlaylistVideos(lastVideoID)
   b) Deezer:      deezer.SearchArtist(artistName) → deezer.GetArtistRadio(id)
   c) Gemini:      gemini.GenerateSongRecommendation(history)
3. Build candidate pool:
   - YouTube Mix tracks (already have video IDs)
   - Deezer radio tracks (need YouTube resolution later)
   - Gemini suggestion (single search query)
4. Deduplicate against SongHistory (by normalized title)
5. Score candidates:
   - In Deezer artist radio:           +3
   - In YouTube Mix:                    +2
   - In BOTH Deezer + YouTube Mix:      +1 bonus (convergence)
   - Matches Gemini suggestion:         +1
   - BPM within ±10 of current:        +2 (if BPM known)
   - BPM within ±20 of current:        +1 (if BPM known)
6. Pick highest-scored candidate
7. If Deezer-sourced: resolve via YouTube search "artist - title"
8. FALLBACK: If blending produces 0 candidates, fall through to existing sequential logic
```

### Themed Radio (vibe set)

```
1. Gemini GenerateThemedRecommendation(vibe, history) → "Artist - Title"
2. Deezer SearchArtist(vibe) → if top match found, GetArtistRadio(id)
3. If Gemini's pick appears in Deezer's pool → high confidence, use it
4. If not → still use Gemini's pick (themed/creative picks may not map to Deezer's model)
5. Deezer pool used as supplementary candidates for future picks in this theme session
```

### Genre Radio Mode

When `/radio genre:<X>` is active:
```
1. Search Deezer /search/radio?q={genre} or map to known genre ID
2. Fetch /radio/{station_id}/tracks → 25 tracks
3. Queue first 3 immediately (resolving each via YouTube search), store rest as candidate pool
4. When needing more: pick next from pool, or re-fetch station (Deezer randomizes order)
5. BPM scoring still applies on top when selecting from pool
```

### Artist Radio Mode

When `/radio artist:<X>` is active:
```
1. Resolve artist via Deezer search
2. Fetch /artist/{id}/radio → 25 tracks
3. Queue first 3, store rest as candidate pool
4. When pool depletes: fetch /artist/{id}/related → pick a related artist → fetch their radio
5. This creates a natural drift through related artists (like a real radio station)
```

## New Commands

### `/radio` enhancements

Add two new optional parameters to the existing `/radio` slash command:

| Parameter | Type | Description |
|-----------|------|-------------|
| `genre` | String (autocomplete) | Lock radio to a Deezer genre station (e.g., "rock", "electronic", "jazz") |
| `artist` | String | Seed radio from a specific artist's Deezer radio |

Precedence: `artist` > `genre` > `vibe` > history-based. Only one mode active at a time (setting one clears the others).

Autocomplete for `genre` parameter uses cached list from `/radio/genres` (refreshed hourly).

### `/charts` command

New slash command registered in `commands.json`:

| Option | Type | Description |
|--------|------|-------------|
| `play` | Boolean | If true, queue the chart tracks. If false (default), just display. |

Response: Discord embed showing top 10 tracks with artist, title, and position. If `play` is true, queues them and starts playback.

## Audio Intelligence: BPM-Aware Selection

### Metadata Resolution

On `PlaybackStarted` event (already exists in `listenForPlaybackEvents`):
1. Spawn a goroutine to resolve current song on Deezer
2. Parse "artist - title" from video title (reuse existing extraction from `helpers/`)
3. `deezer.SearchTrack(artist, title)` → if match, `deezer.GetTrack(id)`
4. Store result on `GuildQueueItem.DeezerMeta`:
   ```go
   type TrackMeta struct {
       DeezerID     int
       BPM          float64
       Genre        string
       AlbumName    string
       AlbumYear    string
       AlbumArtURL  string
       Popularity   int  // 0-1000000 rank
       Explicit     bool
       Duration     int  // seconds
   }
   ```
5. Also store on `SongHistory` entry for use in future radio picks

### BPM Scoring

Deezer's `/artist/{id}/radio` and `/radio/{id}/tracks` responses include basic track info but not BPM. To get BPM for candidates without 25 individual API calls:
- Fetch `/track/{id}` only for the **top 5 scored candidates** (after initial scoring without BPM)
- Apply BPM bonus as a re-ranking step on those 5, then pick final winner
- This caps BPM-related API calls at 5 per radio pick cycle

Scoring rules:
- Current song's BPM known (from DeezerMeta on the playing item):
  - Candidate BPM within ±10: **+2**
  - Candidate BPM within ±20: **+1**
  - Candidate BPM >30 away: +0 (no penalty)
- Current song BPM unknown: skip BPM scoring entirely

### Tempo Drift Prevention

Track BPM of last 5 songs in SongHistory. Compute median. If current song's BPM is >25 away from the session median, slightly prefer candidates closer to the median (+1 bonus for within ±10 of median). This prevents radio from accidentally ratcheting up or down in tempo over a long session.

## Presentation Enhancements

### Rich Now-Playing Embed

Current embed fields: title, URL, thumbnail, requester, duration.

Enhanced (when DeezerMeta available):
- **Thumbnail**: Use Deezer album art (square, high-res) instead of YouTube thumbnail (16:9, often has annotations)
- **Genre field**: Inline field showing genre tag
- **BPM field**: Inline field showing tempo
- **Album field**: "Album Name (Year)" inline field
- **Footer**: Popularity indicator (e.g., "Top 5% on streaming" derived from rank)

Fallback: If DeezerMeta is nil, display existing YouTube-only embed unchanged.

### DJ Context in Gemini Prompts

Enhance `GenerateNowPlayingCommentary()` and `GenerateDJScript()` with structured metadata:

```
Additional context about the current song:
- Genre: Electronic / French House
- BPM: 121
- Album: Homework (1997)  
- Popularity: Very popular (top 5%)
- Related artists: Justice, Breakbot, Kavinsky
- Previous song: "Da Funk" at 118 BPM (smooth tempo match)
- Radio mode: artist-based, seeded from Daft Punk
```

This enables the DJ to make genre-aware observations, note tempo matches, reference the album era, and acknowledge artist connections.

## Configuration

New environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DEEZER_ENABLED` | `true` | Enable/disable Deezer integration entirely |
| `DEEZER_BPM_MATCHING` | `true` | Enable BPM-aware song selection |
| `DEEZER_RATE_LIMIT` | `50` | Requests per 5-second window |

Unlike Spotify, Deezer requires no API keys or credentials. The feature is enabled by default since there's no setup cost.

## Implementation Tiers

### Tier 1: Core (ship first)
- `deezer/` package with client, search, radio, types
- Artist resolution + caching
- Blended scoring in `pickRadioSong()` (history-based mode)
- Basic error handling and Sentry integration

### Tier 2: New Features
- `/radio genre:` and `/radio artist:` parameters
- `/charts` command
- Themed radio Deezer supplementation
- Genre autocomplete

### Tier 3: Intelligence & Polish
- Background BPM resolution on PlaybackStarted
- BPM-aware scoring in candidate selection
- Tempo drift prevention
- Rich now-playing embeds with Deezer metadata
- Enhanced Gemini DJ prompts with metadata context

## Verification Plan

1. **Unit tests** for `deezer/` package: mock HTTP responses, verify parsing, test rate limiting
2. **Integration test**: Call real Deezer endpoints (no auth needed) to verify response shapes haven't changed
3. **Radio blending**: Run radio mode, verify logs show Deezer candidates being scored alongside YouTube Mix
4. **Genre/artist radio**: Test `/radio genre:rock` and `/radio artist:daft punk` produce valid queues
5. **Charts**: Test `/charts` displays correctly, `/charts play` queues tracks
6. **BPM**: Verify BPM metadata appears on now-playing embeds when Deezer match found
7. **Fallback**: Disable Deezer (env var), verify radio mode works unchanged via existing paths
8. **Build**: `go build` passes, `go vet` clean, race detector clean
