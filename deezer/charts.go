package deezer

import (
	"context"
	"encoding/json"
	"fmt"

	sentry "github.com/getsentry/sentry-go"
)

// GetCharts returns Deezer's current global top tracks, artists, and albums.
func GetCharts(ctx context.Context) (*ChartResponse, error) {
	span := sentry.StartSpan(ctx, "deezer.get_charts")
	span.Description = "Get charts from Deezer API"
	defer span.Finish()

	body, err := get(ctx, "/chart", nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: get charts failed: %w", err)
	}

	var charts ChartResponse
	if err := json.Unmarshal(body, &charts); err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to decode charts response: %w", err)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("tracks_count", len(charts.Tracks.Data))
	return &charts, nil
}
