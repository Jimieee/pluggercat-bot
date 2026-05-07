package config

import "testing"

func TestLoadDefaultsToYTDLPBackend(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("LAVALINK_ADDRESS", "localhost:2333")
	t.Setenv("LAVALINK_PASSWORD", "pass")
	t.Setenv("YOUTUBE_BACKEND", "")
	t.Setenv("YOUTUBE_SOURCE_ENABLED", "")
	t.Setenv("LAVASRC_ENABLE_YTDLP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Lavalink.YouTubeBackend != YouTubeBackendYTDLP {
		t.Fatalf("unexpected youtube backend: %q", cfg.Lavalink.YouTubeBackend)
	}
	if !cfg.Lavalink.YTDLPEnabled {
		t.Fatal("expected yt-dlp to be enabled by default")
	}
}

func TestLoadRejectsDisabledBackendDependencies(t *testing.T) {
	testCases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "youtube-source disabled for youtube-source backend",
			env: map[string]string{
				"YOUTUBE_BACKEND":        string(YouTubeBackendSource),
				"YOUTUBE_SOURCE_ENABLED": "false",
			},
			wantErr: `YOUTUBE_BACKEND="youtube-source" requires YOUTUBE_SOURCE_ENABLED=true`,
		},
		{
			name: "ytdlp disabled for ytdlp backend",
			env: map[string]string{
				"YOUTUBE_BACKEND":      string(YouTubeBackendYTDLP),
				"LAVASRC_ENABLE_YTDLP": "false",
			},
			wantErr: `YOUTUBE_BACKEND="ytdlp" requires LAVASRC_ENABLE_YTDLP=true`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DISCORD_TOKEN", "token")
			t.Setenv("LAVALINK_ADDRESS", "localhost:2333")
			t.Setenv("LAVALINK_PASSWORD", "pass")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load to fail")
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadRejectsUnknownYouTubeBackend(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("LAVALINK_ADDRESS", "localhost:2333")
	t.Setenv("LAVALINK_PASSWORD", "pass")
	t.Setenv("YOUTUBE_BACKEND", "mystery")

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load to fail")
	}
	if err.Error() != `YOUTUBE_BACKEND must be "youtube-source" or "ytdlp"` {
		t.Fatalf("unexpected error: %v", err)
	}
}
