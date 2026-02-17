#!/bin/bash
# Start the Cloudflare tunnel for beatbot (runs on host, forwards to Docker container on :8080)
# Run this AFTER docker-restart.sh

SESSION="beatbot-tunnel"

if tmux has-session -t "$SESSION" 2>/dev/null; then
    echo "Killing existing tunnel session..."
    tmux kill-session -t "$SESSION"
fi

tmux new-session -d -s "$SESSION" -n tunnel
tmux send-keys -t "$SESSION:tunnel" "cloudflared tunnel --config ~/.cloudflared/beatbot.yml run beatbot" Enter

echo "Cloudflare tunnel started in tmux session: $SESSION"
echo "Public URL: https://beatbot.bensserver.com"
echo "Attach with: tmux attach -t $SESSION"
