# CLAUDE.md - Agent Guide for Discord Audio Streamer

## Project Overview
Lightweight Discord bot for streaming audio from YouTube, Spotify, and Apple Music to voice channels in private servers. Built for personal use with Go 1.24+.

**Key Features:**
- Slash commands: `/play` (search/URL/playlist), `/queue`, `/view`, `/skip`, `/pause`, `/resume`, `/volume`, `/remove`, `/shuffle`, `/reset`, `/radio`, `/history`, `/leaderboard`, `/help`.
- Queue management: Add singles/playlists/albums, deduplication, shuffle.
- Integrations: YouTube API (search/playlists), Spotify (tracks/playlists/albums), Apple Music (tracks/albums/playlists), Gemini AI (sassy DJ responses).
- Playback: FFmpeg audio buffering, Opus encoding, fade-outs, voice recovery, now-playing cards with progress.
- Persistence: SQLite for history/leaderboard/username cache.
- Extras: Idle disconnect (20min), radio mode (auto-queue similar songs via Gemini/YouTube).

## Architecture
```
Discord Interactions → handlers/handlers.go (slash cmds, URL parsing)
                  ↓
Controller (GuildPlayer per guild) → GuildQueue (mutex-protected, events)
                  ↓
youtube/client.go (API queries, yt-dlp streams) / spotify/ / applemusic/
                  ↓
audio/loader.go (FFmpeg → memory buffer ~55MB) → audio/player.go (Opus → Discord voice)
```

- **GuildPlayer**: Manages queue, voice conn, loaders/players, events (add/skip/clear), idle checker, radio, now-playing.
- **LoadResult**: FFmpeg-buffered audio (s16le, 48kHz stereo); GC via nil refs post-play.
- **Events**: Channels for queue/load/playback notifications; goroutines per guild.
- **Deploy**: Docker (ARM64 optimized for Mac Mini M-series), ngrok/Cloudflare tunnel.

## Key Files
| File/Dir | Purpose |
|----------|---------|
| `README.md` | Setup, Docker, env vars, features. |
| `controller/controller.go` | GuildPlayer/QueueItem, voice mgmt, events, radio, now-playing cards. |
| `handlers/handlers.go` | Slash cmd handlers, Spotify/Apple/YouTube parsing (parallel searches). |
| `youtube/client.go` | YouTube search/playlists, yt-dlp stream URLs (retries on 403). |
| `audio/loader.go` | FFmpeg loads to memory (30s timeout), notifications. |
| `audio/player.go` | Opus encoding, fade-outs, pause/resume (atomic state). |
| `docker-build.sh`, `docker-restart.sh`, `docker-logs.sh` | ARM64 builds, high-res (16GB mem/8CPU), restart. |
| `.env.common` / `.env.example` | Tokens (Discord/YouTube/Spotify/Gemini), limits (playlists=15). |
| `go.mod` | discordgo fork (voice fixes), spotify/v2, genai, sentry. |
| `database/` | SQLite history/leaderboard/usernames. |
| `gemini/` | AI responses (sassy DJ), radio recs. |

## Common Tasks
- **Bug Fixes**: Unmarshal snowflakes (`json:\",string\"`), durations (FFmpeg probe > YT metadata), voice stale (rejoin on reset).
- **Features**: Spotify search (extend `QueryAndQueue`), now-playing updates (every 5s).
- **Deploy**: `./docker-build.sh && ./docker-restart.sh rebuild` (pulls env vars).
- **Test**: `./docker-logs.sh | grep -E 'nowplay|interaction|queue|error'`. Check Sentry.
- **Profile**: `docker stats`, `go tool pprof`.

## Coding Standards
- **AGENTS.md**: Plan in `tasks/todo.md`, subagent review (`git diff --cached`), senior eng (root cause/tests).
- **Go Idioms**: `sync.Mutex` queues, `atomic.Bool` player state, ctx-tracing (Sentry), `logrus` fields, error wrapping.
- **Style**: Concise funcs, early returns, no panics (recover in handlers), comments for \"why\".

## Recent Changes (git log -5)
- `b2bf05f` Fix Discord interaction `data.id` unmarshal (#49) – `json:\",string\"`.
- `3d1a8ca` `.gitignore data/`, docker-logs container name.
- `22cc115` Mac Mini optimize (ARM64, 16GB/8CPU) (#48).
- `f592861` Merge now-playing cards phase1.
- `ecb4700` Suppress health logs.

**PRs**: #49 (merged), now-playing (#47/#46), Gemini radio (#45), Apple Music (#44/#43).

## Pitfalls
- **Snowflake IDs**: Always `json:\",string\"` (handlers.InteractionData.ID).
- **LoadResult GC**: `popQueue()` nils refs (~55MB buffers).
- **ARM64 Docker**: `--platform linux/arm64`, high res (`--memory=16g --cpus=8`).
- **Voice Fork**: MohmmedAshraf/discordgo (encryption/buffers); `vc.Status == Ready`, ctx joins.
- **yt-dlp 403**: Auto-refresh stream URL (3 retries).
- **Race Conditions**: Queue.Mutex.Lock() for all ops; atomic player flags.
- **Gemini Fallbacks**: Empty resp → static msg.
- **Memory**: Buffers per-song; GC fine on Go 1.24+.

**Pro Tip**: For changes, `go build && docker-restart.sh rebuild && docker-logs.sh` → test loop.
