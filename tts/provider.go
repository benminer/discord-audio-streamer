package tts

import (
	"context"
	"fmt"
	"strings"

	"beatbot/config"
)

// Provider abstracts TTS audio synthesis so the bot can swap between
// Gemini, Grok, or future backends via a single env var.
type Provider interface {
	// Synthesize generates audio bytes (WAV or raw PCM) from a DJ script.
	// voice can be empty (uses provider default). Returns audio ready for
	// audio.ConvertTTSToDiscord().
	Synthesize(ctx context.Context, script string, voice string) ([]byte, error)

	// Voices returns the available voice names for this provider.
	Voices() []string

	// DefaultVoice returns the provider's default voice name.
	DefaultVoice() string

	// Name returns the provider identifier ("gemini" or "grok").
	Name() string
}

var defaultProvider Provider

// Init initializes the TTS provider based on config.Config.TTSProvider.
// Must be called after config.NewConfig() and gemini.Init().
func Init() error {
	switch config.Config.TTSProvider {
	case "grok":
		p, err := newGrokProvider()
		if err != nil {
			return fmt.Errorf("grok TTS init: %w", err)
		}
		defaultProvider = p
	case "gemini", "":
		defaultProvider = newGeminiProvider()
	default:
		return fmt.Errorf("unknown TTS_PROVIDER: %q (valid: gemini, grok)", config.Config.TTSProvider)
	}
	return nil
}

// Get returns the active TTS provider. Returns nil if Init hasn't been called
// or the provider failed to initialize.
func Get() Provider { return defaultProvider }

// ValidateVoice checks if a voice name is valid for the active provider.
// Returns the canonical name (proper casing) and true, or ("", false).
func ValidateVoice(name string) (string, bool) {
	p := Get()
	if p == nil {
		return "", false
	}
	for _, v := range p.Voices() {
		if strings.EqualFold(v, name) {
			return v, true
		}
	}
	return "", false
}
