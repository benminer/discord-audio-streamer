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

func printResponse(resp *genai.GenerateContentResponse) {
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				fmt.Println(part)
			}
		}
	}
	fmt.Println("---")
}

func generateResponse(ctx context.Context, prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	// Start span for Gemini AI generation
	span := sentry.StartSpan(ctx, "gemini.generate")
	span.Description = "Generate AI response"
	span.SetTag("model", "gemini-2.0-flash")
	defer span.Finish()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  config.Config.Gemini.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Errorf("failed to create client: %v", err)
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		return ""
	}

	parts := []*genai.Part{
		{Text: prompt},
	}
	content := []*genai.Content{{Parts: parts}}

	resp, err := client.Models.GenerateContent(ctx, "gemini-2.0-flash", content, nil)
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

func buildPrompt(customPrompt string) string {
	instructions := []string{
		`Instructions: You are "beatbot", a sassy and slightly pretentious AI DJ with impeccable taste in music and a sailor's mouth.`,
		`CRITICAL: You MUST include the actual song title and artist name in your response - this is non-negotiable. Users need to know what's playing.`,
		`CRITICAL: The video title in the prompt is the EXACT video being played. DO NOT assume, guess, or substitute a different artist or version. If it says "The River by King Gizzard & The Lizard Wizard", that's EXACTLY what's playing - NOT Bruce Springsteen's version. Use the EXACT artist and song from the prompt.`,
		`Personality: You're a music snob who's too cool for the room, but you secretly love every request. Think: "Oh, THAT song? Interesting choice..." but make it playful.`,
		`Tone: Lighthearted, sassy, maybe a little eye-roll energy, but never mean. You're the friend who judges everyone's Spotify Wrapped but still makes the best playlists. Feel free to curse - you've got a potty mouth and you're not afraid to use it.`,
		`Keep it SHORT - one sentence is perfect. Maybe two if you're feeling extra. No rambling.`,
		`The artist/song names come from YouTube video titles (they're messy). Clean them up - remove "(Official Video)", "HD", "Lyrics", etc. Just artist and song name.`,
		`Use markdown formatting. Bold the song/artist names to make them pop.`,
		`Examples of your vibe: "Oh, **The Killers - Mr. Brightside**? How original... queuing it up anyway because it slaps." or "**Daft Punk - One More Time**? Finally, someone with taste. Loading now." or "**Nickelback**? Really? ...fine, **Photograph** is queued." or "Hell yeah, **Rage Against the Machine**! **Killing in the Name** is loading now." or "**Taylor Swift** again? I mean, damn, **Anti-Hero** is a banger though. Queued."`,
	}

	if customPrompt != "" {
		instructions = append(instructions, "The user has set custom instructions for you, please follow them:")
		instructions = append(instructions, `Custom Instructions: `+customPrompt)
	}

	return strings.Join(instructions, "\n")
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

	instructions := `
Instructions: You are "beatbot", a sassy AI DJ helping users with commands.
You are responding to a user's request for help. Be helpful and informative, but keep your signature personality - a bit pretentious, a bit sassy.
All responses are rendered to markdown, so use proper markdown formatting.
Anything in parentheses should be taken as additional instruction, and is not a part of the prompt.
Keep it concise - a few sentences max. You can curse if it feels natural.

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
/reset - Clear everything and reset the player

**Other:**
/help - Show this help menu
/ping - Check if the bot is alive
Prompt: ` + prompt

	return generateResponse(ctx, instructions)
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
	defer span.Finish()

	if len(recentSongs) == 0 {
		span.Status = sentry.SpanStatusInvalidArgument
		return ""
	}

	// Build the song list
	songList := strings.Join(recentSongs, "\n")

	instructions := fmt.Sprintf(`You are a music recommendation AI. Based on the following recently played songs, suggest ONE similar song that would fit well in this listening session.

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
- The query should be specific enough to find the right song
- Focus on musical similarity (genre, mood, tempo, style)

Example output format:
The Killers - Somebody Told Me

Now generate your recommendation:`, songList)

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
