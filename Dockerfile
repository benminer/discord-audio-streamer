# Build stage
FROM golang:1.24.0-bullseye AS builder

WORKDIR /app

# Install build dependencies (cached until packages change)
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

# Build with optimizations (strip debug info, reduce binary size)
RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o discord-bot

# Runtime stage - using debian-slim instead of ubuntu for smaller size
FROM debian:bullseye-slim

# Install runtime dependencies in single layer
RUN apt-get update && apt-get install -y \
    libopusfile0 \
    libopus0 \
    ffmpeg \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

# Create non-root user for security
RUN useradd -m -u 1000 -s /bin/bash appuser

WORKDIR /app

# Copy binary and startup files from builder
COPY --from=builder /app/discord-bot .
COPY --from=builder /app/commands.json .
COPY --from=builder /app/entrypoint.sh .

# Create data directory for SQLite persistence
RUN mkdir -p /app/data

# Set ownership
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Environment variables
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
