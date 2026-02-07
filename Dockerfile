# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build with optimizations for ARM64: strip debug info and symbols
ARG TARGETPLATFORM
RUN CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o discord-bot

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    libopusfile0 \
    libopus0 \
    ffmpeg \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux_aarch64 -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

# Create non-root user
RUN useradd -m -u 1000 -s /bin/bash appuser

WORKDIR /app

# Copy artifacts
COPY --from=builder /app/discord-bot .
COPY --from=builder /app/commands.json .
COPY --from=builder /app/entrypoint.sh .

# Data dir
RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser

ENV RELEASE=true \
    PORT=8080 \
    GIN_MODE=release \
    ENFORCE_VOICE_CHANNEL="true" \
    GEMINI_ENABLED="true" \
    SENTRY_ENVIRONMENT="production"

HEALTHCHECK --interval=30s --timeout=30s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

VOLUME /app/data
EXPOSE 8080
CMD ["./entrypoint.sh"]