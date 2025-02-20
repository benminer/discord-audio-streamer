#!/bin/bash

source .env

gcloud run deploy discord-dj \
  --image $GOOGLE_REGISTRY_URL/discord-music-bot:latest \
  --min-instances=1 \
  --max-instances=1 \
  --platform managed \
  --region us-central1 \
  --allow-unauthenticated \
  --cpu=4 \
  --memory=4Gi \
  --set-env-vars="DISCORD_APP_ID=$DISCORD_APP_ID" \
  --set-env-vars="DISCORD_PUBLIC_KEY=$DISCORD_PUBLIC_KEY" \
  --set-env-vars="DISCORD_BOT_TOKEN=$DISCORD_BOT_TOKEN" \
  --set-env-vars="GEMINI_API_KEY=$GEMINI_API_KEY" \
  --set-env-vars="YOUTUBE_API_KEY=$YOUTUBE_API_KEY" \
  --set-env-vars="RELEASE=true"