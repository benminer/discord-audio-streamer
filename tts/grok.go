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

// grokBuiltinVoices lists the built-in xAI TTS voices. Custom/cloned voices
// (created via the xAI console or Custom Voices API) are passed through as-is
// even if they're not in this list.
var grokBuiltinVoices = []string{
	"Ara", "Eve", "Leo", "Rex", "Sal",
}

type grokProvider struct {
	apiKey string
	speed  float64
	client *http.Client
}

func newGrokProvider() (*grokProvider, error) {
	cfg := config.Config.GrokTTS
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("GROK_TTS_API_KEY is required when TTS_PROVIDER=grok")
	}
	speed := cfg.Speed
	if speed == 0 {
		speed = 1.0
	}
	return &grokProvider{
		apiKey: cfg.APIKey,
		speed:  speed,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// outputFormat configures xAI to return WAV at 48kHz so FFmpeg only needs to
// upmix mono→stereo without resampling. WAV is lossless and already handled
// by audio.ConvertTTSToDiscord.
type outputFormat struct {
	Codec      string `json:"codec"`
	SampleRate int    `json:"sample_rate"`
	BitRate    int    `json:"bit_rate,omitempty"`
}

type ttsRequest struct {
	Text         string       `json:"text"`
	VoiceID      string       `json:"voice_id"`
	Language     string       `json:"language"`
	OutputFormat outputFormat `json:"output_format"`
	Speed        float64      `json:"speed"`
}

func (g *grokProvider) Synthesize(ctx context.Context, script string, voice string) ([]byte, error) {
	if voice == "" {
		voice = g.DefaultVoice()
	}
	// xAI voice IDs are case-insensitive; lowercase for consistency
	voice = strings.ToLower(voice)

	span := sentry.StartSpan(ctx, "grok.tts")
	span.Description = "Generate TTS audio via xAI"
	span.SetTag("voice", voice)
	defer span.Finish()

	// Wrap with xAI speech tags for late-night DJ delivery style
	text := buildGrokTTSText(script)

	payload := ttsRequest{
		Text:     text,
		VoiceID:  voice,
		Language: "en",
		OutputFormat: outputFormat{
			Codec:      "wav",
			SampleRate: 48000,
		},
		Speed: g.speed,
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

		req, err := http.NewRequestWithContext(ctx, "POST", "https://api.x.ai/v1/tts", bytes.NewReader(jsonBody))
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

func (g *grokProvider) Voices() []string     { return grokBuiltinVoices }
func (g *grokProvider) DefaultVoice() string { return "Carina" }
func (g *grokProvider) Name() string         { return "grok" }

// buildGrokTTSText wraps the DJ script with xAI speech tags for delivery.
// Uses <slow> for the laid-back late-night feel and [pause] for natural pacing.
func buildGrokTTSText(script string) string {
	return "<slow>" + script + "</slow>"
}

func isRetryableGrokErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
