#!/bin/bash

# Optimized for Mac Mini ARM64: higher resources, ARM64 image, consistent naming

source .env

should_rebuild=false
if [ "$1" == "rebuild" ]; then
    should_rebuild=true
    echo "Rebuilding Docker image for ARM64..."
    docker build --platform linux/arm64 -t benminer/discord-audio-streamer:latest ./
fi

# Remove existing container
if docker ps -a --format '{{.Names}}' | grep -q "^discord-audio-streamer$"; then
    echo "Removing existing container..."
    docker rm -f discord-audio-streamer
fi

PORT_MAPPING=""
if [ "${TUNNEL_PROVIDER:-ngrok}" = "cloudflare" ]; then
    PORT_MAPPING="-p 8080:8080"
fi

# Run with high resources for Mac Mini (adjust as needed: M2 Pro 12c/32GB example)
docker run -d --name discord-audio-streamer \
  --restart always \
  $PORT_MAPPING \
  --cpus=8 \
  --memory=16g \
  --memory-swap=24g \
  -v discord-audio-streamer-data:/app/data \
  -e DISCORD_APP_ID=$DISCORD_APP_ID \
  -e DISCORD_PUBLIC_KEY=$DISCORD_PUBLIC_KEY \
  -e DISCORD_BOT_TOKEN=$DISCORD_BOT_TOKEN \
  -e ENFORCE_VOICE_CHANNEL=$ENFORCE_VOICE_CHANNEL \
  -e YOUTUBE_API_KEY=$YOUTUBE_API_KEY \
  -e SPOTIFY_CLIENT_ID=$SPOTIFY_CLIENT_ID \
  -e SPOTIFY_CLIENT_SECRET=$SPOTIFY_CLIENT_SECRET \
  -e SPOTIFY_ENABLED=true \
  -e GEMINI_API_KEY=$GEMINI_API_KEY \
  -e GEMINI_ENABLED=$GEMINI_ENABLED \
  -e NGROK_AUTHTOKEN=$NGROK_AUTHTOKEN \
  -e NGROK_DOMAIN=$NGROK_DOMAIN \
  -e TUNNEL_PROVIDER=${TUNNEL_PROVIDER:-ngrok} \
  -e CLOUDFLARE_TUNNEL_URL=$CLOUDFLARE_TUNNEL_URL \
  -e SENTRY_DSN=$SENTRY_DSN \
  benminer/discord-audio-streamer:latest