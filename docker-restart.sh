#!/bin/bash

# Optimized for Mac Mini ARM64: higher resources, ARM64 image, consistent naming
# Runs both ngrok (via app) and cloudflared in parallel during transition.
# To fully cut over to cloudflare: set TUNNEL_PROVIDER=cloudflare in .env
# and update the Discord webhook URL to https://beatbot.bensserver.com

source .env

should_rebuild=false
if [ "$1" == "rebuild" ]; then
    should_rebuild=true
    echo "Rebuilding Docker image for ARM64..."
    docker build --platform linux/arm64 -t benminer/discord-audio-streamer:latest ./
fi

# --- Stop existing cloudflared tunnel ---
if pgrep -f "cloudflared tunnel run beatbot" > /dev/null; then
    echo "Stopping cloudflared beatbot tunnel..."
    pkill -f "cloudflared tunnel run beatbot"
    sleep 1
fi

# --- Remove existing container ---
if docker ps -a --format '{{.Names}}' | grep -q "^discord-audio-streamer$"; then
    echo "Removing existing container..."
    docker rm -f discord-audio-streamer
fi

# --- Start Docker container ---
# Port 8080 always exposed so cloudflared can reach it regardless of TUNNEL_PROVIDER
echo "Starting discord-audio-streamer container..."
docker run -d --name discord-audio-streamer \
  --restart always \
  -p 8080:8080 \
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

# --- Wait for container to be ready ---
echo "Waiting for bot to be ready on :8080..."
for i in $(seq 1 15); do
    if curl -s http://localhost:8080/health > /dev/null 2>&1 || curl -s http://localhost:8080 > /dev/null 2>&1; then
        echo "Bot is up!"
        break
    fi
    sleep 2
done

# --- Start cloudflared tunnel ---
echo "Starting cloudflared beatbot tunnel..."
cloudflared tunnel --config ~/.cloudflared/beatbot.yml run beatbot > ~/.openclaw/logs/cloudflared-beatbot.log 2>&1 &
echo "Cloudflare tunnel started (PID: $!)"

echo ""
echo "=== Done ==="
echo "ngrok URL:       https://${NGROK_DOMAIN} (active if TUNNEL_PROVIDER=ngrok)"
echo "Cloudflare URL:  https://beatbot.bensserver.com (always active)"
echo ""
echo "To cut over to Cloudflare fully:"
echo "  1. Update Discord webhook URL to https://beatbot.bensserver.com"
echo "  2. Set TUNNEL_PROVIDER=cloudflare in .env"
echo "  3. Re-run ./docker-restart.sh"
