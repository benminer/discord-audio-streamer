# Fix Gemini Song Hallucination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop Gemini from announcing the wrong song by feeding it authoritative YouTube title + channel metadata with strict instructions not to substitute.

**Architecture:** Add `ChannelName` to `youtube.VideoResponse` and `controller.SongInfo`, populate it from `snippet.ChannelTitle` at every YouTube API call site, then thread it through `PlaybackState` and into Gemini prompts.

**Tech Stack:** Go 1.26, YouTube Data API v3, Google Gemini API

---

## File Map

- `youtube/client.go` — add `ChannelName` to `VideoResponse` and `PlaylistVideoInfo`; populate from API responses.
- `handlers/youtube.go` — carry `ChannelName` when converting `PlaylistVideoInfo` to `VideoResponse`.
- `controller/playback_state.go` — add `ChannelName` to `SongInfo`.
- `controller/controller.go` — thread `ChannelName` through all state mutations and into Gemini calls.
- `gemini/gemini.go` — add `ChannelName` fields to `DJScriptContext`, update prompts, update `GenerateNowPlayingCommentary` signature.
- `controller/*_test.go` — add `ChannelName` field to `VideoResponse` and `SongInfo` literals so tests compile.

---

### Task 1: Enrich `youtube.VideoResponse` with `ChannelName`

**Files:**
- Modify: `youtube/client.go:28-33`

- [ ] **Step 1: Add `ChannelName` field to `VideoResponse`**

```go
type VideoResponse struct {
	Title       string        `json:"title"`
	VideoID     string        `json:"video_id"`
	Duration    time.Duration `json:"duration"`
	ChannelName string        `json:"channel_name"`
}
```

- [ ] **Step 2: Commit**

```bash
git add youtube/client.go
git commit -m "feat(youtube): add ChannelName to VideoResponse"
```

---

### Task 2: Populate `ChannelName` from YouTube API calls

**Files:**
- Modify: `youtube/client.go:96-121` (`GetVideoByID`)
- Modify: `youtube/client.go:40-53` (`PlaylistVideoInfo`)
- Modify: `youtube/client.go:185-199` (`GetPlaylistVideos`)
- Modify: `youtube/client.go:299-309` (`fetchMixViaAPI`)
- Modify: `youtube/client.go:358-368` (`fetchMixViaYtdlp`)
- Modify: `youtube/client.go:435-445` (`Query`)

- [ ] **Step 1: Update `GetVideoByID` to return `ChannelName`**

```go
return VideoResponse{
	Title:       response.Items[0].Snippet.Title,
	VideoID:     videoID,
	ChannelName: response.Items[0].Snippet.ChannelTitle,
}, nil
```

- [ ] **Step 2: Update `PlaylistVideoInfo` struct**

```go
type PlaylistVideoInfo struct {
	VideoID     string
	Title       string
	ChannelName string
	Position    int
}
```

- [ ] **Step 3: Update `GetPlaylistVideos` to populate `ChannelName`**

```go
videos = append(videos, PlaylistVideoInfo{
	VideoID:     videoID,
	Title:       html.UnescapeString(item.Snippet.Title),
	ChannelName: html.UnescapeString(item.Snippet.ChannelTitle),
	Position:    i,
})
```

- [ ] **Step 4: Update `fetchMixViaAPI` to populate `ChannelName`**

Capture channel title in the first loop:

```go
videoIDs = append(videoIDs, vid)
titleMap[vid] = html.UnescapeString(item.Snippet.Title)
channelMap[vid] = html.UnescapeString(item.Snippet.ChannelTitle)
```

Then include it when building `VideoResponse`:

```go
videos = append(videos, VideoResponse{
	Title:       titleMap[item.Id],
	VideoID:     item.Id,
	Duration:    dur,
	ChannelName: channelMap[item.Id],
})
```

- [ ] **Step 5: Update `fetchMixViaYtdlp` to populate `ChannelName`**

The function already calls `service.Videos.List([]string{"snippet", "contentDetails"})`. Add `ChannelName`:

```go
videos = append(videos, VideoResponse{
	Title:       html.UnescapeString(item.Snippet.Title),
	VideoID:     item.Id,
	Duration:    dur,
	ChannelName: html.UnescapeString(item.Snippet.ChannelTitle),
})
```

- [ ] **Step 6: Update `Query` to populate `ChannelName`**

Capture channel title in the search loop:

```go
videoMap[item.Id.VideoId] = html.UnescapeString(item.Snippet.Title)
channelMap[item.Id.VideoId] = html.UnescapeString(item.Snippet.ChannelTitle)
```

Then include it when building `VideoResponse`:

```go
videos = append(videos, VideoResponse{
	Title:       videoMap[item.Id],
	VideoID:     item.Id,
	Duration:    ytDuration,
	ChannelName: channelMap[item.Id],
})
```

- [ ] **Step 7: Run tests for `youtube` package**

```bash
go test ./youtube/...
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add youtube/client.go
git commit -m "feat(youtube): populate ChannelName from YouTube API responses"
```

---

### Task 3: Carry `ChannelName` through playlist handler

**Files:**
- Modify: `handlers/youtube.go:72-79`

- [ ] **Step 1: Include `ChannelName` in conversion**

```go
videos = append(videos, youtube.VideoResponse{
	Title:       v.Title,
	VideoID:     v.VideoID,
	ChannelName: v.ChannelName,
})
```

- [ ] **Step 2: Run tests for `handlers` package**

```bash
go test ./handlers/...
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add handlers/youtube.go
git commit -m "feat(handlers): carry ChannelName through playlist conversion"
```

---

### Task 4: Add `ChannelName` to `controller.SongInfo`

**Files:**
- Modify: `controller/playback_state.go:13-18`

- [ ] **Step 1: Add `ChannelName` field**

```go
type SongInfo struct {
	Title       string
	VideoID     string
	IsRadioPick bool
	QueuedBy    string
	ChannelName string
}
```

- [ ] **Step 2: Commit**

```bash
git add controller/playback_state.go
git commit -m "feat(controller): add ChannelName to SongInfo"
```

---

### Task 5: Wire `ChannelName` through controller state mutations

**Files:**
- Modify: `controller/controller.go:1191-1196`
- Modify: `controller/controller.go:1216-1219`
- Modify: `controller/controller.go:1881-1891`
- Modify: `controller/controller.go:1905-1910`
- Modify: `controller/controller.go:1970-1986`
- Modify: `controller/controller.go:2115-2130`
- Modify: `controller/controller.go:2209-2214`

- [ ] **Step 1: Update `PlaybackStarted` `SetCurrent` call**

```go
p.playbackState.SetCurrent(SongInfo{
	Title:       queueItem.Video.Title,
	VideoID:     queueItem.Video.VideoID,
	IsRadioPick: queueItem.IsRadioPick,
	QueuedBy:    p.resolveQueuedBy(queueItem),
	ChannelName: queueItem.Video.ChannelName,
})
```

- [ ] **Step 2: Update `SongHistory.Add` call**

```go
p.SongHistory.Add(SongHistoryEntry{
	VideoID:     queueItem.Video.VideoID,
	Title:       queueItem.Video.Title,
	ChannelName: queueItem.Video.ChannelName,
})
```

- [ ] **Step 3: Update `Remove` to snapshot channel name**

Add to the snapshot before unlocking:

```go
if len(p.Queue.Items) > 0 {
	hasNext = true
	nextTitle = p.Queue.Items[0].Video.Title
	nextVideoID = p.Queue.Items[0].Video.VideoID
	nextChannelName = p.Queue.Items[0].Video.ChannelName
	nextIsRadioPick = p.Queue.Items[0].IsRadioPick
	if p.Queue.Items[0].Interaction != nil {
		nextUserID = p.Queue.Items[0].Interaction.UserID
	}
}
```

Then pass it to `SetNext`:

```go
p.playbackState.SetNext(&SongInfo{
	Title:       nextTitle,
	VideoID:     nextVideoID,
	IsRadioPick: nextIsRadioPick,
	QueuedBy:    queuedBy,
	ChannelName: nextChannelName,
})
```

- [ ] **Step 4: Update `Shuffle` to snapshot channel name**

```go
if len(p.Queue.Items) > 0 {
	nextTitle = p.Queue.Items[0].Video.Title
	nextVideoID = p.Queue.Items[0].Video.VideoID
	nextChannelName = p.Queue.Items[0].Video.ChannelName
	nextIsRadioPick = p.Queue.Items[0].IsRadioPick
}
```

Then pass to `SetNext`:

```go
p.playbackState.SetNext(&SongInfo{
	Title:       nextTitle,
	VideoID:     nextVideoID,
	IsRadioPick: nextIsRadioPick,
	ChannelName: nextChannelName,
})
```

- [ ] **Step 5: Update `attemptVoiceRecovery` fresh item**

```go
freshItem := &GuildQueueItem{
	Video:          savedItem.Video,
	ProbedDuration: savedItem.ProbedDuration,
	AddedAt:        time.Now(),
	LoadAttempts:   0,
	MaxAttempts:    3,
	Context:        context.Background(),
	IsRadioPick:    savedItem.IsRadioPick,
	Interaction:    savedItem.Interaction,
	LoadResult:     nil,
	Stream:         nil,
	streamReady:    make(chan struct{}),
}
```

`savedItem.Video` already carries `ChannelName`, so the fresh item inherits it automatically. No code change needed beyond confirming the struct copy.

- [ ] **Step 6: Update `syncNextFromQueue`**

```go
p.playbackState.SetNext(&SongInfo{
	Title:       next.Video.Title,
	VideoID:     next.Video.VideoID,
	IsRadioPick: next.IsRadioPick,
	QueuedBy:    queuedBy,
	ChannelName: next.Video.ChannelName,
})
```

- [ ] **Step 7: Run tests for `controller` package**

```bash
go test ./controller/...
```

Expected: initial compile failures due to `SongInfo` literal fields in tests (fixed in Task 8)

- [ ] **Step 8: Commit**

```bash
git add controller/controller.go
git commit -m "feat(controller): thread ChannelName through PlaybackState"
```

---

### Task 6: Update Gemini `DJScriptContext` and prompts

**Files:**
- Modify: `gemini/gemini.go:44-52`
- Modify: `gemini/gemini.go:537-557` and `562-567`

- [ ] **Step 1: Add fields to `DJScriptContext`**

```go
type DJScriptContext struct {
	Type               AnnouncementType
	CurrentSong        string
	CurrentChannelName string
	NextSong           string
	NextChannelName    string
	RecentHistory      []string
	IsRadioPick        bool
	CurrentQueuedBy    string
	NextQueuedBy       string
}
```

- [ ] **Step 2: Update transition prompt**

Replace the existing task prompt block in `AnnouncementTransition` with:

```go
taskPrompt = fmt.Sprintf(`Current song (just finished): %s (channel: %s)
Next up: %s (channel: %s)

%s

%s
%s
IMPORTANT: You MUST announce the songs using ONLY the exact titles and channels provided above. Do not guess, substitute, or use your own knowledge about what artist or song this might be.

Your task: Announce what just played and what's coming up next. Write it as two halves: first announce what just played, then what's next.
- You MUST say BOTH the song/artist that just played AND the song/artist coming up next
- Never omit either name — they are the entire point of the announcement
- If you know who queued a song, mention them by name. Skip attribution for songs with no requester.
- If both songs were queued by the same person, mention them once naturally (e.g. "both queued by Ben").

Now write your transition:`, sc.CurrentSong, sc.CurrentChannelName, sc.NextSong, sc.NextChannelName, recentHistoryBlock(sc.RecentHistory), radioStr, requesterStr)
```

- [ ] **Step 3: Update intro prompt**

```go
taskPrompt = fmt.Sprintf(`First song of the session: %s (channel: %s)

IMPORTANT: You MUST say the exact song title and channel provided above. Do not guess or substitute.

Your task: Introduce the first song of the session. Build a little anticipation — you're kicking things off.
- You MUST say the song/artist

Now write your intro:`, sc.NextSong, sc.NextChannelName)
```

- [ ] **Step 4: Update radio start prompt**

```go
taskPrompt = fmt.Sprintf(`Your task: Radio mode was just turned on. You're taking over as DJ.
Announce that radio mode is on and introduce your first pick: %s (channel: %s).
Keep it brief and natural.

IMPORTANT: You MUST say the exact song title and channel provided above. Do not guess or substitute.

Now write your announcement:`, sc.NextSong, sc.NextChannelName)
```

- [ ] **Step 5: Commit**

```bash
git add gemini/gemini.go
git commit -m "feat(gemini): include ChannelName in DJ script prompts"
```

---

### Task 7: Update `GenerateNowPlayingCommentary`

**Files:**
- Modify: `gemini/gemini.go:382-471`
- Modify: `controller/controller.go:2445`

- [ ] **Step 1: Update function signature**

```go
func GenerateNowPlayingCommentary(ctx context.Context, currentSong, channelName string, recentHistory []string, isRadioPick bool) string {
```

- [ ] **Step 2: Update prompt to include channel name and strict instruction**

```go
instructions := buildPrompt(fmt.Sprintf(`Current song playing: **%s**
Channel: %s

%s

%s

Your task: Write ONE sentence of commentary about this song. Two sentences only if the second adds something the first genuinely can't.

IMPORTANT: Your commentary MUST reference the song using ONLY the exact title and channel provided above. Do not guess the artist or substitute with your own knowledge.

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

Now write your commentary:`, currentSong, channelName, historyStr, radioStr))
```

- [ ] **Step 3: Update controller call site**

```go
commentary := gemini.GenerateNowPlayingCommentary(ctx, queueItem.Video.Title, queueItem.Video.ChannelName, recentSongs, queueItem.IsRadioPick)
```

- [ ] **Step 4: Commit**

```bash
git add gemini/gemini.go controller/controller.go
git commit -m "feat(gemini): pass channel name to now-playing commentary"
```

---

### Task 8: Update controller call sites for `GenerateDJScript`

**Files:**
- Modify: `controller/controller.go:1490-1493`
- Modify: `controller/controller.go:2281-2289`

- [ ] **Step 1: Update `startRadioMode` call**

```go
script := gemini.GenerateDJScript(scriptCtx, gemini.DJScriptContext{
	Type:            gemini.AnnouncementRadioStart,
	NextSong:        picked.Title,
	NextChannelName: picked.ChannelName,
})
```

- [ ] **Step 2: Update `generateTransitionTTS` call**

```go
script := gemini.GenerateDJScript(scriptCtx, gemini.DJScriptContext{
	Type:               gemini.AnnouncementTransition,
	CurrentSong:        current.Title,
	CurrentChannelName: current.ChannelName,
	NextSong:           next.Title,
	NextChannelName:    next.ChannelName,
	RecentHistory:      recentHistory,
	IsRadioPick:        next.IsRadioPick,
	CurrentQueuedBy:    current.QueuedBy,
	NextQueuedBy:       next.QueuedBy,
})
```

- [ ] **Step 3: Commit**

```bash
git add controller/controller.go
git commit -m "feat(controller): pass ChannelName to Gemini DJ script context"
```

---

### Task 9: Fix test compilation

**Files:**
- Modify: `controller/controller_test.go:106,145,152,215,317,321,394`
- Modify: `controller/accessors_test.go:28,45,171,172,227`
- Modify: `controller/wait_for_stream_test.go:15,35,52,67,90,129`

- [ ] **Step 1: Add `ChannelName` field to `VideoResponse` literals**

For each `youtube.VideoResponse{...}` literal in tests, add `ChannelName: ""` so the struct literal remains valid. Zero value is fine for these tests.

Example:

```go
Video: youtube.VideoResponse{Title: "test song", ChannelName: ""},
```

- [ ] **Step 2: Add `ChannelName` field to `SongInfo` literals**

For each `SongInfo{...}` literal in tests, add `ChannelName: ""`.

Example:

```go
ps.SetCurrent(SongInfo{Title: "test song", VideoID: "1", ChannelName: ""})
```

- [ ] **Step 3: Run controller tests**

```bash
go test ./controller/...
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add controller/*_test.go
git commit -m "test(controller): add ChannelName to test literals"
```

---

### Task 10: Full build and test verification

- [ ] **Step 1: Run full test suite**

```bash
go test ./...
```

Expected: all packages PASS

- [ ] **Step 2: Run build**

```bash
go build
```

Expected: no errors, binary produced

- [ ] **Step 3: Commit if any final fixes were needed**

If no fixes were needed, no commit needed.

---

## Self-Review Checklist

- [ ] Spec coverage: every spec section (data model, population, wiring, Gemini prompts, tests) has at least one task.
- [ ] No placeholders: no "TBD", "TODO", or vague steps.
- [ ] Type consistency: `ChannelName` is used consistently across `VideoResponse`, `PlaylistVideoInfo`, `SongInfo`, `DJScriptContext`, and `GenerateNowPlayingCommentary`.
- [ ] Test plan: tests are updated to compile with the new field.
