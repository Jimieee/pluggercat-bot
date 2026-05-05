package musicapp

import (
	"context"
	"testing"
	"time"

	"pluggercat/internal/adapters/store/memory"
	domainmusic "pluggercat/internal/domain/music"
)

type fakeAudioBackend struct {
	searchResults []domainmusic.Track
	played        []domainmusic.Track
	paused        int
	resumed       int
	stopped       int
}

func (f *fakeAudioBackend) Connect(context.Context) error {
	return nil
}

func (f *fakeAudioBackend) Close(context.Context) error {
	return nil
}

func (f *fakeAudioBackend) SearchTracks(context.Context, string) ([]domainmusic.Track, error) {
	return f.searchResults, nil
}

func (f *fakeAudioBackend) PlayTrack(_ context.Context, _ string, track domainmusic.Track) error {
	f.played = append(f.played, track)
	return nil
}

func (f *fakeAudioBackend) Pause(context.Context, string) error {
	f.paused++
	return nil
}

func (f *fakeAudioBackend) Resume(context.Context, string) error {
	f.resumed++
	return nil
}

func (f *fakeAudioBackend) Stop(context.Context, string) error {
	f.stopped++
	return nil
}

func (f *fakeAudioBackend) ForwardVoiceState(context.Context, string, *string, string) error {
	return nil
}

func (f *fakeAudioBackend) ForwardVoiceServer(context.Context, string, string, string) error {
	return nil
}

func TestEnqueueStartsFirstTrack(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{
				ID:        "track-1",
				Title:     "Track 1",
				Author:    "Artist",
				Duration:  3 * time.Minute,
				Reference: "encoded-1",
			},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	track, started, err := service.Enqueue(context.Background(), "guild-1", "hello")
	if err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}
	if !started {
		t.Fatalf("expected playback to start for the first track")
	}
	if track.ID != "track-1" {
		t.Fatalf("unexpected track id: %s", track.ID)
	}
	if len(backend.played) != 1 {
		t.Fatalf("expected 1 played track, got %d", len(backend.played))
	}
}

func TestSkipAdvancesQueue(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{
				ID:        "track-1",
				Title:     "Track 1",
				Author:    "Artist",
				Duration:  3 * time.Minute,
				Reference: "encoded-1",
			},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{
			ID:        "track-2",
			Title:     "Track 2",
			Author:    "Artist",
			Duration:  4 * time.Minute,
			Reference: "encoded-2",
		},
	}

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	nextTrack, hasNext, err := service.Skip(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("skip returned error: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected next track to exist")
	}
	if nextTrack.ID != "track-2" {
		t.Fatalf("unexpected next track id: %s", nextTrack.ID)
	}
	if len(backend.played) != 2 {
		t.Fatalf("expected 2 play operations, got %d", len(backend.played))
	}
}

func TestStopAndClearRemovesQueue(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{
				ID:        "track-1",
				Title:     "Track 1",
				Author:    "Artist",
				Duration:  3 * time.Minute,
				Reference: "encoded-1",
			},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}

	if err := service.StopAndClear(context.Background(), "guild-1"); err != nil {
		t.Fatalf("stop and clear returned error: %v", err)
	}

	if backend.stopped != 1 {
		t.Fatalf("expected stop to be called once, got %d", backend.stopped)
	}

	queue, err := service.Queue(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("queue returned error: %v", err)
	}
	if queue.Current != nil || len(queue.Upcoming) != 0 {
		t.Fatalf("expected queue to be empty after clear")
	}
}

func TestAdvanceAfterTrackEndStartsNextTrack(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{
				ID:        "track-1",
				Title:     "Track 1",
				Author:    "Artist",
				Duration:  3 * time.Minute,
				Reference: "encoded-1",
			},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{
			ID:        "track-2",
			Title:     "Track 2",
			Author:    "Artist",
			Duration:  4 * time.Minute,
			Reference: "encoded-2",
		},
	}

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	nextTrack, hasNext, err := service.AdvanceAfterTrackEnd(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("advance after track end returned error: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected next track to start")
	}
	if nextTrack.ID != "track-2" {
		t.Fatalf("unexpected next track id: %s", nextTrack.ID)
	}
	if len(backend.played) != 2 {
		t.Fatalf("expected 2 play operations, got %d", len(backend.played))
	}
}

func TestAdvanceAfterTrackEndClearsQueueWhenLastTrackEnds(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{
				ID:        "track-1",
				Title:     "Track 1",
				Author:    "Artist",
				Duration:  3 * time.Minute,
				Reference: "encoded-1",
			},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}

	nextTrack, hasNext, err := service.AdvanceAfterTrackEnd(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("advance after track end returned error: %v", err)
	}
	if hasNext {
		t.Fatalf("expected no next track, got %s", nextTrack.ID)
	}

	queue, err := service.Queue(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("queue returned error: %v", err)
	}
	if queue.Current != nil || len(queue.Upcoming) != 0 {
		t.Fatalf("expected queue to be empty after last track ended")
	}
}
