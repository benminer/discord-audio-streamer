#!/bin/bash

source .env

docker build -t benminer/discord-music-bot ./
docker rm -f benminer/discord-music-bot 
docker run -d --name benminer/discord-music-bot \
  --restart always \
  --memory="1g" \
  --memory-reservation="512m" \
  --memory-swap="2g" \
  --cpus="2" \           
  --cpu-shares="2048" \  
  --memory-swappiness="20" \
  -e DISCORD_APP_ID=$DISCORD_APP_ID \
  -e DISCORD_PUBLIC_KEY=$DISCORD_PUBLIC_KEY \
  -e DISCORD_BOT_TOKEN=$DISCORD_BOT_TOKEN \
  -e ENFORCE_VOICE_CHANNEL=$ENFORCE_VOICE_CHANNEL \
  -e YOUTUBE_API_KEY=$YOUTUBE_API_KEY \
  -e GEMINI_API_KEY=$GEMINI_API_KEY \
  -e GEMINI_ENABLED=$GEMINI_ENABLED \
  -e NGROK_AUTHTOKEN=$NGROK_AUTHTOKEN \
  -e NGROK_DOMAIN=$NGROK_DOMAIN \
  benminer/discord-music-bot:latest