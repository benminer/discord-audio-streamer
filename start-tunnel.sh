#!/bin/bash
# Ensure the beatbot Cloudflare tunnel is running under launchd.
#
# The tunnel runs as the LaunchAgent com.ben.cloudflared-beatbot (KeepAlive +
# RunAtLoad), so it survives reboots and crashes. Its definition lives in
# deploy/com.ben.cloudflared-beatbot.plist; it is copied into
# ~/Library/LaunchAgents only when missing, so after editing it there, re-copy
# it and run `launchctl bootout gui/$(id -u)/com.ben.cloudflared-beatbot`
# before running this script again.
#
# Usage: ./start-tunnel.sh            # load if not loaded, otherwise leave alone
#        ./start-tunnel.sh restart    # force a restart of the tunnel process
set -euo pipefail

LABEL="com.ben.cloudflared-beatbot"
SRC="$(cd "$(dirname "$0")" && pwd)/deploy/$LABEL.plist"
DEST="$HOME/Library/LaunchAgents/$LABEL.plist"
DOMAIN="gui/$(id -u)"

if [[ ! -f "$DEST" ]]; then
    echo "Installing $DEST"
    cp "$SRC" "$DEST"
fi

if launchctl print "$DOMAIN/$LABEL" > /dev/null 2>&1; then
    if [[ "${1:-}" == "restart" ]]; then
        echo "Restarting cloudflared beatbot tunnel ($LABEL)..."
        launchctl kickstart -k "$DOMAIN/$LABEL"
    else
        echo "Cloudflared beatbot tunnel already running under launchd ($LABEL)"
    fi
else
    echo "Loading cloudflared beatbot tunnel ($LABEL)..."
    launchctl bootstrap "$DOMAIN" "$DEST"
fi

echo "Public URL: https://beatbot.bensserver.com"
echo "Tunnel log: ~/.cloudflared/beatbot-tunnel.log"
