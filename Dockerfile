# Build stage
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    unzip \
    && rm -rf /var/lib/apt/lists/*

# Install libdave (Discord DAVE E2EE) prebuilt binary for Linux ARM64
RUN curl -fsSL https://github.com/discord/libdave/releases/download/v1.1.1/cpp/libdave-Linux-ARM64-boringssl.zip -o /tmp/libdave.zip \
    && mkdir -p /tmp/libdave \
    && unzip -j /tmp/libdave.zip "include/dave/dave.h" -d /usr/local/include \
    && unzip -j /tmp/libdave.zip "lib/libdave.so" -d /usr/local/lib \
    && rm -f /tmp/libdave.zip \
    && ldconfig \
    && mkdir -p /usr/local/lib/pkgconfig \
    && printf 'prefix=/usr/local\nexec_prefix=${prefix}\nlibdir=${exec_prefix}/lib\nincludedir=${prefix}/include\n\nName: dave\nDescription: Discord DAVE E2EE\nVersion: 1.1.1\nLibs: -L${libdir} -ldave\nCflags: -I${includedir}\n' > /usr/local/lib/pkgconfig/dave.pc

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build with optimizations for ARM64: strip debug info and symbols
ARG TARGETPLATFORM
RUN CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o discord-bot

# Runtime stage
FROM ubuntu:24.04

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

# Copy libdave shared library from builder stage
COPY --from=builder /usr/local/lib/libdave.so /usr/local/lib/libdave.so
RUN ldconfig

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