#!/bin/bash
# Start beatbot (discord-audio-streamer) + Cloudflare tunnel in a tmux session
# Usage: ./start-beatbot.sh

SESSION="beatbot"
BOT_DIR="/Users/ben/discord-apps/discord-audio-streamer"
BOT_BIN="$BOT_DIR/beatbot"

echo "=== Stopping existing beatbot instances ==="

# Kill any running beatbot processes
if pgrep -f "$BOT_BIN" > /dev/null; then
    echo "Killing beatbot process..."
    pkill -f "$BOT_BIN"
    sleep 1
fi

# Kill any running cloudflared tunnel for beatbot
if pgrep -f "cloudflared tunnel run beatbot" > /dev/null; then
    echo "Killing cloudflared tunnel..."
    pkill -f "cloudflared tunnel run beatbot"
    sleep 1
fi

# Kill existing tmux session if it exists
if tmux has-session -t "$SESSION" 2>/dev/null; then
    echo "Killing tmux session '$SESSION'..."
    tmux kill-session -t "$SESSION"
fi

echo "=== Starting fresh ==="

# Create tmux session with beatbot in first window
tmux new-session -d -s "$SESSION" -n bot -c "$BOT_DIR"
tmux send-keys -t "$SESSION:bot" "TUNNEL_PROVIDER=cloudflare CLOUDFLARE_TUNNEL_URL=https://beatbot.bensserver.com ./beatbot" Enter

# Create second window for cloudflare tunnel
tmux new-window -t "$SESSION" -n tunnel
tmux send-keys -t "$SESSION:tunnel" "cloudflared tunnel --config ~/.cloudflared/beatbot.yml run beatbot" Enter

echo "=== Done ==="
echo "Bot URL:    https://beatbot.bensserver.com"
echo "tmux:       $SESSION (windows: bot, tunnel)"
echo ""
echo "Attach with: tmux attach -t $SESSION"
