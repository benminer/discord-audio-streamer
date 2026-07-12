package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"beatbot/config"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

var grokVoices = []string{
	"Alloy", "Ash", "Ballad", "Coral", "Echo",
	"Fable", "Nova", "Onyx", "Sage", "Shimmer",
}

type grokProvider struct {
	apiKey string
	model  string
	speed  float64
	client *http.Client
}

func newGrokProvider() (*grokProvider, error) {
	cfg := config.Config.GrokTTS
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("GROK_TTS_API_KEY is required when TTS_PROVIDER=grok")
	}
	model := cfg.Model
	if model == "" {
		model = "grok-3-mini-tts"
	}
	speed := cfg.Speed
	if speed == 0 {
		speed = 1.0
	}
	return &grokProvider{
		apiKey: cfg.APIKey,
		model:  model,
		speed:  speed,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (g *grokProvider) Synthesize(ctx context.Context, script string, voice string) ([]byte, error) {
	if voice == "" {
		voice = g.DefaultVoice()
	}

	// Validate voice; fall back to default if the stored voice is from another provider
	valid := false
	for _, v := range grokVoices {
		if strings.EqualFold(v, voice) {
			voice = strings.ToLower(v)
			valid = true
			break
		}
	}
	if !valid {
		log.WithFields(log.Fields{
			"module":          "grok_tts",
			"requested_voice": voice,
			"fallback":        g.DefaultVoice(),
		}).Warn("Unknown voice for Grok provider, using default")
		voice = strings.ToLower(g.DefaultVoice())
	}

	span := sentry.StartSpan(ctx, "grok.tts")
	span.Description = "Generate TTS audio via Grok"
	span.SetTag("model", g.model)
	span.SetTag("voice", voice)
	defer span.Finish()

	payload := map[string]any{
		"model":           g.model,
		"input":           script,
		"voice":           voice,
		"response_format": "wav",
		"speed":           g.speed,
	}
	jsonBody, _ := json.Marshal(payload)

	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt > 1 {
			log.WithFields(log.Fields{
				"module":  "grok_tts",
				"attempt": attempt,
				"error":   lastErr,
			}).Warn("Retrying Grok TTS after transient failure")

			select {
			case <-time.After(1 * time.Second):
			case <-ctx.Done():
				span.Status = sentry.SpanStatusDeadlineExceeded
				return nil, fmt.Errorf("Grok TTS cancelled during retry wait: %w", ctx.Err())
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", "https://api.x.ai/v1/audio/speech", bytes.NewReader(jsonBody))
		if err != nil {
			span.Status = sentry.SpanStatusInternalError
			return nil, fmt.Errorf("failed to create Grok TTS request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < 2 && isRetryableGrokErr(err) {
				continue
			}
			span.Status = sentry.SpanStatusInternalError
			sentry.CaptureException(err)
			return nil, fmt.Errorf("Grok TTS request failed: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			span.Status = sentry.SpanStatusInternalError
			return nil, fmt.Errorf("failed to read Grok TTS response: %w", readErr)
		}

		if resp.StatusCode >= 500 && attempt < 2 {
			lastErr = fmt.Errorf("Grok TTS returned %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("Grok TTS error %d: %s", resp.StatusCode, string(respBody))
			span.Status = sentry.SpanStatusInternalError
			sentry.CaptureException(err)
			return nil, err
		}

		log.WithFields(log.Fields{
			"module":    "grok_tts",
			"data_size": len(respBody),
			"voice":     voice,
			"attempt":   attempt,
		}).Debug("TTS audio received from Grok")

		span.Status = sentry.SpanStatusOK
		return respBody, nil
	}

	span.Status = sentry.SpanStatusInternalError
	return nil, fmt.Errorf("Grok TTS exhausted retries: %w", lastErr)
}

func (g *grokProvider) Voices() []string     { return grokVoices }
func (g *grokProvider) DefaultVoice() string { return "Alloy" }
func (g *grokProvider) Name() string         { return "grok" }

func isRetryableGrokErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
