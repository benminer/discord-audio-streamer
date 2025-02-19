#!/bin/bash

source .env

# Send the PUT request (changed from POST to PUT)
curl -X PUT \
  -H "Authorization: Bot $DISCORD_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d @commands.json \
  "https://discord.com/api/v10/applications/$DISCORD_APP_ID/commands"

echo "Commands set successfully"