# Build stage — use Ubuntu 24.04 for glibc 2.38+ (required by libdave)
FROM ubuntu:24.04 AS builder

ARG TARGETARCH

# Install Go toolchain + build dependencies
# golang:bookworm has glibc 2.36 which is too old for libdave's symbols
RUN apt-get update && apt-get install -y \
    curl \
    gcc \
    g++ \
    libopusfile-dev \
    libopus-dev \
    pkg-config \
    unzip \
    && rm -rf /var/lib/apt/lists/* \
    && GO_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "amd64") \
    && curl -fsSL "https://go.dev/dl/go1.25.0.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /app

# Install libdave (Discord DAVE E2EE) prebuilt binary
# Map Docker TARGETARCH (amd64/arm64) to libdave release asset names (X64/ARM64)
RUN LIBDAVE_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "ARM64" || echo "X64") \
    && curl -fsSL "https://github.com/discord/libdave/releases/download/v1.1.1/cpp/libdave-Linux-${LIBDAVE_ARCH}-boringssl.zip" -o /tmp/libdave.zip \
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

# Build with optimizations: strip debug info and symbols
RUN CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-w -s" -o discord-bot

# Runtime stage
FROM ubuntu:24.04

ARG TARGETARCH

# Install runtime dependencies
# Map TARGETARCH to yt-dlp binary name (arm64 -> yt-dlp_linux_aarch64, amd64 -> yt-dlp_linux)
RUN apt-get update && apt-get install -y \
    libopusfile0 \
    libopus0 \
    ffmpeg \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && YTDLP_SUFFIX=$([ "$TARGETARCH" = "arm64" ] && echo "_aarch64" || echo "") \
    && curl -L "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux${YTDLP_SUFFIX}" -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

# Create non-root user (use UID 1001 since ubuntu:24.04 already has UID 1000)
RUN useradd -m -u 1001 -s /bin/bash appuser

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