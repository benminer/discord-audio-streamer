#!/bin/bash
# Test script for Gemini-powered radio feature
# Usage: ./test-gemini-radio.sh

set -e

echo "🎵 Gemini Radio Feature Test Script"
echo "===================================="
echo ""

# Check if we're in the right directory
if [ ! -f "main.go" ]; then
    echo "❌ Error: Not in discord-audio-streamer directory"
    exit 1
fi

# Check Docker container status
echo "📦 Checking Docker container..."
if docker ps | grep -q discord-music-bot; then
    echo "✅ Container is running"
    CONTAINER_ID=$(docker ps | grep discord-music-bot | awk '{print $1}')
else
    echo "❌ Container is not running"
    exit 1
fi

# Check environment variables
echo ""
echo "🔧 Checking configuration..."
docker exec $CONTAINER_ID printenv | grep -E "(GEMINI_ENABLED|GEMINI_API_KEY)" > /tmp/gemini-config.txt 2>&1 || true

if grep -q "GEMINI_ENABLED=true" /tmp/gemini-config.txt; then
    echo "✅ GEMINI_ENABLED=true"
else
    echo "⚠️  Warning: GEMINI_ENABLED not set to true"
fi

if grep -q "GEMINI_API_KEY" /tmp/gemini-config.txt; then
    echo "✅ GEMINI_API_KEY is set"
else
    echo "❌ GEMINI_API_KEY is missing"
    exit 1
fi

# Check recent logs
echo ""
echo "📋 Recent logs (last 20 lines)..."
docker logs --tail 20 $CONTAINER_ID

echo ""
echo "🔍 Searching for Gemini-related activity in logs..."
if docker logs --tail 1000 $CONTAINER_ID 2>&1 | grep -i "gemini" > /tmp/gemini-logs.txt; then
    echo "✅ Found Gemini activity:"
    tail -5 /tmp/gemini-logs.txt
else
    echo "ℹ️  No Gemini activity yet (this is normal if radio hasn't triggered)"
fi

# Check for radio activity
echo ""
echo "📻 Searching for radio activity in logs..."
if docker logs --tail 1000 $CONTAINER_ID 2>&1 | grep -i "radio" > /tmp/radio-logs.txt; then
    echo "✅ Found radio activity:"
    tail -5 /tmp/radio-logs.txt
else
    echo "ℹ️  No radio activity yet"
fi

echo ""
echo "✅ Test script complete!"
echo ""
echo "📝 Manual Testing Steps:"
echo "  1. Open Discord and join a voice channel"
echo "  2. Run: /radio (to enable radio mode)"
echo "  3. Run: /play <song1>"
echo "  4. Run: /play <song2>"
echo "  5. Run: /play <song3>"
echo "  6. Wait for songs to finish and queue to empty"
echo "  7. Observe: Radio should auto-queue a similar song"
echo "  8. Check logs: docker logs discord-music-bot | grep -i gemini"
echo ""
echo "Expected log output:"
echo "  'Requesting Gemini song recommendation based on recent history'"
echo "  'Gemini recommended search query: <artist> - <song>'"
echo "  'Radio queuing: <song title>'"
echo ""

# Cleanup
rm -f /tmp/gemini-config.txt /tmp/gemini-logs.txt /tmp/radio-logs.txt
