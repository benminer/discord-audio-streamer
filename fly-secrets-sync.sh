#!/bin/bash
# Syncs local .env secrets to fly.io

set -e

if [ ! -f .env ]; then
    echo "Error: .env file not found"
    exit 1
fi

source .env

echo "Syncing secrets to fly.io..."

fly secrets set \
    DISCORD_BOT_TOKEN="$DISCORD_BOT_TOKEN" \
    DISCORD_APP_ID="$DISCORD_APP_ID" \
    DISCORD_PUBLIC_KEY="$DISCORD_PUBLIC_KEY" \
    YOUTUBE_API_KEY="$YOUTUBE_API_KEY" \
    SPOTIFY_CLIENT_ID="$SPOTIFY_CLIENT_ID" \
    SPOTIFY_CLIENT_SECRET="$SPOTIFY_CLIENT_SECRET" \
    SPOTIFY_ENABLED="$SPOTIFY_ENABLED" \
    GEMINI_API_KEY="$GEMINI_API_KEY" \
    GEMINI_ENABLED="$GEMINI_ENABLED" \
    SENTRY_DSN="$SENTRY_DSN"

echo "Secrets synced successfully"
