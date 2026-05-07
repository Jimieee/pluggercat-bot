# Pluggercat

This bot is written in Go.
It uses Lavalink as its audio backend and `LavaSrc` for extra source resolution.

## Current features

- `/play` supports autocomplete, direct track links, and full playlist or album loading
- `/search` shows paginated search results with native Discord buttons and a select menu
- `/queue` is paginated and supports native queue actions to play now, move, remove, shuffle, clear upcoming, repeat modes, and remove ranges
- the bot auto-advances tracks, auto-leaves on idle queue, and auto-leaves when left alone in voice

## Supported playback sources

- YouTube through a configurable backend:
  - `LavaSrc + yt-dlp` for direct playback, recommended on VPS
  - `youtube-source` as an optional alternative when you want to experiment with OAuth / poToken / remote cipher
- SoundCloud through Lavalink
- Spotify links through `LavaSrc` when Spotify is enabled
- Apple Music links through `LavaSrc` when Apple Music is enabled
- Deezer links through `LavaSrc` when Deezer is enabled

## Source configuration

`LavaSrc` is already wired into `docker-compose.yml` and `deploy/lavalink/application.yml`, but each provider stays disabled until you enable it with environment variables.

### Spotify

Set these values in `.env` and rebuild Lavalink:

```env
LAVASRC_ENABLE_SPOTIFY=true
LAVASRC_SPOTIFY_CLIENT_ID=your_spotify_client_id
LAVASRC_SPOTIFY_CLIENT_SECRET=your_spotify_client_secret
```

Spotify playback is mirrored. In practice that means `LavaSrc` resolves Spotify metadata and then uses the configured providers, which in this project default to `ytsearch`.

### Apple Music

Set these values in `.env` and rebuild Lavalink:

```env
LAVASRC_ENABLE_APPLE_MUSIC=true
LAVASRC_APPLE_MUSIC_MEDIA_API_TOKEN=your_apple_music_token
```

Apple Music playback is also mirrored through the configured fallback providers.

### Deezer

Set these values in `.env` and rebuild Lavalink:

```env
LAVASRC_ENABLE_DEEZER=true
LAVASRC_DEEZER_MASTER_DECRYPTION_KEY=your_deezer_master_key
LAVASRC_DEEZER_ARL=your_deezer_arl_cookie
```

Deezer is the direct source in this stack. If you do not provide both credentials, Deezer links should remain disabled.

## YouTube strategy

As of `2026-05-06`, the more robust VPS-friendly path in this repository is `LavaSrc + yt-dlp`.
The `lavalink` service is now built from [deploy/lavalink/Dockerfile](/home/jimie/code/go/pluggercat/deploy/lavalink/Dockerfile:1), which pins a `yt-dlp` binary inside the container so the `LavaSrc` `ytdlp` source can handle YouTube URLs, playlists and searches directly.
Because the Lavalink image is Alpine-based, the default release file here is `yt-dlp_musllinux`, not `yt-dlp_linux`.
On many VPS/datacenter IPs, direct YouTube playback still needs cookies even with `yt-dlp`. This repository now mounts [deploy/lavalink/secrets](/home/jimie/code/go/pluggercat/deploy/lavalink/secrets/.gitignore:1) into the container and the wrapper at [deploy/lavalink/Dockerfile](/home/jimie/code/go/pluggercat/deploy/lavalink/Dockerfile:1) automatically adds `--cookies` when `LAVASRC_YTDLP_COOKIES_FILE` points to an existing file.

Recommended `.env` values:

```env
YOUTUBE_BACKEND=ytdlp
YOUTUBE_SOURCE_ENABLED=false
LAVASRC_ENABLE_YTDLP=true
LAVASRC_YTDLP_VERSION=2026.03.17
LAVASRC_YTDLP_RELEASE_FILE=yt-dlp_musllinux
LAVASRC_YTDLP_COOKIES_FILE=/opt/Lavalink/secrets/youtube-cookies.txt
```

If direct YouTube URLs still fail on a VPS with a `Sign in to confirm you're not a bot` error, export a Netscape-format cookies file from a logged-in YouTube browser session and place it at:

```text
deploy/lavalink/secrets/youtube-cookies.txt
```

Then rebuild Lavalink:

```bash
docker compose up -d --build --force-recreate lavalink
```

If you want to try `youtube-source` anyway, keep these expectations in mind:

- OAuth, poToken and remote cipher are still optional integrations, not guaranteed fixes
- `TV` is the OAuth-compatible playback client in `youtube-source`
- YouTube player and cipher changes can still break playback on datacenter IPs

Relevant optional `youtube-source` values:

```env
YOUTUBE_SOURCE_ENABLED=true
YOUTUBE_BACKEND=youtube-source
YOUTUBE_OAUTH_ENABLED=true
YOUTUBE_OAUTH_REFRESH_TOKEN=your_refresh_token
YOUTUBE_REMOTE_CIPHER_URL=http://yt-cipher:8001
YOUTUBE_REMOTE_CIPHER_PASSWORD=pluggercat-yt-cipher
YOUTUBE_REMOTE_CIPHER_USER_AGENT=pluggercat
```

After changing YouTube strategy or source settings, rebuild and recreate Lavalink:

```bash
docker compose up -d --build --force-recreate lavalink
```
