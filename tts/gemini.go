package tts

import (
	"context"

	"beatbot/gemini"
)

type geminiProvider struct{}

func newGeminiProvider() *geminiProvider { return &geminiProvider{} }

func (g *geminiProvider) Synthesize(ctx context.Context, script string, voice string) ([]byte, error) {
	prompt := gemini.BuildTTSPrompt(script)
	return gemini.GenerateTTSAudio(ctx, prompt, voice, "")
}

func (g *geminiProvider) Voices() []string     { return gemini.AvailableVoices }
func (g *geminiProvider) DefaultVoice() string { return "Aoede" }
func (g *geminiProvider) Name() string         { return "gemini" }
