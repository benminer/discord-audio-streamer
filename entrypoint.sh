#!/bin/bash
set -e

# Register slash commands with Discord on startup
if [ -n "$DISCORD_BOT_TOKEN" ] && [ -n "$DISCORD_APP_ID" ]; then
  echo "Registering Discord commands..."
  curl -sf -X PUT \
    -H "Authorization: Bot $DISCORD_BOT_TOKEN" \
    -H "Content-Type: application/json" \
    -d @commands.json \
    "https://discord.com/api/v10/applications/$DISCORD_APP_ID/commands" > /dev/null
  echo "Commands registered."
else
  echo "Warning: DISCORD_BOT_TOKEN or DISCORD_APP_ID not set, skipping command registration."
fi

exec ./discord-bot
