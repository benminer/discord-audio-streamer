package gemini

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"beatbot/config"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/genai"
)

const (
	// TTSTimeout is the recommended context deadline for TTS generation calls.
	// Budget: one full attempt (~20s) + retry delay (1s) + second attempt (~20s).
	TTSTimeout = 45 * time.Second

	ttsMaxAttempts = 2
	ttsRetryDelay  = 1 * time.Second
)

var leadingNumberRe = regexp.MustCompile(`^\d+\.\s*`)

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

// AnnouncementType identifies which kind of DJ announcement is being generated,
// so GenerateDJScript can tailor instructions and context to the moment.
type AnnouncementType int

const (
	AnnouncementTransition AnnouncementType = iota
	AnnouncementIntro
	AnnouncementQueueEmpty
	AnnouncementRadioStart
)

// DJScriptContext carries the situational details GenerateDJScript needs to
// write a natural-sounding announcement for the given AnnouncementType.
type DJScriptContext struct {
	Type                AnnouncementType
	CurrentSong         string   // song that just played (transition)
	CurrentChannelName  string   // YouTube channel/uploader of current song
	CurrentArtistName   string   // Deezer-resolved artist (preferred over channel when set)
	NextSong            string   // song coming up (transition, intro)
	NextChannelName     string   // YouTube channel/uploader of next song
	NextArtistName      string   // Deezer-resolved artist for next song (rarely available)
	RecentHistory       []string // recent song titles
	IsRadioPick         bool     // whether next song was auto-queued by radio
	CurrentQueuedBy     string   // who queued the current song (empty = radio/unknown)
	NextQueuedBy        string   // who queued the next song (empty = radio/unknown)
	VoiceChannelMembers []string // display names of listeners in the voice channel (excludes the bot)
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
		HTTPClient: &http.Client{
			Timeout: 35 * time.Second,
		},
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

// GenerateThemedRecommendation is like GenerateSongRecommendation but driven primarily
// by a user-provided theme (e.g. "chill indie", "90s hip hop", "Radiohead deep cuts"),
// with recent song history as secondary context. Returns an empty string if Gemini is
// disabled or on error.
func GenerateThemedRecommendation(ctx context.Context, theme string, recentSongs []string) string {
	if !config.Config.Gemini.Enabled || defaultClient == nil {
		return ""
	}

	// Start span for Gemini themed recommendation generation
	span := sentry.StartSpan(ctx, "gemini.themed_recommendation")
	span.Description = "Generate themed song recommendation query"
	span.SetTag("model", config.Config.Gemini.Model)
	defer span.Finish()

	songList := "No recent songs played yet."
	if len(recentSongs) > 0 {
		songList = strings.Join(recentSongs, "\n")
	}

	instructions := buildPrompt(fmt.Sprintf(`The listener wants: %s

Their recent songs for context:
%s

Suggest ONE song that matches the requested vibe. Return ONLY a YouTube search query like "Artist - Song Title". Don't repeat anything from the list.`, theme, songList))

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
		"theme":  theme,
		"query":  query,
	}).Debug("Generated themed recommendation query")

	return query
}

// GenerateRequestQueries generates 3-5 YouTube search queries matching a user's
// free-text song/artist/vibe request, used by the /request command. Returns nil
// if Gemini is disabled or on error.
func GenerateRequestQueries(ctx context.Context, suggestion string, recentSongs []string) []string {
	if !config.Config.Gemini.Enabled || defaultClient == nil {
		return nil
	}

	// Start span for Gemini request query generation
	span := sentry.StartSpan(ctx, "gemini.request_queries")
	span.Description = "Generate request search queries"
	span.SetTag("model", config.Config.Gemini.Model)
	defer span.Finish()

	songList := "No recent songs played yet."
	if len(recentSongs) > 0 {
		songList = strings.Join(recentSongs, "\n")
	}

	instructions := buildPrompt(fmt.Sprintf(`The listener is requesting: %s

Their recent songs for context:
%s

Suggest 3-5 songs that match the request. Return each as a YouTube search query (e.g. "Artist - Song Title"), one per line. Don't repeat anything from the list. Don't number the lines.`, suggestion, songList))

	response := generateResponse(ctx, instructions)
	if response == "" {
		span.Status = sentry.SpanStatusInternalError
		return nil
	}

	lines := strings.Split(response, "\n")
	queries := make([]string, 0, len(lines))
	for _, line := range lines {
		query := strings.TrimSpace(line)
		query = strings.Trim(query, "\"'`")
		query = leadingNumberRe.ReplaceAllString(query, "")
		if query == "" {
			continue
		}
		queries = append(queries, query)
	}

	span.Status = sentry.SpanStatusOK
	span.SetData("num_queries", fmt.Sprintf("%d", len(queries)))

	log.WithFields(log.Fields{
		"module":     "gemini",
		"suggestion": suggestion,
		"queries":    queries,
	}).Debug("Generated request search queries")

	return queries
}

// SongContext carries optional Deezer-derived metadata (and radio-mode state)
// used to give GenerateNowPlayingCommentary more to work with than the bare
// title/history. All fields are best-effort — nil/zero values are simply
// omitted from the prompt.
type SongContext struct {
	ArtistName     string
	BPM            float64
	AlbumName      string
	AlbumYear      string
	Popularity     int
	RelatedArtists []string
	PreviousBPM    float64
	RadioMode      string
}

// GenerateNowPlayingCommentary creates conversational DJ commentary for the current song
// based on the song history and whether it was auto-queued by radio mode.
// songCtx is optional (may be nil) and adds Deezer-derived metadata (genre, BPM,
// album, artist) to the prompt when available.
func GenerateNowPlayingCommentary(ctx context.Context, currentSong, channelName string, recentHistory []string, isRadioPick bool, songCtx *SongContext) string {
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

	// Build artist identification line: prefer Deezer-resolved artist, fall back to YouTube channel
	artistLine := fmt.Sprintf("Channel: %s", channelLabel(channelName))
	if songCtx != nil && songCtx.ArtistName != "" {
		artistLine = fmt.Sprintf("Artist: %s (verified)", songCtx.ArtistName)
	}

	instructions := buildPrompt(fmt.Sprintf(`Current song playing: **%s**
%s

%s

%s

Your task: Write ONE sentence of commentary about this song. Two sentences only if the second adds something the first genuinely can't.

IMPORTANT: Your commentary MUST reference the song using ONLY the exact title and artist/channel provided above. Do not guess the artist or substitute with your own knowledge.

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

Examples of good commentary (these are style examples; use only the exact title and artist above for facts):
- "**House of Jealous Lovers** — that cowbell is doing a lot of work."
- "This one sounds like it was recorded in a bedroom, and somehow that makes it better."
- "After that stretch of bangers, this is the right call."
- "This is the song that makes the band sound inevitable."
- "The production on this is doing a lot with very little."

Rules:
- ONE sentence. Two max, only if earned.
- Bold **the song title** when you reference it
- Never say "As an AI..." or apologize
- If you don't know something specific, be vague rather than invent facts

Now write your commentary:`, currentSong, artistLine, historyStr, radioStr))

	if songCtx != nil {
		instructions += "\n\nAdditional context about the current song:"
		if songCtx.BPM > 0 {
			instructions += fmt.Sprintf("\n- BPM: %.0f", songCtx.BPM)
		}
		if songCtx.AlbumName != "" {
			year := ""
			if songCtx.AlbumYear != "" {
				year = " (" + songCtx.AlbumYear + ")"
			}
			instructions += fmt.Sprintf("\n- Album: %s%s", songCtx.AlbumName, year)
		}
		if songCtx.PreviousBPM > 0 && songCtx.BPM > 0 {
			diff := math.Abs(songCtx.BPM - songCtx.PreviousBPM)
			if diff <= 5 {
				instructions += "\n- Note: smooth tempo match with previous song"
			} else if diff > 30 {
				instructions += "\n- Note: significant tempo shift from previous song"
			}
		}
		if songCtx.RadioMode != "" {
			instructions += fmt.Sprintf("\n- Radio mode: %s", songCtx.RadioMode)
		}
		instructions += "\n\nUse this context naturally — mention genre, tempo, or album era IF it adds to the commentary. Don't force it."
	}

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

// announcementTypeTag returns a short string for Sentry tagging.
func announcementTypeTag(t AnnouncementType) string {
	switch t {
	case AnnouncementTransition:
		return "transition"
	case AnnouncementIntro:
		return "intro"
	case AnnouncementQueueEmpty:
		return "queue_empty"
	case AnnouncementRadioStart:
		return "radio_start"
	default:
		return "unknown"
	}
}

// recentHistoryBlock formats recent song history as a numbered list, or a
// fallback line when there's no history yet. Shared across announcement types.
func recentHistoryBlock(recentHistory []string) string {
	if len(recentHistory) == 0 {
		return "This is the first song in the session."
	}
	var sb strings.Builder
	sb.WriteString("Recent songs played (most recent first):\n")
	for i, song := range recentHistory {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, song))
	}
	return sb.String()
}

// channelLabel returns the channel name for prompt display, with a fallback
// when YouTube does not expose one. Prevents awkward empty placeholders like
// "(channel: )" from reaching the model.
func channelLabel(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}

var youtubeSuffixes = []string{
	"(Official Music Video)", "[Official Music Video]",
	"(Official Video)", "[Official Video]",
	"(Official Audio)", "[Official Audio]",
	"(Official Lyric Video)", "[Official Lyric Video]",
	"(Official Visualizer)", "[Official Visualizer]",
	"(Lyric Video)", "[Lyric Video]",
	"(Lyrics)", "[Lyrics]",
	"(Audio)", "[Audio]",
	"(Video)", "[Video]",
	"(Visualizer)", "[Visualizer]",
	"(Official)", "[Official]",
	"(MV)", "[MV]",
	"(HQ)", "[HQ]",
	"(HD)", "[HD]",
	"(4K)", "[4K]",
}

func cleanTitle(title string) string {
	cleaned := strings.TrimSpace(title)
	for {
		prev := cleaned
		for _, suffix := range youtubeSuffixes {
			cleaned = strings.TrimSpace(strings.TrimSuffix(cleaned, suffix))
		}
		if cleaned == prev {
			break
		}
	}
	return cleaned
}

// GenerateDJScript generates a short spoken-word DJ script — with inline audio
// tags for TTS delivery — for the moment described by sc. The result must
// avoid markdown; it's fed directly into GenerateTTSAudio via BuildTTSPrompt.
func GenerateDJScript(ctx context.Context, sc DJScriptContext) string {
	if !config.Config.Gemini.Enabled || defaultClient == nil {
		return ""
	}

	span := sentry.StartSpan(ctx, "gemini.dj_script")
	span.Description = "Generate DJ script for TTS"
	span.SetTag("model", config.Config.Gemini.Model)
	span.SetTag("announcement_type", announcementTypeTag(sc.Type))
	defer span.Finish()

	// artistOrChannel picks the best available identification for a song.
	// Deezer-resolved artist is preferred (more accurate), YouTube channel is fallback.
	artistOrChannel := func(artistName, channelName string) string {
		if artistName != "" {
			return fmt.Sprintf("artist: %s", artistName)
		}
		return fmt.Sprintf("channel: %s", channelLabel(channelName))
	}

	currentLabel := artistOrChannel(sc.CurrentArtistName, sc.CurrentChannelName)
	nextLabel := artistOrChannel(sc.NextArtistName, sc.NextChannelName)

	currentSong := cleanTitle(sc.CurrentSong)
	nextSong := cleanTitle(sc.NextSong)

	var roastStr string
	if len(sc.VoiceChannelMembers) > 0 {
		roastStr = fmt.Sprintf("\nPeople currently listening in voice: %s\n"+
			"Pick ONE person at random and roast them. Make it specific and funny. "+
			"Go after their music taste, the fact they queued something, or just "+
			"roast them for being there. Keep it sharp, not cruel.\n",
			strings.Join(sc.VoiceChannelMembers, ", "))
	}

	var taskPrompt string
	switch sc.Type {
	case AnnouncementTransition:
		radioStr := ""
		if sc.IsRadioPick {
			radioStr = "This next song was auto-queued by radio mode based on the listening pattern."
		}

		requesterStr := ""
		if sc.CurrentQueuedBy != "" && sc.CurrentQueuedBy == sc.NextQueuedBy {
			requesterStr = fmt.Sprintf("Both songs were queued by %s.\n", sc.CurrentQueuedBy)
		} else {
			if sc.CurrentQueuedBy != "" {
				requesterStr += fmt.Sprintf("The current song was queued by %s.\n", sc.CurrentQueuedBy)
			}
			if sc.NextQueuedBy != "" {
				requesterStr += fmt.Sprintf("The next song was queued by %s.\n", sc.NextQueuedBy)
			}
		}

		taskPrompt = fmt.Sprintf(`Current song (just finished): %s (%s)
Next up: %s (%s)

%s

%s
%s
%s
IMPORTANT: You MUST announce the songs using ONLY the exact titles and artist/channel provided above. Do not guess, substitute, or use your own knowledge about what artist or song this might be.

Your task: Announce what just played and what's coming up next. Write it as two halves: first announce what just played, then what's next. Roast someone in between if there are listeners listed.
- You MUST say BOTH the song/artist that just played AND the song/artist coming up next
- Never omit either name — they are the entire point of the announcement
- If you know who queued a song, mention them by name. Skip attribution for songs with no requester.
- If both songs were queued by the same person, mention them once naturally (e.g. "both queued by Ben").
- If there are listeners, pick one and roast them as part of your transition

Now write your transition:`, currentSong, currentLabel, nextSong, nextLabel, recentHistoryBlock(sc.RecentHistory), radioStr, requesterStr, roastStr)
	case AnnouncementIntro:
		taskPrompt = fmt.Sprintf(`First song of the session: %s (%s)
%s
IMPORTANT: You MUST say the exact song title and artist/channel provided above. Do not guess or substitute.

Your task: Introduce the first song of the session. Kick things off with energy.
- You MUST say the song/artist
- If there are listeners, pick one and give them shit as you start up

Now write your intro:`, nextSong, nextLabel, roastStr)
	case AnnouncementQueueEmpty:
		taskPrompt = fmt.Sprintf(`%s
Your task: The queue just ran out. Roast whoever let it die. Hype up /radio mode as the way to keep the music going — it auto-queues songs based on what's been playing. Also mention /play or /queue for adding specific songs, but lead with radio as the main suggestion.
- If there are listeners, roast one of them for not queuing anything

Now write your announcement:`, roastStr)
	case AnnouncementRadioStart:
		taskPrompt = fmt.Sprintf(`%s
Your task: Radio mode was just turned on. You're taking over as DJ.
Announce that radio mode is on and introduce your first pick: %s (%s).
Keep it brief and natural.
- If there are listeners, roast one as you take over the aux

IMPORTANT: You MUST say the exact song title and artist/channel provided above. Do not guess or substitute.

Now write your announcement:`, roastStr, nextSong, nextLabel)
	default:
		span.Status = sentry.SpanStatusInvalidArgument
		return ""
	}

	instructions := TTSPersonalityPrompt + "\n\n" + taskPrompt

	response := generateResponse(ctx, instructions)
	if response == "" {
		span.Status = sentry.SpanStatusInternalError
		return ""
	}

	span.Status = sentry.SpanStatusOK
	return strings.TrimSpace(response)
}

// BuildTTSPrompt wraps a generated DJ script in the fixed audio profile so the
// TTS model has consistent voice direction across every announcement.
func BuildTTSPrompt(script string) string {
	return fmt.Sprintf(TTSAudioProfile, script)
}

// isRetryableTTSError returns true for transient errors worth retrying.
func isRetryableTTSError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "503") || strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "500") || strings.Contains(msg, "internal") {
		return true
	}
	return false
}

// GenerateTTSAudio synthesizes speech from a prompt using Gemini's TTS model.
// The prompt should include the audio profile and transcript (see BuildTTSPrompt).
// Returns raw PCM audio data (16-bit 24kHz mono). The caller is responsible for
// encoding or resampling before sending to Discord.
// Retries once on transient failures (timeouts, 5xx).
func GenerateTTSAudio(ctx context.Context, prompt, voice, model string) ([]byte, error) {
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

	content := []*genai.Content{{Parts: []*genai.Part{{Text: prompt}}}}
	cfg := &genai.GenerateContentConfig{
		ResponseModalities: []string{"AUDIO"},
		SpeechConfig: &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voice,
				},
			},
		},
	}

	var lastErr error
	for attempt := 1; attempt <= ttsMaxAttempts; attempt++ {
		if attempt > 1 {
			log.WithFields(log.Fields{
				"module":  "gemini",
				"attempt": attempt,
				"error":   lastErr,
			}).Warn("Retrying TTS generation after transient failure")

			select {
			case <-time.After(ttsRetryDelay):
			case <-ctx.Done():
				span.Status = sentry.SpanStatusDeadlineExceeded
				return nil, fmt.Errorf("TTS generation cancelled during retry wait: %w", ctx.Err())
			}
		}

		resp, err := defaultClient.Models.GenerateContent(ctx, model, content, cfg)
		if err != nil {
			lastErr = err
			if attempt < ttsMaxAttempts && isRetryableTTSError(err) {
				continue
			}
			log.WithFields(log.Fields{
				"module":  "gemini",
				"model":   model,
				"voice":   voice,
				"attempt": attempt,
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
			"attempt":   attempt,
		}).Debug("TTS audio received from Gemini")

		span.Status = sentry.SpanStatusOK
		return inlineData.Data, nil
	}

	span.Status = sentry.SpanStatusInternalError
	return nil, fmt.Errorf("TTS generation exhausted retries: %w", lastErr)
}
