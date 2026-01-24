#!/bin/bash

# This script is used to restart the docker container
# It will build the docker image and run it
# It will also remove the existing container if it exists
# It will also set the environment variables from the .env file

source .env

should_rebuild=false

if [ "$1" == "rebuild" ]; then
    should_rebuild=true
    echo "Rebuilding docker image"
fi

if [ "$should_rebuild" == "true" ]; then
    docker build -t benminer/discord-music-bot ./
fi

if docker ps -a --format '{{.Names}}' | grep -q "^discord-music-bot$"; then
    echo "Found existing container, removing..."
    docker rm -f discord-music-bot
else
    echo "No existing container found"
fi

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
  -e SPOTIFY_CLIENT_ID=$SPOTIFY_CLIENT_ID \
  -e SPOTIFY_CLIENT_SECRET=$SPOTIFY_CLIENT_SECRET \
  -e SPOTIFY_ENABLED=true \
  -e GEMINI_API_KEY=$GEMINI_API_KEY \
  -e GEMINI_ENABLED=$GEMINI_ENABLED \
  -e NGROK_AUTHTOKEN=$NGROK_AUTHTOKEN \
  -e NGROK_DOMAIN=$NGROK_DOMAIN \
  -e SENTRY_DSN=$SENTRY_DSN \
  benminer/discord-music-bot:latest
