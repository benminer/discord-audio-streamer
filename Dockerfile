# Build stage
FROM golang:1.23.6 AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o discord-bot

# Runtime stage
FROM ubuntu:22.04

# Prevent timezone prompt during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    ffmpeg \
    python3 \
    python3-pip \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install yt-dlp
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/discord-bot .

# Set non-sensitive environment variables
ENV RELEASE=true
ENV PORT=8080
ENV GIN_MODE=release
ENV ENFORCE_VOICE_CHANNEL="true"

# Run the bot
CMD ["./discord-bot"]