FROM ghcr.io/lavalink-devs/lavalink:4-alpine

ARG YTDLP_VERSION=2026.03.17
ARG YTDLP_RELEASE_FILE=yt-dlp_musllinux

USER root

RUN apk add --no-cache curl nodejs npm ffmpeg \
  && curl -fsSL "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/${YTDLP_RELEASE_FILE}" -o /usr/local/bin/yt-dlp-bin \
  && chmod 0755 /usr/local/bin/yt-dlp-bin \
  && printf '%s\n' \
  '#!/bin/sh' \
  'set -eu' \
  'YTDLP_BIN="/usr/local/bin/yt-dlp-bin"' \
  'TMP_COOKIES=""' \
  'cleanup() {' \
  '  if [ -n "${TMP_COOKIES:-}" ] && [ -f "$TMP_COOKIES" ]; then rm -f "$TMP_COOKIES"; fi' \
  '}' \
  'trap cleanup EXIT HUP INT TERM' \
  'COOKIES_FILE="${LAVASRC_YTDLP_COOKIES_FILE:-}"' \
  'if [ -n "$COOKIES_FILE" ] && [ -f "$COOKIES_FILE" ]; then' \
  '  TMP_COOKIES="$(mktemp -p /tmp yt-dlp-cookies.XXXXXX)"' \
  '  cp "$COOKIES_FILE" "$TMP_COOKIES"' \
  '  "$YTDLP_BIN" --js-runtimes node --remote-components ejs:github --cookies "$TMP_COOKIES" "$@"' \
  '  exit "$?"' \
  'fi' \
  '"$YTDLP_BIN" --js-runtimes node --remote-components ejs:github "$@"' \
  > /usr/local/bin/yt-dlp \
  && chmod 0755 /usr/local/bin/yt-dlp \
  && apk del curl

USER lavalink
