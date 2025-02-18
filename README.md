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

3. **ngrok** - Tunneling service for local development
   - Sign up at [ngrok.com](https://ngrok.com)
   - Set up your authtoken and reserved domain
   - Required environment variables:
     - `NGROK_AUTHTOKEN`: Your ngrok authentication token
     - `NGROK_DOMAIN`: Your reserved ngrok domain
   - Documentation: [ngrok docs](https://ngrok.com/docs)

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

## 💻 Development

This project was created for personal use in a private Discord server. While you're welcome to use and modify it, please note it's not maintained as a product or service.

## 📝 License

MIT License - See LICENSE file for details.
