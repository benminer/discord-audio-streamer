package gemini

import (
	"context"
	"fmt"
	"strings"

	"beatbot/config"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/genai"
)

// defaultClient is the package-level Gemini client, initialized once by Init().
// Creating a new HTTP client + TLS session on every call was wasteful.
var defaultClient *genai.Client

// AvailableVoices lists all Gemini TTS voice presets available for DJ announcements.
var AvailableVoices = []string{
	"Zephyr", "Puck", "Charon", "Kore", "Fenrir", "Leda", "Orus", "Aoede",
	"Callirrhoe", "Autonoe", "Enceladus", "Iapetus", "Umbriel", "Algieba",
	"Despina", "Erinome", "Algenib", "Rasalgethi", "Laomedeia", "Achernar",
	"Alnilam", "Schedar", "Gacrux", "Pulcherrima", "Achird", "Zubenelgenubi",
	"Vindemiatrix", "Sadachbia", "Sadaltager", "Sulafat",
}

// Init initializes the shared Gemini client. Must be called once at startup
// (after config is loaded) before any Gemini functions are used. Safe to call
// when Gemini is disabled — it becomes a no-op.
func Init() error {
	if !config.Config.Gemini.Enabled {
		return nil
	}
	// Use a background context for client creation — the client itself is
	// long-lived and should not be tied to any single request context.
	c, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  config.Config.Gemini.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return fmt.Errorf("failed to create Gemini client: %w", err)
	}
	defaultClient = c
	log.Info("Gemini client initialized")
	return nil
}

func generateResponse(ctx context.Context, prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}
	if defaultClient == nil {
		log.Warn("generateResponse called before Gemini client was initialized")
		return ""
	}

	// Start span for Gemini AI generation
	span := sentry.StartSpan(ctx, "gemini.generate")
	span.Description = "Generate AI response"
	span.SetTag("model", config.Config.Gemini.Model)
	defer span.Finish()

	parts := []*genai.Part{
		{Text: prompt},
	}
	content := []*genai.Content{{Parts: parts}}

	resp, err := defaultClient.Models.GenerateContent(ctx, config.Config.Gemini.Model, content, nil)
	if err != nil {
		log.Errorf("failed to generate content: %v", err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return ""
	}

	var sb strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					sb.WriteString(part.Text)
				}
			}
		}
	}
	response := sb.String()
	span.Status = sentry.SpanStatusOK
	return response
}

// buildPrompt prepends the shared beatbot personality to task-specific instructions.
func buildPrompt(instructions string) string {
	return PersonalityPrompt + "\n\n" + instructions
}

// GenerateRaw prepends the shared personality and generates a response for the given prompt.
// Use this when you need Gemini generation from outside the gemini package.
func GenerateRaw(ctx context.Context, prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}
	return generateResponse(ctx, buildPrompt(prompt))
}

func GenerateResponse(ctx context.Context, prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	instructions := buildPrompt(prompt)

	return generateResponse(ctx, instructions)
}

func GenerateHelpfulResponse(ctx context.Context, prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	instructions := buildPrompt(fmt.Sprintf(`You are responding to a user's help request. Be helpful and informative but keep your signature personality.
All responses are rendered in Discord markdown, so use proper formatting.
Keep it concise—a few sentences max.

Here are the available commands:

**Music Control:**
/play (or /queue) - Queue a song. Takes a search query, YouTube URL/playlist, or Spotify URL. Note: YouTube links with ?list= will queue the whole playlist
/skip - Skip the current song and play the next in queue
/pause (or /stop) - Pause the current song
/resume - Resume playback
/volume - Set playback volume (0-100)

**Queue Management:**
/view - View the current queue
/remove - Remove a song from the queue by index number
/clear - Clear the entire queue

**Radio / AI Mode:**
/radio - Toggle AI radio mode (auto-queues songs based on listening history)

**Info:**
/now-playing - Show the current song
/history - Show recently played songs
/leaderboard - Show most played songs

User's request: %s`, prompt))

	return generateResponse(ctx, instructions)
}

// GenerateAgeRestrictedResponse returns a snarky DJ response for when a video
// is blocked due to age restrictions. directRequest indicates whether the user
// specifically asked for that video by URL (vs. a search result that happened to
// be gated). Falls back to a hardcoded string if Gemini is disabled.
func GenerateAgeRestrictedResponse(ctx context.Context, directRequest bool) string {
	var fallback string
	if directRequest {
		fallback = "That video is age-restricted and can't be played — YouTube won't let me near it. Try a different link?"
	} else {
		fallback = `YouTube blocked this from loading because it's "restricted" — sorry! Try something else.`
	}

	if !config.Config.Gemini.Enabled {
		return fallback
	}

	var instructions string
	if directRequest {
		instructions = `A user requested a specific YouTube video by URL and YouTube won't play it because it's age-restricted. Tell them in one sentence.`
	} else {
		instructions = `A user requested a song and YouTube blocked it because it's "restricted". Tell them in one sentence and suggest they try something else.`
	}

	response := generateResponse(ctx, buildPrompt(instructions))
	if response == "" {
		return fallback
	}
	return strings.TrimSpace(response)
}

// GenerateSongRecommendation analyzes recent listening history and generates a search query
// for finding a similar song. Returns an empty string if Gemini is disabled or on error.
func GenerateSongRecommendation(ctx context.Context, recentSongs []string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	// Start span for Gemini recommendation generation
	span := sentry.StartSpan(ctx, "gemini.song_recommendation")
	span.Description = "Generate song recommendation query"
	span.SetTag("num_songs", fmt.Sprintf("%d", len(recentSongs)))
	span.SetTag("model", config.Config.Gemini.Model)
	defer span.Finish()

	if len(recentSongs) == 0 {
		span.Status = sentry.SpanStatusInvalidArgument
		return ""
	}

	// Build the song list
	songList := strings.Join(recentSongs, "\n")

	instructions := buildPrompt(fmt.Sprintf(`You are a music recommendation AI. Based on the following recently played songs, suggest ONE similar song that would fit well in this listening session.

Recent songs played:
%s

Your task:
1. Analyze the genre, mood, era, and style of these songs
2. Identify common patterns or themes
3. Suggest ONE song that is musically similar but NOT in the list above
4. Return ONLY a search query string that can be used to find this song on YouTube

Important:
- Return ONLY the search query (e.g., "Artist Name - Song Title")
- Do NOT include explanations, reasoning, or extra text
- Do NOT suggest a song that's already in the list
- Avoid recommending the same artist or song title as any entry in the recent history, even alternate versions or uploads.
- The query should be specific enough to find the right song
- Focus on musical similarity (genre, mood, tempo, style)

Example output format:
The Killers - Somebody Told Me

Now generate your recommendation:`, songList))

	response := generateResponse(ctx, instructions)
	if response == "" {
		span.Status = sentry.SpanStatusInternalError
		return ""
	}

	// Clean up the response (remove any extra formatting or explanations)
	query := strings.TrimSpace(response)
	// Remove quotes if present
	query = strings.Trim(query, "\"'`")

	span.Status = sentry.SpanStatusOK
	span.SetData("query", query)

	log.WithFields(log.Fields{
		"module": "gemini",
		"query":  query,
	}).Debug("Generated song recommendation query")

	return query
}

// GenerateNowPlayingCommentary creates conversational DJ commentary for the current song
// based on the song history and whether it was auto-queued by radio mode.
func GenerateNowPlayingCommentary(ctx context.Context, currentSong string, recentHistory []string, isRadioPick bool) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	// Start span for Gemini commentary generation
	span := sentry.StartSpan(ctx, "gemini.now_playing_commentary")
	span.Description = "Generate DJ commentary for now playing song"
	span.SetTag("model", config.Config.Gemini.Model)
	defer span.Finish()

	// Build the recent history string
	var historyStr string
	if len(recentHistory) > 0 {
		historyStr = "Recent songs played (most recent first):\n"
		for i, song := range recentHistory {
			historyStr += fmt.Sprintf("%d. %s\n", i+1, song)
		}
	} else {
		historyStr = "This is the first song in the session."
	}

	// Build instructions with task-specific logic
	radioStr := func() string {
		if isRadioPick {
			return "This song was auto-queued by radio mode based on the listening pattern above."
		}
		return "This song was manually requested by a user."
	}()

	instructions := buildPrompt(fmt.Sprintf(`Current song playing: **%s**

%s

%s

Your task: Write ONE sentence of commentary about this song. Two sentences only if the second adds something the first genuinely can't.

Good commentary looks like:
- One specific detail — a production choice, a story behind the track, a connection to the listening pattern
- A dry transition when the mood shifts ("after all that, here's something to breathe to")
- Context that makes someone nod, not cheer

Bad commentary (avoid these):
- Exclamation-heavy hype ("Get ready to move!", "This is a BANGER!")
- Reference stacking — pick ONE angle, not four
- Explaining the vibe out loud instead of just setting it
- Fake enthusiasm ("you can practically smell the...")
- Announcing it's a radio pick — just play it

Examples of good commentary:
- "**House of Jealous Lovers** and that cowbell. DFA knew exactly what they were doing."
- "Kevin Parker recorded this in his childhood bedroom. Somehow that's obvious and impressive at the same time."
- "After that stretch of bangers, **Nick Drake** is the right call."
- "This is the song that made people realize **LCD Soundsystem** was serious."
- "**Stevie Wonder** played almost every instrument on this himself. Still sounds effortless."

Rules:
- ONE sentence. Two max, only if earned.
- Bold **artist and song names**
- Never say "As an AI..." or apologize
- If you don't know something specific, be vague rather than invent facts

Now write your commentary:`, currentSong, historyStr, radioStr))

	response := generateResponse(ctx, instructions)
	if response == "" {
		span.Status = sentry.SpanStatusInternalError
		return ""
	}

	// Clean up the response
	commentary := strings.TrimSpace(response)
	// Remove quotes if present
	commentary = strings.Trim(commentary, "\"' `")

	span.Status = sentry.SpanStatusOK
	span.SetData("commentary", commentary)

	log.WithFields(log.Fields{
		"module":     "gemini",
		"song":       currentSong,
		"is_radio":   isRadioPick,
		"commentary": commentary,
	}).Debug("Generated now playing commentary")

	return commentary
}

// GenerateDJTransitionScript generates a short spoken-word transition line that
// announces the song that just played and the one coming up next, in a natural
// radio DJ cadence. The result is fed into TTS and must avoid markdown.
func GenerateDJTransitionScript(ctx context.Context, currentSong, nextSong string, recentHistory []string, isRadioPick bool) string {
	if !config.Config.Gemini.Enabled || defaultClient == nil {
		return ""
	}

	span := sentry.StartSpan(ctx, "gemini.dj_transition_script")
	span.Description = "Generate DJ transition script for TTS"
	span.SetTag("model", config.Config.Gemini.Model)
	defer span.Finish()

	var historyStr string
	if len(recentHistory) > 0 {
		historyStr = "Recent songs played (most recent first):\n"
		for i, song := range recentHistory {
			historyStr += fmt.Sprintf("%d. %s\n", i+1, song)
		}
	} else {
		historyStr = "This is the first song in the session."
	}

	radioStr := ""
	if isRadioPick {
		radioStr = "This next song was auto-queued by radio mode based on the listening pattern."
	}

	instructions := buildPrompt(fmt.Sprintf(`You are a radio DJ over the fade-out of the current song. Write ONE short sentence that announces the song that just played and what's coming up next.

Current song (just finished): %s
Next up: %s

%s

%s

Rules:
- ONE sentence ONLY, divided into two halves
- First half: announce what just played ("That was X...")
- Second half: announce what's coming up next ("...up next is Y")
- No markdown. No bold. No asterisks
- Natural spoken cadence for text-to-speech
- Keep it SHORT - 15-20 words max, 5-10 seconds spoken
- Artist names and song titles only — skip the album mention unless it's obvious
- Sound like a real radio DJ, not a robot

Example good: "That was the Weeknd rolling out, up next we got some Daft Punk coming your way."
Example bad: "**The Weeknd's** track was great! Now let me introduce you to **Daft Punk** with their amazing hit!"

Now write your transition:`, currentSong, nextSong, historyStr, radioStr))

	response := generateResponse(ctx, instructions)
	if response == "" {
		span.Status = sentry.SpanStatusInternalError
		return ""
	}

	span.Status = sentry.SpanStatusOK
	return strings.TrimSpace(response)
}

// GenerateTTSAudio synthesizes speech from a script using Gemini's TTS model.
// Returns raw PCM audio data (16-bit 24kHz mono). The caller is responsible for
// encoding or resampling before sending to Discord.
func GenerateTTSAudio(ctx context.Context, script, voice, model string) ([]byte, error) {
	if defaultClient == nil {
		return nil, fmt.Errorf("gemini client not initialized")
	}

	if model == "" {
		model = config.Config.Gemini.TTSModel
	}
	if voice == "" {
		voice = "Aoede"
	}

	span := sentry.StartSpan(ctx, "gemini.tts")
	span.Description = "Generate TTS audio"
	span.SetTag("model", model)
	span.SetTag("voice", voice)
	defer span.Finish()

	content := []*genai.Content{{Parts: []*genai.Part{{Text: script}}}}
	resp, err := defaultClient.Models.GenerateContent(ctx, model, content, &genai.GenerateContentConfig{
		ResponseModalities: []string{"AUDIO"},
		SpeechConfig: &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voice,
				},
			},
		},
	})
	if err != nil {
		log.WithFields(log.Fields{
			"module": "gemini",
			"model":  model,
			"voice":  voice,
		}).Errorf("TTS generation failed: %v", err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, fmt.Errorf("TTS generation failed: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil ||
		len(resp.Candidates[0].Content.Parts) == 0 || resp.Candidates[0].Content.Parts[0].InlineData == nil {
		err := fmt.Errorf("TTS response contained no audio data")
		log.WithField("module", "gemini").Error(err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}

	inlineData := resp.Candidates[0].Content.Parts[0].InlineData
	log.WithFields(log.Fields{
		"module":    "gemini",
		"mime_type": inlineData.MIMEType,
		"data_size": len(inlineData.Data),
	}).Debug("TTS audio received from Gemini")

	span.Status = sentry.SpanStatusOK
	return inlineData.Data, nil
}
