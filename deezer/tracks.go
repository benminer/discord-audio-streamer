package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

// GetTrack fetches full track detail (including BPM/gain/ISRC) for a
// Deezer track ID.
func GetTrack(ctx context.Context, trackID int) (*TrackDetail, error) {
	span := sentry.StartSpan(ctx, "deezer.get_track")
	span.Description = "Get track from Deezer API"
	span.SetTag("track_id", strconv.Itoa(trackID))
	span.SetTag("area", "deezer")
	defer span.Finish()

	body, err := get(ctx, fmt.Sprintf("/track/%d", trackID), nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: get track %d: %w", trackID, err))
		return nil, fmt.Errorf("deezer: get track failed: %w", err)
	}

	var track TrackDetail
	if err := json.Unmarshal(body, &track); err != nil {
		span.Status = sentry.SpanStatusInternalError
		sentry.CaptureException(fmt.Errorf("deezer: decode track %d: %w", trackID, err))
		return nil, fmt.Errorf("deezer: failed to decode track response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	return &track, nil
}

// ResolveTrackMeta searches Deezer for the given artist/title and, on a
// match, fetches the full track detail to build enrichment metadata for a
// queue item. It's best-effort: any failure (no match, API error) results in
// a nil return rather than an error, since Deezer metadata is a nice-to-have
// and shouldn't block playback of a track we already resolved elsewhere.
func ResolveTrackMeta(ctx context.Context, artist, title string) *TrackMeta {
	span := sentry.StartSpan(ctx, "deezer.resolve_track_meta")
	span.Description = "Resolve track metadata from Deezer API"
	span.SetTag("artist", artist)
	span.SetTag("title", title)
	span.SetTag("area", "deezer")
	defer span.Finish()

	track, err := SearchTrack(ctx, artist, title)
	if err != nil {
		log.Debugf("deezer: no track match for %q by %q: %v", title, artist, err)
		span.Status = sentry.SpanStatusNotFound
		return nil
	}

	detail, err := GetTrack(ctx, track.ID)
	if err != nil {
		log.Debugf("deezer: failed to fetch track detail for %d: %v", track.ID, err)
		span.Status = sentry.SpanStatusInternalError
		return nil
	}

	albumYear := ""
	if len(detail.Album.Released) >= 4 {
		albumYear = detail.Album.Released[:4]
	}

	meta := &TrackMeta{
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

	span.Status = sentry.SpanStatusOK
	return meta
}
