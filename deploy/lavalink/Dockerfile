FROM ghcr.io/lavalink-devs/lavalink:4-alpine

ARG YTDLP_VERSION=2026.03.17

USER root

RUN apk add --no-cache curl \
    && curl -fsSL "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_linux" -o /usr/local/bin/yt-dlp \
    && chmod 0755 /usr/local/bin/yt-dlp \
    && apk del curl

USER lavalink
