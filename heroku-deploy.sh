#!/bin/bash
source .env

# heroku config:set DISCORD_APP_ID=$DISCORD_APP_ID
# heroku config:set DISCORD_PUBLIC_KEY=$DISCORD_PUBLIC_KEY
# heroku config:set DISCORD_BOT_TOKEN=$DISCORD_BOT_TOKEN
# heroku config:set ENFORCE_VOICE_CHANNEL=true
# heroku config:set YOUTUBE_API_KEY=$YOUTUBE_API_KEY
# heroku config:set GEMINI_API_KEY=$GEMINI_API_KEY
# heroku config:set GEMINI_ENABLED=true
# heroku config:set RELEASE=true

git push heroku main