#!/bin/bash

source .env

docker buildx build --platform linux/amd64 -t benminer/discord-music-bot ./