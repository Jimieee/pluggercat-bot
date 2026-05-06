# Pluggercat

This bot is written in Go.
It uses Lavalink as its audio backend and `LavaSrc` for extra source resolution.

## Current features

- `/play` supports autocomplete, direct track links, and full playlist or album loading
- `/search` shows paginated search results with native Discord buttons and a select menu
- `/queue` is paginated and supports native queue actions to play now, move, remove, shuffle, clear upcoming, repeat modes, and remove ranges
- the bot auto-advances tracks, auto-leaves on idle queue, and auto-leaves when left alone in voice

## Supported playback sources

- YouTube and YouTube Music through `youtube-source`
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

Spotify playback is mirrored. In practice that means `LavaSrc` resolves Spotify metadata and then uses the configured providers, which in this project default to `ytmsearch` and `ytsearch`.

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

## YouTube on VPS

For VPS and datacenter IPs, the recommended `youtube-source` stack is:

- `TV` client for authenticated playback with OAuth
- `remoteCipher` enabled for signature deciphering

This repository now wires a `yt-cipher` service into `docker-compose.yml` and points Lavalink to it by default. That follows the current `youtube-source` guidance that YouTube cipher changes are becoming more frequent and complex, so remote ciphering is the safer path when local extraction breaks.

Relevant `.env` values:

```env
YOUTUBE_OAUTH_ENABLED=true
YOUTUBE_OAUTH_REFRESH_TOKEN=your_refresh_token
YOUTUBE_REMOTE_CIPHER_URL=http://yt-cipher:8001
YOUTUBE_REMOTE_CIPHER_PASSWORD=pluggercat-yt-cipher
YOUTUBE_REMOTE_CIPHER_USER_AGENT=pluggercat
```

After changing YouTube or source settings, recreate Lavalink:

```bash
docker compose up -d --force-recreate lavalink
```
