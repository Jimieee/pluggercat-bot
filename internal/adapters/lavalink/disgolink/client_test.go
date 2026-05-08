package disgolinkadapter

import (
	"reflect"
	"testing"

	"github.com/disgoorg/disgolink/v3/lavalink"

	"pluggercat/internal/config"
	domainmusic "pluggercat/internal/domain/music"
)

func TestCandidateIdentifiers(t *testing.T) {
	testCases := []struct {
		name    string
		query   string
		backend config.YouTubeBackend
		want    []string
	}{
		{
			name:    "youtube-source text search prefers ytmsearch",
			query:   "foxy itown",
			backend: config.YouTubeBackendSource,
			want:    []string{"ytmsearch:foxy itown", "ytsearch:foxy itown"},
		},
		{
			name:    "ytdlp text search uses ytsearch only",
			query:   "foxy itown",
			backend: config.YouTubeBackendYTDLP,
			want:    []string{"ytsearch:foxy itown"},
		},
		{
			name:    "ytdlp rewrites explicit ytmsearch",
			query:   "ytmsearch:foxy itown",
			backend: config.YouTubeBackendYTDLP,
			want:    []string{"ytsearch:foxy itown"},
		},
		{
			name:    "urls pass through",
			query:   "https://youtu.be/dQw4w9WgXcQ",
			backend: config.YouTubeBackendYTDLP,
			want:    []string{"https://youtu.be/dQw4w9WgXcQ"},
		},
		{
			name:    "markdown links normalize to url",
			query:   "[Never Gonna Give You Up](https://youtu.be/dQw4w9WgXcQ)",
			backend: config.YouTubeBackendYTDLP,
			want:    []string{"https://youtu.be/dQw4w9WgXcQ"},
		},
		{
			name:    "angle bracket urls normalize to url",
			query:   "<https://youtu.be/dQw4w9WgXcQ>",
			backend: config.YouTubeBackendYTDLP,
			want:    []string{"https://youtu.be/dQw4w9WgXcQ"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := candidateIdentifiers(tc.query, tc.backend)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("candidateIdentifiers(%q, %q) = %#v, want %#v", tc.query, tc.backend, got, tc.want)
			}
		})
	}
}

func TestMapLoadResultTreatsSearchPlaylistAsSearch(t *testing.T) {
	result, found, err := mapLoadResult("ytsearch:foxy song itown", lavalink.Playlist{
		Info: lavalink.PlaylistInfo{
			Name:          "Youtube Search: foxy song itown",
			SelectedTrack: -1,
		},
		Tracks: []lavalink.Track{
			{
				Encoded: "encoded-1",
				Info: lavalink.TrackInfo{
					Identifier: "track-1",
					Title:      "Track 1",
					Author:     "Artist 1",
					SourceName: "youtube",
				},
			},
			{
				Encoded: "encoded-2",
				Info: lavalink.TrackInfo{
					Identifier: "track-2",
					Title:      "Track 2",
					Author:     "Artist 2",
					SourceName: "youtube",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("mapLoadResult returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected mapped search playlist to contain results")
	}
	if result.Type != domainmusic.LoadResultTypeSearch {
		t.Fatalf("expected search playlist to map as search, got %q", result.Type)
	}
	if result.Collection != nil {
		t.Fatalf("expected search playlist collection to be ignored, got %+v", result.Collection)
	}
	if len(result.Tracks) != 2 || result.Tracks[0].ID != "track-1" || result.Tracks[1].ID != "track-2" {
		t.Fatalf("unexpected mapped tracks: %+v", result.Tracks)
	}
}
