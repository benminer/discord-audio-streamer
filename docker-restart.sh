#!/bin/bash

# This script is used to restart the docker container
# It will build the docker image and run it
# It will also remove the existing container if it exists
# It will also set the environment variables from the .env file

source .env

should_rebuild=false

if [ "$1" == "rebuild" ]; then
    should_rebuild=true
    echo "Rebuilding docker image"
fi

if [ "$should_rebuild" == "true" ]; then
    docker build -t benminer/discord-music-bot ./
fi

docker compose down
docker compose up -d