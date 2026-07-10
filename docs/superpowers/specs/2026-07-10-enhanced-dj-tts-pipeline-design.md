# Enhanced DJ TTS Pipeline

## Context

The bot's TTS DJ announcements sound robotic and don't act like a real DJ. The voice doesn't mention song or artist names reliably, delivery is flat regardless of the music's energy, and the three announcement types (transition, intro, queue-empty) each use different ad-hoc code paths with inconsistent prompt quality.

Gemini's speech generation API supports audio tags (`[excited]`, `[warm]`, `[laughs]`, etc.) and structured audio profiles (character persona, director's notes for style/pacing) that can make TTS sound dramatically more natural. The newer `gemini-3.1-flash-tts-preview` model also improves baseline quality.

This change introduces a two-step pipeline: an LLM generates a DJ script with inline audio tags, then the TTS model speaks it within a fixed audio profile that establishes the DJ's character and delivery style.

## Architecture

### Pipeline Flow

```
Song Context (current, next, history, radio mode)
    │
    ▼
┌─────────────────────────────────────────┐
│  Step 1: GenerateDJScript               │
│  Model: gemini-2.5-flash (text)         │
│  Input: TTS system prompt + song context│
│  Output: DJ script with inline audio    │
│          tags (plain text, no markdown)  │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│  Step 2: BuildTTSPrompt                 │
│  Wraps script in fixed audio profile    │
│  (character persona + director's notes) │
└─────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────┐
│  Step 3: GenerateTTSAudio               │
│  Model: gemini-3.1-flash-tts-preview    │
│  Input: Full TTS prompt (profile +      │
│         script with audio tags)         │
│  Output: Raw PCM audio (24kHz mono)     │
└─────────────────────────────────────────┘
    │
    ▼
  ConvertTTSToDiscord (existing, unchanged)
    │
    ▼
  Player playback (existing, unchanged)
```

### Announcement Types

All three types flow through the same pipeline with different context:

| Type | Trigger | Context Provided |
|------|---------|-----------------|
| **Transition** | Song starts playing, next song exists | Current song, next song, recent history, radio mode |
| **Intro** | First song queued on idle channel | Song title only |
| **Queue Empty** | Last song ends, queue is empty | None (mentions /queue and /radio) |

## Detailed Design

### 1. New `GenerateDJScript` Function (`gemini/gemini.go`)

Replaces `GenerateDJTransitionScript` and the two inline `GenerateRaw` calls.

```go
type AnnouncementType int

const (
    AnnouncementTransition AnnouncementType = iota
    AnnouncementIntro
    AnnouncementQueueEmpty
)

type DJScriptContext struct {
    Type          AnnouncementType
    CurrentSong   string   // song that just played (transition)
    NextSong      string   // song coming up (transition, intro)
    RecentHistory []string // recent song titles
    IsRadioPick   bool     // whether next song was auto-queued by radio
}

func GenerateDJScript(ctx context.Context, sc DJScriptContext) string
```

**System prompt** (separate from `PersonalityPrompt`, no markdown rules):

```
You are beatbot, a DJ announcing between songs on a Discord music channel.
Write what you'd actually SAY out loud. Natural, warm, confident. Not a
morning show host, not monotone. The person at the party with impeccable
taste who genuinely loves what's playing.

Use audio tags in square brackets to direct your delivery. Place them
before the words they should color. Match the energy to the music:
- [warm] — smooth, appreciative moments
- [excited] — high-energy drops, bangers, hype moments
- [smooth] — chill transitions, laid-back vibes
- [laughs] — genuine amusement, playful moments
- [chill] — relaxed, easy-going delivery

Rules:
- 1-2 sentences, 15-25 words max
- You MUST say the song title and artist for every song you reference
- No markdown, no asterisks, no formatting — this is spoken out loud
- Natural spoken cadence, not written prose
- At least one audio tag to set the tone
```

Type-specific instructions appended:

- **Transition**: "Announce what just played and what's coming up next. Two halves: 'That was X...' then '...up next is Y.' Current song: {current}. Next up: {next}."
- **Intro**: "Introduce the first song of the session. Build a little anticipation. Song: {next}."
- **Queue Empty**: "The queue just ran out. Mention they can add songs with /queue or turn on non-stop music with /radio."

Recent history and radio-mode context included when available.

### 2. New `BuildTTSPrompt` Function (`gemini/gemini.go`)

Wraps the LLM-generated script in a fixed audio profile.

```go
func BuildTTSPrompt(script string) string
```

Returns a string with this structure:

```
AUDIO PROFILE: beatbot / "The Booth"
A warm, confident music lover who happens to be your DJ. Not a hype host.
The person who puts on the perfect record and genuinely loves sharing it.
When they speak between songs, you hear real appreciation for what just
played and natural anticipation for what's next.

DIRECTOR'S NOTES:
Style: Natural warmth with easy confidence. Not performing excitement,
just letting it come through. Think late-night radio, the DJ who makes
you feel like you're the only one listening. The "vocal smile" — you can
hear the grin without it being forced.
Pacing: Conversational flow. Not rushed, not dragging. Words breathe
naturally, like talking to friends between songs. Slight emphasis on
artist and song names.

TRANSCRIPT:
{script with audio tags}
```

The audio profile is a package-level constant (`TTSAudioProfile`), stored alongside the existing `PersonalityPrompt` in `gemini/personality.go`.

### 3. Model Bump (`config/config.go`)

Change the default TTS model:

```go
func getGeminiTTSModel() string {
    model := os.Getenv("GEMINI_TTS_MODEL")
    if model == "" {
        return "gemini-3.1-flash-tts-preview"  // was gemini-2.5-flash-preview-tts
    }
    return model
}
```

### 4. Controller Changes (`controller/controller.go`)

All three call sites converge on the same pattern:

**`preGenerateTTS`** (transitions):
```go
// Before:
script := gemini.GenerateDJTransitionScript(ctx, currentSong, nextSong, recentHistory, isRadioPick)
audioBytes, err := gemini.GenerateTTSAudio(ctx, script, p.AnnounceVoice, "")

// After:
script := gemini.GenerateDJScript(ctx, gemini.DJScriptContext{
    Type:          gemini.AnnouncementTransition,
    CurrentSong:   currentSong,
    NextSong:      nextSong,
    RecentHistory: recentHistory,
    IsRadioPick:   isRadioPick,
})
ttsPrompt := gemini.BuildTTSPrompt(script)
audioBytes, err := gemini.GenerateTTSAudio(ctx, ttsPrompt, p.AnnounceVoice, "")
```

**`preGenerateIntroAnnouncement`** (intro):
```go
// Before:
script := gemini.GenerateRaw(ctx, "Say something brief as a radio DJ...")

// After:
script := gemini.GenerateDJScript(ctx, gemini.DJScriptContext{
    Type:    gemini.AnnouncementIntro,
    NextSong: title,
})
ttsPrompt := gemini.BuildTTSPrompt(script)
audioBytes, err := gemini.GenerateTTSAudio(ctx, ttsPrompt, p.AnnounceVoice, "")
```

**`playNoMoreSongsMessage`** (queue empty):
```go
// Before:
script := gemini.GenerateRaw(ctx, "Say the queue is empty in a cool radio DJ voice...")

// After:
script := gemini.GenerateDJScript(ctx, gemini.DJScriptContext{
    Type: gemini.AnnouncementQueueEmpty,
})
ttsPrompt := gemini.BuildTTSPrompt(script)
audioBytes, err := gemini.GenerateTTSAudio(ctx, ttsPrompt, p.AnnounceVoice, "")
```

Static fallback for queue-empty when Gemini is disabled stays unchanged.

### 5. What Gets Removed

- `GenerateDJTransitionScript` in `gemini/gemini.go` (only caller is `preGenerateTTS`)
- The three inline `GenerateRaw` calls for TTS scripts in the controller are replaced by `GenerateDJScript`
- `GenerateRaw` itself stays. It's still used by `helpers/gemini.go` (text chat responses) and `handlers/playback.go` (voice-demo command)
- `buildPrompt` / `PersonalityPrompt` remain for text chat responses, not used by the new TTS pipeline

Note: `refreshTransitionTTS` delegates to `preGenerateTTS`, so updating `preGenerateTTS` automatically covers the queue-mutation refresh path. The `/voice-demo` handler (`handlers/playback.go:317`) is left as-is since it's a one-off sample, not a real announcement.

### 6. What Stays Unchanged

- `GenerateTTSAudio` function signature and behavior (receives a richer prompt, same output)
- `audio/tts.go` — ConvertTTSToDiscord pipeline
- `audio/player.go` — inline TTS before EOF, standalone PlayAnnouncement
- All staleness guards, generation counters, refresh logic
- Voice selection, `/voices`, `/announce`, `/voice-demo` commands
- `PersonalityPrompt` constant (used for text chat, not TTS)

## Files Modified

| File | Change |
|------|--------|
| `gemini/gemini.go` | Add `GenerateDJScript`, `BuildTTSPrompt`, `AnnouncementType` enum, `DJScriptContext` struct. Remove `GenerateDJTransitionScript`. |
| `gemini/personality.go` | Add `TTSAudioProfile` and `TTSPersonalityPrompt` constants. |
| `config/config.go` | Change default TTS model to `gemini-3.1-flash-tts-preview`. |
| `controller/controller.go` | Update `preGenerateTTS`, `preGenerateIntroAnnouncement`, `playNoMoreSongsMessage` to use new pipeline. |
| `cmd/tts-debug/main.go` | Update to use new pipeline for testing (optional, low priority). |

## Verification

1. **Build**: `go build` passes with no errors
2. **Type check**: No new warnings from `go vet`
3. **Manual test with tts-debug tool**: Run `cmd/tts-debug/main.go` with the new pipeline and compare WAV output quality against the old pipeline
4. **Live test in Discord**: Queue 2-3 songs, verify:
   - Transition announcements mention both song names
   - Audio tags produce audible delivery variation (excited vs. smooth)
   - Intro announcement plays before the first song
   - Queue-empty announcement plays when the queue drains
   - Voice sounds natural, warm, not robotic
5. **Edge cases**: Skip rapidly, add songs mid-play (refresh TTS), test with radio mode
