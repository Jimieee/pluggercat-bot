package musicapp

import (
	"context"
	"testing"
	"time"

	"pluggercat/internal/adapters/store/memory"
	domainmusic "pluggercat/internal/domain/music"
)

func TestAutocompleteSkipsShortBlindQueries(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	suggestions, err := service.Autocomplete(context.Background(), "user-1", "as", 10)
	if err != nil {
		t.Fatalf("autocomplete returned error: %v", err)
	}
	if len(suggestions) != 0 {
		t.Fatalf("expected no suggestions for short query, got %d", len(suggestions))
	}
	if backend.searchCalls != 0 {
		t.Fatalf("expected no backend search calls, got %d", backend.searchCalls)
	}
}

func TestAutocompleteSelectionEnqueuesExactTrack(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{
				ID:        "track-1",
				Title:     "Never Gonna Give You Up",
				Author:    "Rick Astley",
				Duration:  212 * time.Second,
				Reference: "encoded-1",
				Source:    "youtube",
				URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	suggestions, err := service.Autocomplete(context.Background(), "user-1", "never gonna", 10)
	if err != nil {
		t.Fatalf("autocomplete returned error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	if !service.search.IsSelectionToken(suggestions[0].Value) {
		t.Fatalf("expected autocomplete to return a selection token, got %q", suggestions[0].Value)
	}

	backend.searchResults = nil

	track, started, err := service.Enqueue(context.Background(), "guild-1", suggestions[0].Value)
	if err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}
	if !started {
		t.Fatalf("expected playback to start from selected suggestion")
	}
	if track.ID != "track-1" {
		t.Fatalf("unexpected track id: %s", track.ID)
	}
	if backend.searchCalls != 1 {
		t.Fatalf("expected enqueue to reuse selected track without another search, got %d searches", backend.searchCalls)
	}
	if len(backend.played) != 1 || backend.played[0].ID != "track-1" {
		t.Fatalf("expected selected track to be played, got %#v", backend.played)
	}
}

func TestEnqueueReturnsSelectionExpiredForUnknownToken(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	_, _, err := service.Enqueue(context.Background(), "guild-1", "pcat:deadbeef")
	if err != ErrAutocompleteSelectionExpired {
		t.Fatalf("expected ErrAutocompleteSelectionExpired, got %v", err)
	}
	if backend.searchCalls != 0 {
		t.Fatalf("expected no backend search for invalid token, got %d", backend.searchCalls)
	}
}
