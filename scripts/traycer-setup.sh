#!/usr/bin/env bash
# Prepares a fresh git worktree (e.g. one created by Traycer) for development.
# Safe to re-run. Invoked from .traycer/environment.json.
#
# Never touches the production container (discord-audio-streamer), the beatbot
# tmux sessions, or the cloudflared tunnel.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
MAIN="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")"
echo "==> Worktree: $PWD"
echo "==> Main checkout: $MAIN"

# Gitignored local files to carry over from the main checkout.
LOCAL_FILES=(.env .env.dev)
for f in "${LOCAL_FILES[@]}"; do
  if [[ "$MAIN" != "$PWD" && -f "$MAIN/$f" && ! -e "$f" ]]; then
    cp "$MAIN/$f" "$f"
    echo "==> Copied $f from main checkout"
  fi
done

mkdir -p data

# libdave (cgo, via pkg-config) may be installed under ~/.local rather than a
# default pkg-config location.
if [[ -f "$HOME/.local/lib/pkgconfig/dave.pc" ]]; then
  export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
fi

echo "==> Downloading Go modules ($(go version | awk '{print $3}'), go.mod wants $(awk '/^go /{print $2}' go.mod))"
go mod download

echo "==> go vet"
go vet ./...

if pkg-config --exists dave; then
  echo "==> Building"
  go build -o /dev/null .
  echo "==> Worktree ready"
else
  echo "!! libdave not found by pkg-config; skipped go build. Install libdave or use ./docker-build.sh."
fi

command -v yt-dlp >/dev/null || echo "!! yt-dlp is not on PATH; playback will fail when running the bot natively."
