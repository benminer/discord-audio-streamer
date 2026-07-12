package tts

import (
	"context"
	"fmt"
	"strings"

	"beatbot/config"

	log "github.com/sirupsen/logrus"
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

	// AcceptsCustomVoices returns true if the provider supports voice IDs
	// beyond its built-in list (e.g. xAI custom/cloned voices).
	AcceptsCustomVoices() bool
}

var (
	defaultProvider Provider
	// staleVoices holds built-in voices from inactive providers, used by
	// ResolveVoice to detect voices left over after a provider switch.
	staleVoices []string
)

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
		staleVoices = (&geminiProvider{}).Voices()
	case "gemini", "":
		defaultProvider = newGeminiProvider()
		staleVoices = grokBuiltinVoices
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
// For providers that accept custom voices, any non-empty name is valid.
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
	if p.AcceptsCustomVoices() && name != "" {
		return name, true
	}
	return "", false
}

// ResolveVoice validates a stored guild voice against the active provider,
// falling back to the provider's default when the voice is stale (e.g. a
// Gemini voice left in the DB after switching to Grok).
func ResolveVoice(stored string) string {
	p := Get()
	if p == nil {
		return stored
	}
	if stored == "" {
		return p.DefaultVoice()
	}
	// Current provider's built-in list — use canonical casing
	for _, v := range p.Voices() {
		if strings.EqualFold(v, stored) {
			return v
		}
	}
	// For custom-voice providers: distinguish legitimate custom voices
	// from stale voices left after a provider switch
	if p.AcceptsCustomVoices() {
		for _, v := range staleVoices {
			if strings.EqualFold(v, stored) {
				log.WithFields(log.Fields{
					"module":   "tts",
					"stored":   stored,
					"fallback": p.DefaultVoice(),
				}).Warn("Stored voice belongs to another provider, using default")
				return p.DefaultVoice()
			}
		}
		return stored
	}
	// Not valid for current provider
	log.WithFields(log.Fields{
		"module":   "tts",
		"stored":   stored,
		"fallback": p.DefaultVoice(),
	}).Warn("Stored voice not valid for current provider, using default")
	return p.DefaultVoice()
}
