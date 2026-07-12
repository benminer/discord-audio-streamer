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

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

const (
	baseURL = "https://api.deezer.com"

	// Deezer's public API is rate limited to ~50 requests per 5 seconds per IP.
	rateLimitTokens   = 50
	rateLimitInterval = 5 * time.Second
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// bucket is a simple token bucket used to stay under Deezer's rate limit.
// It refills to full capacity on a fixed interval rather than a continuous
// trickle, which is close enough to Deezer's documented window and much
// simpler to reason about.
type bucket struct {
	mu       sync.Mutex
	tokens   int
	max      int
	interval time.Duration
	lastFill time.Time
}

var limiter = &bucket{
	tokens:   rateLimitTokens,
	max:      rateLimitTokens,
	interval: rateLimitInterval,
	lastFill: time.Now(),
}

// waitForRateLimit blocks until a token is available, refilling the bucket
// whenever a full interval has elapsed since the last refill.
func waitForRateLimit(ctx context.Context) error {
	for {
		limiter.mu.Lock()
		if time.Since(limiter.lastFill) >= limiter.interval {
			limiter.tokens = limiter.max
			limiter.lastFill = time.Now()
		}

		if limiter.tokens > 0 {
			limiter.tokens--
			limiter.mu.Unlock()
			return nil
		}

		wait := limiter.interval - time.Since(limiter.lastFill)
		limiter.mu.Unlock()

		if wait <= 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// apiError mirrors the shape Deezer uses to report errors. Deezer always
// responds with HTTP 200, so callers must check for this field explicitly.
type apiError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// get performs a rate-limited GET request against the Deezer API and returns
// the raw response body, after checking for Deezer's embedded error format.
func get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if err := waitForRateLimit(ctx); err != nil {
		return nil, fmt.Errorf("deezer: rate limit wait canceled: %w", err)
	}

	reqURL := baseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	span := sentry.StartSpan(ctx, "http.client", sentry.WithDescription("GET "+path))
	span.SetTag("deezer.path", path)
	defer span.Finish()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to build request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{
			"path":   path,
			"status": resp.StatusCode,
		}).Warn("deezer: non-200 response")
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: unexpected status %d", resp.StatusCode)
	}

	// Deezer reports errors as HTTP 200 with an "error" object in the body,
	// so a status-code check alone isn't sufficient.
	var apiErr apiError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		log.WithFields(log.Fields{
			"path":    path,
			"code":    apiErr.Error.Code,
			"type":    apiErr.Error.Type,
			"message": apiErr.Error.Message,
		}).Warn("deezer: API-level error")
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("deezer: API error (%s): %s", apiErr.Error.Type, apiErr.Error.Message)
	}

	span.Status = sentry.SpanStatusOK
	return body, nil
}
