package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

// artistCache holds normalized artist name -> *Artist lookups so repeated
// resolutions (e.g. radio/related-artist flows) don't keep hitting the API.
var artistCache sync.Map

// normalizeArtistName lowercases and trims an artist name so cache lookups
// are insensitive to casing and incidental whitespace.
func normalizeArtistName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// SearchArtist resolves an artist by name, using an in-memory cache to avoid
// repeat lookups for the same artist.
func SearchArtist(ctx context.Context, name string) (*Artist, error) {
	key := normalizeArtistName(name)
	if key == "" {
		return nil, fmt.Errorf("deezer: artist name is required")
	}

	if cached, ok := artistCache.Load(key); ok {
		artist := cached.(*Artist)
		log.Tracef("deezer: artist cache hit for %q", name)
		return artist, nil
	}

	span := sentry.StartSpan(ctx, "deezer.search_artist")
	span.Description = "Search Deezer for artist"
	span.SetTag("query", name)
	span.SetTag("area", "deezer")
	defer span.Finish()

	params := url.Values{"q": {name}, "limit": {"1"}}
	body, err := get(ctx, "/search/artist", params)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: search artist %q: %w", name, err))
		return nil, fmt.Errorf("deezer: search artist failed: %w", err)
	}

	var results listResponse[Artist]
	if err := json.Unmarshal(body, &results); err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: decode artist search for %q: %w", name, err))
		return nil, fmt.Errorf("deezer: failed to decode artist search response: %w", err)
	}

	if len(results.Data) == 0 {
		span.Status = sentry.SpanStatusNotFound
		return nil, fmt.Errorf("deezer: no artist found for %q", name)
	}

	artist := results.Data[0]
	artistCache.Store(key, &artist)

	span.Status = sentry.SpanStatusOK
	return &artist, nil
}

// getArtistByID looks up an artist directly by Deezer artist ID.
func getArtistByID(ctx context.Context, id int) (*Artist, error) {
	span := sentry.StartSpan(ctx, "deezer.get_artist")
	span.Description = "Get artist from Deezer API"
	span.SetTag("artist_id", strconv.Itoa(id))
	defer span.Finish()

	body, err := get(ctx, fmt.Sprintf("/artist/%d", id), nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: get artist failed: %w", err)
	}

	var artist Artist
	if err := json.Unmarshal(body, &artist); err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to decode artist response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	return &artist, nil
}

// SearchTrack looks up a track by artist and title. It first tries Deezer's
// advanced query syntax for a precise match, then falls back to a simple
// free-text query if that comes up empty.
func SearchTrack(ctx context.Context, artist, title string) (*Track, error) {
	if artist == "" || title == "" {
		return nil, fmt.Errorf("deezer: artist and title are required")
	}

	span := sentry.StartSpan(ctx, "deezer.search_track")
	span.Description = "Search Deezer for track"
	span.SetTag("artist", artist)
	span.SetTag("title", title)
	span.SetTag("area", "deezer")
	defer span.Finish()

	advancedQuery := fmt.Sprintf(`artist:"%s" track:"%s"`, artist, title)
	track, err := searchTrackQuery(ctx, advancedQuery)
	if err == nil {
		span.Status = sentry.SpanStatusOK
		return track, nil
	}

	log.Debugf("deezer: advanced track search failed for %q - %q (%v), falling back to simple query", artist, title, err)

	simpleQuery := fmt.Sprintf("%s %s", artist, title)
	track, err = searchTrackQuery(ctx, simpleQuery)
	if err != nil {
		span.Status = sentry.SpanStatusNotFound
		return nil, fmt.Errorf("deezer: no track found for %q by %q: %w", title, artist, err)
	}

	span.Status = sentry.SpanStatusOK
	return track, nil
}

// searchTrackQuery runs a raw track search query and returns the top result.
func searchTrackQuery(ctx context.Context, query string) (*Track, error) {
	params := url.Values{"q": {query}, "limit": {"1"}}
	body, err := get(ctx, "/search/track", params)
	if err != nil {
		return nil, err
	}

	var results listResponse[Track]
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("deezer: failed to decode track search response: %w", err)
	}

	if len(results.Data) == 0 {
		return nil, fmt.Errorf("deezer: no results for query %q", query)
	}

	track := results.Data[0]
	return &track, nil
}

// SearchRadioStation searches Deezer's curated radio stations by name.
func SearchRadioStation(ctx context.Context, query string) ([]RadioStation, error) {
	if query == "" {
		return nil, fmt.Errorf("deezer: query is required")
	}

	span := sentry.StartSpan(ctx, "deezer.search_radio")
	span.Description = "Search Deezer for radio station"
	span.SetTag("query", query)
	span.SetTag("area", "deezer")
	defer span.Finish()

	params := url.Values{"q": {query}}
	body, err := get(ctx, "/search/radio", params)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: search radio %q: %w", query, err))
		return nil, fmt.Errorf("deezer: search radio failed: %w", err)
	}

	var results listResponse[RadioStation]
	if err := json.Unmarshal(body, &results); err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: decode radio search for %q: %w", query, err))
		return nil, fmt.Errorf("deezer: failed to decode radio search response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("results_count", len(results.Data))
	return results.Data, nil
}

// GetRelatedArtists returns artists Deezer considers related to the given
// artist ID, useful for building "more like this" radio experiences.
func GetRelatedArtists(ctx context.Context, artistID int) ([]Artist, error) {
	span := sentry.StartSpan(ctx, "deezer.get_related_artists")
	span.Description = "Get related artists from Deezer API"
	span.SetTag("artist_id", strconv.Itoa(artistID))
	span.SetTag("area", "deezer")
	defer span.Finish()

	body, err := get(ctx, fmt.Sprintf("/artist/%d/related", artistID), nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: get related artists for %d: %w", artistID, err))
		return nil, fmt.Errorf("deezer: get related artists failed: %w", err)
	}

	var results listResponse[Artist]
	if err := json.Unmarshal(body, &results); err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: decode related artists for %d: %w", artistID, err))
		return nil, fmt.Errorf("deezer: failed to decode related artists response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("results_count", len(results.Data))
	return results.Data, nil
}
