# AI Agent Instructions for Discord Music Bot

## Working Style & Communication

### Ben's Preferences
- **Be honest and forthright** - Don't sugarcoat issues or make changes just because asked
- **Analyze first, implement second** - Always discuss findings before making code changes
- **Explain trade-offs** - Present options with pros/cons, let Ben decide
- **No unnecessary apologizing** - Stay professional but conversational
- **Use modern Go idioms** - Range loops over index-based when appropriate
- **Clean code over clever code** - Readability matters

### When Making Changes
1. **Discuss first** - Especially for architectural decisions
2. **Test builds** - Always run `go build` after edits
3. **Add comments for future reference** - Document why, not just what
4. **Consider race conditions** - This is a concurrent Go app with goroutines everywhere

## Project Architecture

### Key Components

**`audio/player.go`** - Audio playback engine (singleton per guild)
- Handles Opus encoding, fade-outs, pause/resume
- Uses atomic.Bool for thread-safe state (`paused`, `stopping`)
- Reused across songs - must reset state properly
- **Critical**: Use `io.ReadFull()` when reading from FFmpeg pipes (see comments in file)

**`audio/loader.go`** - FFmpeg stream loader
- **Streams from FFmpeg stdout** (not memory buffering)
- Memory buffering = huge allocations = GC pauses = audio stutters
- See detailed comments in file about streaming vs buffering trade-offs
- If audio quality degrades, check that player.go uses `io.ReadFull()`

**`controller/controller.go`** - Per-guild player management
- Manages queue, voice connections, event routing
- Spawns goroutines for event listeners (queue, load, playback)
- GuildPlayer is singleton per guild, Player is reused

**`youtube/client.go`** - YouTube integration
- Uses batched API calls (avoid N+1 queries)
- yt-dlp for stream URL extraction (can't avoid ~1-2s latency)
- Optimized flags: no OGG preference, direct bestaudio

**`discord/voice.go`** - Voice connection helpers
- Uses MohmmedAshraf's fork with encryption fixes + buffer size tweaks
- Context-based timeouts, Status field instead of Ready boolean

### Important Architectural Decisions

#### Audio Streaming (Not Buffering)
- FFmpeg streams via stdout pipe, not loaded to memory
- **Why**: 55MB+ allocations caused GC pauses → audio stutters
- **Trade-off**: Must use `io.ReadFull()` to avoid partial reads
- **Revert guide**: See comments in `audio/loader.go`

#### Fade-Out on All Exits
- 5-frame (100ms) cubic fade prevents audio artifacts
- Applied to: pause, skip, stop, end-of-stream
- **Why**: Discord's 100-packet buffer + abrupt cuts = loud pops/distortion

#### Thread Safety
- Use `atomic.Bool` for shared state accessed by multiple goroutines
- Player state (`paused`, `stopping`) uses atomics, not mutexes
- Mutex in Play() held for entire duration - avoid adding more mutex contention

## Discord Voice Setup

### The Fork Situation
**Using**: `github.com/MohmmedAshraf/discordgo` (branch: `shotcaller-voice-encryption-fix-updated`)

**Why not base discordgo?**
- Base library uses deprecated `xsalsa20_poly1305` encryption
- Discord rejects with error 4016 (Unknown encryption mode)
- Need modern modes: `aead_aes256_gcm_rtpsize`, `aead_xchacha20_poly1305_rtpsize`

**Why not ozraru's fork (PR #1593)?**
- Has encryption fix BUT small OpusSend/OpusRecv buffers (2 packets)
- Small buffers = dropped packets = audio stuttering
- MohmmedAshraf's fork = encryption fix + 100-packet buffers

**API Differences from base discordgo:**
```go
// Base:
vc.ChannelVoiceJoin(guildId, channelId, false, true)
vc.Disconnect()
vc.Ready  // boolean

// Fork:
vc.ChannelVoiceJoin(ctx, guildId, channelId, false, true)
vc.Disconnect(ctx)
vc.Status == discordgo.VoiceConnectionStatusReady  // enum
```

## Common Issues & Solutions

### Audio Stuttering
1. **Check if streaming** - Should NOT be buffering to memory
2. **Check io.ReadFull usage** - Binary.Read can cause artifacts with pipes
3. **GC pressure** - Large allocations cause pauses
4. **Network issues** - Can't fix, but confirm it's not code

### Audio Artifacts/Distortion on Pause/Skip
- Needs fade-out (already implemented)
- Cubic curve works better than linear
- 5 frames (100ms) is the sweet spot

### Silent Songs After Skip
- `stopping` flag not reset at Play() start
- Always reset state flags when starting new song

### Race Conditions
- Check all goroutine spawning points
- Shared state must use atomics or mutexes
- Player is reused, state persists between songs

## Testing & Debugging

### Build & Run
```bash
go build
./beatbot  # or your binary name
```

### Environment Variables
Key vars in `.env`:
- `DISCORD_BOT_TOKEN` - Required
- `DISCORD_APP_ID` - Required
- `YOUTUBE_API_KEY` - Required for searches
- `AUDIO_QUALITY` - Was tested, not implemented (didn't help)

### Log Levels
- TRACE - Very verbose, use for debugging specific issues
- DEBUG - Moderate detail
- INFO - Standard operation
- WARN/ERROR - Problems

### Race Detector
```bash
go run -race .
```
Should be clean with atomic.Bool usage.

## Code Patterns

### Error Handling
- Log with context (use logrus fields)
- Send Sentry for unexpected errors
- User-facing errors via Discord followups

### Goroutines
- Event listeners in controller spawn goroutines
- Always consider cleanup (though this app runs indefinitely)
- Be careful with `go p.playNext()` - can explode on errors

### Channels
- Use buffered channels for notifications (100 capacity)
- Player has `completed` channel for stopping
- Loader has `canceled` channel for cancellation

## Performance Optimizations Made

1. **YouTube API batching** - 11 calls → 2 calls (~3-4s savings)
2. **yt-dlp flags** - Removed OGG preference, added timeouts (~300ms savings)
3. **FFmpeg streaming** - No memory buffering (eliminates GC stutters)
4. **Atomic operations** - Lock-free state checking (faster than mutexes)

## Things NOT to Change Without Discussion

1. **FFmpeg streaming approach** - Reverting to buffering affects memory/GC
2. **Fade-out curve/duration** - Tuned to avoid artifacts
3. **Fork choice** - MohmmedAshraf's has specific fixes we need
4. **OpusSend buffer size** - 100 is in the fork, don't change in our code
5. **Audio encoder settings** - Complexity 10, max bitrate works well

## Future Ideas (Not Implemented)

- Audio quality config (LOW/MEDIUM/HIGH) - tested, didn't help with stuttering
- Stream URL caching - complex due to expiration
- Pre-fetching next song - could improve perceived latency
- Goroutine cleanup - app runs indefinitely so not critical

## Getting Unstuck

1. **Check recent git commits** - Context for why changes were made
2. **Read inline comments** - Especially in loader.go and player.go
3. **Search for similar patterns** - Codebase is consistent
4. **Ask Ben** - He knows the history and will be honest about trade-offs

## Working with Ben

- He appreciates deep analysis over quick fixes
- "Let's discuss" means present options, don't just pick one
- He'll tell you to revert if something isn't working
- Clean branches make experimentation safe
- He values learning the "why" as much as the "how"

---

**Remember**: This is a real-time audio streaming application. Timing, concurrency, and memory management matter more than in typical CRUD apps. When in doubt, discuss trade-offs first!
