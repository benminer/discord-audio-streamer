# 🎵 Discord Music Bot

A lightweight Discord bot for streaming audio to voice channels in private servers. This is a personal project built for fun and learning - not intended as a commercial product or service.

## 🔍 About

A simple bot implementation for playing audio in Discord voice channels using [yt-dlp](https://github.com/yt-dlp/yt-dlp).

## 🛠️ Prerequisites

1. **Go** - Go programming language runtime

   - Download from [golang.org](https://golang.org/dl/)
   - Verify installation with: `go version`

2. **yt-dlp** - Media download utility

   - Install from [yt-dlp/yt-dlp](https://github.com/yt-dlp/yt-dlp)
   - Verify installation with: `yt-dlp --version`

3. **ngrok** - Tunneling service for local development or self-hosting

   - Sign up at [ngrok.com](https://ngrok.com)
   - Set up your authtoken and reserved domain
   - Required environment variables:
     - `NGROK_AUTHTOKEN`: Your ngrok authentication token
     - `NGROK_DOMAIN`: Your reserved ngrok domain (this isn't required, but it'll change every time you restart)
   - Documentation: [ngrok docs](https://ngrok.com/docs)

4. **Docker** - Containerization tool

   - Install from [Docker](https://docs.docker.com/get-docker/)
   - Verify installation with: `docker --version`

5. **Gemini** - Optional AI integration for song requests. Used to make responses more natural and human-like. It's set to caveman mode by default lol

   - Get an API key from [Google AI Studio](https://makersuite.google.com/app/apikey)
   - Required environment variables:
     - `GEMINI_API_KEY`: Your Gemini API key
     - `GEMINI_ENABLED`: Set to "true" to enable Gemini integration, or "false" to disable it.

## Discord

1. Create a new application in the Discord Developer Portal

   - Documentation: [Discord Developer Portal](https://discord.com/developers/applications)

2. Create a bot for your application

   - Documentation: [Discord Bot](https://discord.com/developers/docs/interactions/application-commands#registering-a-command)

## 🚀 Setup

1. Clone this repository:

   ```bash
   git clone https://github.com/benminer/discord-audio-streamer.git
   cd discord-audio-streamer
   ```

2. Set up your environment variables:

   ```bash
   cp common.env .env
   ```

3. Configure your `.env` file with the required parameters found in `common.env`

## 🔨 Build & Run

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

## 🐳 Docker

1.  Build the Docker image:

    ```bash
    docker build -t discord-music-bot:latest ./
    ```

    **Note for Apple Silicon users:** If you are building the Docker image on an Apple Silicon Mac, you may need to use `docker buildx` to build an image that is compatible with your architecture. For example:

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
      -e NGROK_AUTHTOKEN=$NGROK_AUTHTOKEN \
      -e NGROK_DOMAIN=$NGROK_DOMAIN \
      -e SENTRY_DSN=$SENTRY_DSN \
      discord-music-bot:latest
    ```

    I recommend setting at least 1GB, with 2GB of swap, since songs are stored in memory while streaming.

## 💻 Development

This project was created for personal use in a private Discord server. While you're welcome to use and modify it, please note it's not maintained as a product or service.

## 🙏 Acknowledgments

This project makes use of several excellent open-source libraries that I couldn't have built this without.

- [DiscordGo](https://github.com/bwmarrin/discordgo) - A powerful Go package for creating Discord bots
- [Opus](https://gopkg.in/hraban/opus.v2) - Go bindings for the Opus audio codec

## 📝 License

MIT License - See LICENSE file for details.
