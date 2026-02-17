# Discord Music Bot

A lightweight Discord bot for streaming audio to voice channels in private servers. This is a personal project built for fun and learning - not intended as a commercial product or service.

## About

A simple bot implementation for playing audio in Discord voice channels using [yt-dlp](https://github.com/yt-dlp/yt-dlp).

## Prerequisites

1. **Go 1.24+** - Go programming language runtime

   - Download from [golang.org](https://golang.org/dl/)
   - Verify installation with: `go version`

2. **yt-dlp** - Media download utility

   - Install from [yt-dlp/yt-dlp](https://github.com/yt-dlp/yt-dlp)
   - Verify installation with: `yt-dlp --version`

3. **FFmpeg** - Audio processing

   - Install from [ffmpeg.org](https://ffmpeg.org/download.html) or via package manager
   - Verify installation with: `ffmpeg -version`

4. **Docker** (optional) - Containerization tool

   - Install from [Docker](https://docs.docker.com/get-docker/)
   - Verify installation with: `docker --version`

## Discord Setup

1. Create a new application in the [Discord Developer Portal](https://discord.com/developers/applications)

2. Create a bot for your application
   - Documentation: [Discord Bot](https://discord.com/developers/docs/interactions/application-commands#registering-a-command)

## Setup

1. Clone this repository:

   ```bash
   git clone https://github.com/benminer/discord-audio-streamer.git
   cd discord-audio-streamer
   ```

2. Create a `.env` file with the required parameters:

   ```bash
   # Required
   DISCORD_BOT_TOKEN=your_bot_token
   DISCORD_APP_ID=your_app_id
   YOUTUBE_API_KEY=your_youtube_api_key

   # Optional - YouTube playlist limit
   YOUTUBE_PLAYLIST_LIMIT=15

   # Optional - Spotify integration
   SPOTIFY_ENABLED=false
   SPOTIFY_CLIENT_ID=your_spotify_client_id
   SPOTIFY_CLIENT_SECRET=your_spotify_client_secret
   SPOTIFY_PLAYLIST_LIMIT=10

   # Optional - Gemini AI
   GEMINI_ENABLED=false
   GEMINI_API_KEY=your_gemini_api_key

   # Optional - Idle timeout (minutes before disconnecting from empty channel)
   IDLE_TIMEOUT_MINUTES=20

   # Optional - Audio bitrate (in bps, default: 128000)
   # Range: 8000-512000 (8 kbps to 512 kbps)
   # Recommended values:
   #   64000 (64 kbps) - Very stable, slight quality trade-off
   #   96000 (96 kbps) - Good balance of quality and stability
   #   128000 (128 kbps) - Default, maximum for regular voice channels
   #   384000 (384 kbps) - Maximum for stage channels (requires boost)
   AUDIO_BITRATE=128000

   # Optional - Sentry error tracking
   SENTRY_DSN=your_sentry_dsn
   ```

## Build and Run

1. Install dependencies:

   ```bash
   go mod download
   ```

2. Build the project:

   ```bash
   go build -o discord-bot
   ```

3. Run the bot:
   ```bash
   ./discord-bot
   ```

## Docker

1.  Build the Docker image:

    ```bash
    docker build -t discord-music-bot:latest ./
    ```

    **Note for Apple Silicon users:** You may need to use `docker buildx` to build an image compatible with your architecture:

    ```bash
    docker buildx build --platform linux/amd64 -t discord-music-bot:latest .
    ```

2.  Run the Docker container:

    ```bash
    docker run -d --name discord-music-bot \
      --restart always \
      --memory="1g" \
      --memory-reservation="512m" \
      --memory-swap="2g" \
      --cpus="2" \
      --cpu-shares="2048" \
      -e DISCORD_APP_ID=$DISCORD_APP_ID \
      -e DISCORD_PUBLIC_KEY=$DISCORD_PUBLIC_KEY \
      -e DISCORD_BOT_TOKEN=$DISCORD_BOT_TOKEN \
      -e ENFORCE_VOICE_CHANNEL=$ENFORCE_VOICE_CHANNEL \
      -e YOUTUBE_API_KEY=$YOUTUBE_API_KEY \
      -e GEMINI_API_KEY=$GEMINI_API_KEY \
      -e GEMINI_ENABLED=$GEMINI_ENABLED \
      -e SPOTIFY_CLIENT_ID=$SPOTIFY_CLIENT_ID \
      -e SPOTIFY_CLIENT_SECRET=$SPOTIFY_CLIENT_SECRET \
      -e SPOTIFY_ENABLED=$SPOTIFY_ENABLED \
      -e IDLE_TIMEOUT_MINUTES=$IDLE_TIMEOUT_MINUTES \
      -e AUDIO_BITRATE=$AUDIO_BITRATE \
      -e SENTRY_DSN=$SENTRY_DSN \
      discord-music-bot:latest
    ```

    Recommended: at least 1GB memory with 2GB swap, since songs are buffered in memory during playback.

## Optional Features

### YouTube Playlist Support

Queue entire YouTube playlists with a single command. Just paste a YouTube playlist URL.

- Default limit: 15 videos per playlist
- Set `YOUTUBE_PLAYLIST_LIMIT` to change (max 50)

### Spotify Integration

Parses Spotify track, playlist, and album URLs and searches YouTube for the corresponding songs.

- Get credentials from [Spotify Developer Dashboard](https://developer.spotify.com/dashboard)
- Set `SPOTIFY_ENABLED=true` and configure `SPOTIFY_CLIENT_ID` and `SPOTIFY_CLIENT_SECRET`
- Set `SPOTIFY_PLAYLIST_LIMIT` to change the default playlist limit (default 10, max 50)

### Gemini AI

Generates personality-driven responses for song announcements, help messages, and idle disconnect farewells. Configured as a sassy DJ personality.

- Get an API key from [Google AI Studio](https://makersuite.google.com/app/apikey)
- Set `GEMINI_ENABLED=true` and configure `GEMINI_API_KEY`

## Development

This project was created for personal use in a private Discord server. While you're welcome to use and modify it, please note it's not maintained as a product or service.

## Acknowledgments

- [DiscordGo](https://github.com/bwmarrin/discordgo) - Go package for Discord bots (uses [MohmmedAshraf's fork](https://github.com/MohmmedAshraf/discordgo) with voice encryption fixes)
- [Opus](https://gopkg.in/hraban/opus.v2) - Go bindings for the Opus audio codec

## License

MIT License - See LICENSE file for details.
