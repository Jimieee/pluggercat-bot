FROM ghcr.io/lavalink-devs/lavalink:4-alpine

ARG YTDLP_VERSION=2026.03.17
ARG YTDLP_RELEASE_FILE=yt-dlp_musllinux

USER root

RUN apk add --no-cache curl \
    && curl -fsSL "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/${YTDLP_RELEASE_FILE}" -o /usr/local/bin/yt-dlp-bin \
    && chmod 0755 /usr/local/bin/yt-dlp-bin \
    && printf '%s\n' \
      '#!/bin/sh' \
      'set -eu' \
      'YTDLP_BIN="/usr/local/bin/yt-dlp-bin"' \
      'COOKIES_FILE="${LAVASRC_YTDLP_COOKIES_FILE:-}"' \
      'if [ -n "$COOKIES_FILE" ] && [ -f "$COOKIES_FILE" ]; then' \
      '  exec "$YTDLP_BIN" --cookies "$COOKIES_FILE" "$@"' \
      'fi' \
      'exec "$YTDLP_BIN" "$@"' \
      > /usr/local/bin/yt-dlp \
    && chmod 0755 /usr/local/bin/yt-dlp \
    && apk del curl

USER lavalink
