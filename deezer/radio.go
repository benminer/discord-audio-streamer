package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	sentry "github.com/getsentry/sentry-go"
)

// GetArtistRadio returns Deezer's algorithmic radio mix for the given
// artist ID - a curated set of tracks similar to that artist's catalog.
func GetArtistRadio(ctx context.Context, artistID int) ([]Track, error) {
	span := sentry.StartSpan(ctx, "deezer.get_artist_radio")
	span.Description = "Get artist radio from Deezer API"
	span.SetTag("artist_id", strconv.Itoa(artistID))
	defer span.Finish()

	body, err := get(ctx, fmt.Sprintf("/artist/%d/radio", artistID), nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: get artist radio failed: %w", err)
	}

	var results listResponse[Track]
	if err := json.Unmarshal(body, &results); err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to decode artist radio response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("tracks_count", len(results.Data))
	return results.Data, nil
}

// GetGenreStations returns all genres Deezer tracks, each with its
// associated curated radio stations.
func GetGenreStations(ctx context.Context) ([]Genre, error) {
	span := sentry.StartSpan(ctx, "deezer.get_genre_stations")
	span.Description = "Get genre radio stations from Deezer API"
	defer span.Finish()

	body, err := get(ctx, "/radio/genres", nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: get genre stations failed: %w", err)
	}

	var results listResponse[Genre]
	if err := json.Unmarshal(body, &results); err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to decode genre stations response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("genres_count", len(results.Data))
	return results.Data, nil
}

// GetStationTracks returns the current tracklist for a curated radio
// station ID (e.g. one returned from GetGenreStations or SearchRadioStation).
func GetStationTracks(ctx context.Context, stationID int) ([]Track, error) {
	span := sentry.StartSpan(ctx, "deezer.get_station_tracks")
	span.Description = "Get radio station tracks from Deezer API"
	span.SetTag("station_id", strconv.Itoa(stationID))
	defer span.Finish()

	body, err := get(ctx, fmt.Sprintf("/radio/%d/tracks", stationID), nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: get station tracks failed: %w", err)
	}

	var results listResponse[Track]
	if err := json.Unmarshal(body, &results); err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to decode station tracks response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("tracks_count", len(results.Data))
	return results.Data, nil
}
