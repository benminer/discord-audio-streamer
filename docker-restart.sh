#!/bin/bash

# Optimized for Mac Mini ARM64: higher resources, ARM64 image, consistent naming

source .env

should_rebuild=false
if [ &quot;$1&quot; == &quot;rebuild&quot; ]; then
    should_rebuild=true
    echo &quot;Rebuilding Docker image for ARM64...&quot;
    docker build --platform linux/arm64 -t benminer/discord-audio-streamer:latest ./
fi

# --- Stop existing cloudflared tunnel ---
if pgrep -f &quot;cloudflared tunnel run beatbot&quot; &gt; /dev/null; then
    echo &quot;Stopping cloudflared beatbot tunnel...&quot;
    pkill -f &quot;cloudflared tunnel run beatbot&quot;
    sleep 1
fi

# --- Remove existing container ---
if docker ps -a --format '{{.Names}}' | grep -q &quot;^discord-audio-streamer$&quot;; then
    echo &quot;Removing existing container...&quot;
    docker rm -f discord-audio-streamer
fi

# --- Start Docker container ---
# Port 8080 always exposed so cloudflared can reach it
echo &quot;Starting discord-audio-streamer container...&quot;
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
  -e CLOUDFLARE_TUNNEL_URL=$CLOUDFLARE_TUNNEL_URL \
  -e SENTRY_DSN=$SENTRY_DSN \
  benminer/discord-audio-streamer:latest

# --- Wait for container to be ready ---
echo &quot;Waiting for bot to be ready on :8080...&quot;
for i in $(seq 1 15); do
    if curl -s http://localhost:8080/health &gt; /dev/null 2&gt;&amp;1 || curl -s http://localhost:8080 &gt; /dev/null 2&gt;&amp;1; then
        echo &quot;Bot is up!&quot;
        break
    fi
    sleep 2
done

# --- Start cloudflared tunnel ---
echo &quot;Starting cloudflared beatbot tunnel...&quot;
cloudflared tunnel --config ~/.cloudflared/beatbot.yml run beatbot &gt; ~/.openclaw/logs/cloudflared-beatbot.log 2&gt;&amp;1 &amp;
echo &quot;Cloudflare tunnel started (PID: $!)&quot;

echo &quot;&quot;
echo &quot;=== Done ===&quot;
echo &quot;URL: https://beatbot.bensserver.com&quot;
