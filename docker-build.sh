#!/bin/bash
# Build ARM64 image, push to Docker Hub (login required)
docker build --platform linux/arm64 -t benminer/discord-audio-streamer:latest ./
# docker push benminer/discord-audio-streamer:latest