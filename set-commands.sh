#!/bin/bash

is_dev=false

if [ "$1" == "dev" ]; then
  source .env.dev
  echo "Setting commands for dev"
else
  source .env
fi

curl -s -X PUT \
  -H "Authorization: Bot $DISCORD_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d @commands.json \
  "https://discord.com/api/v10/applications/$DISCORD_APP_ID/commands"

echo "Commands set successfully"