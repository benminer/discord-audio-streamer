FROM golang:1.23.6-bullseye AS builder

WORKDIR /app

COPY files/ /app/files/

RUN apt-get update && apt-get install -y \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o discord-bot

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    ffmpeg \
    python3 \
    python3-pip \
    curl \
    && rm -rf /var/lib/apt/lists/*

RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

WORKDIR /app

COPY --from=builder /app/discord-bot .
COPY --from=builder /app/files/ /app/files/

ENV RELEASE=true
ENV PORT=8080
ENV GIN_MODE=release
ENV ENFORCE_VOICE_CHANNEL="true"
ENV GEMINI_ENABLED="true"

HEALTHCHECK --interval=30s --timeout=30s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1

CMD ["./discord-bot"]