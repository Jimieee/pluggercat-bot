package disgolinkadapter

import (
	"reflect"
	"testing"

	"pluggercat/internal/config"
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
