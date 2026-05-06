package musicapp

import (
	"context"
	"strconv"
	"testing"
	"time"

	"pluggercat/internal/adapters/store/memory"
	domainmusic "pluggercat/internal/domain/music"
)

type fakeAudioBackend struct {
	searchResults []domainmusic.Track
	loadResult    domainmusic.LoadResult
	searchCalls   int
	loadCalls     int
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
	f.searchCalls++
	return f.searchResults, nil
}

func (f *fakeAudioBackend) LoadTracks(context.Context, string) (domainmusic.LoadResult, error) {
	f.loadCalls++
	if f.loadResult.Type != "" || len(f.loadResult.Tracks) > 0 || f.loadResult.Collection != nil {
		return f.loadResult, nil
	}
	return domainmusic.LoadResult{
		Type:   domainmusic.LoadResultTypeSearch,
		Tracks: append([]domainmusic.Track(nil), f.searchResults...),
	}, nil
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

func TestEnqueueQueryAddsFullPlaylist(t *testing.T) {
	backend := &fakeAudioBackend{
		loadResult: domainmusic.LoadResult{
			Type: domainmusic.LoadResultTypePlaylist,
			Tracks: []domainmusic.Track{
				{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
				{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
				{ID: "track-3", Title: "Track 3", Author: "Artist", Duration: 5 * time.Minute, Reference: "encoded-3"},
			},
			Collection: &domainmusic.Collection{
				Name:          "My Playlist",
				SelectedTrack: -1,
			},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	result, err := service.EnqueueQuery(context.Background(), "guild-1", "https://example.com/playlist")
	if err != nil {
		t.Fatalf("enqueue query returned error: %v", err)
	}
	if !result.Started {
		t.Fatalf("expected playback to start for the first playlist track")
	}
	if result.LeadTrack.ID != "track-1" {
		t.Fatalf("unexpected lead track id: %s", result.LeadTrack.ID)
	}
	if result.AddedCount != 3 {
		t.Fatalf("expected 3 tracks to be added, got %d", result.AddedCount)
	}
	if result.CollectionName != "My Playlist" {
		t.Fatalf("unexpected collection name: %s", result.CollectionName)
	}

	queue, found, err := repo.Get(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("load queue returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected queue to be saved")
	}
	if queue.Current == nil || queue.Current.ID != "track-1" {
		t.Fatalf("expected track-1 to be playing, got %+v", queue.Current)
	}
	if len(queue.Upcoming) != 2 || queue.Upcoming[0].ID != "track-2" || queue.Upcoming[1].ID != "track-3" {
		t.Fatalf("unexpected upcoming order: %+v", queue.Upcoming)
	}
}

func TestEnqueueQueryHonorsSelectedTrackInPlaylist(t *testing.T) {
	backend := &fakeAudioBackend{
		loadResult: domainmusic.LoadResult{
			Type: domainmusic.LoadResultTypePlaylist,
			Tracks: []domainmusic.Track{
				{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
				{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
				{ID: "track-3", Title: "Track 3", Author: "Artist", Duration: 5 * time.Minute, Reference: "encoded-3"},
			},
			Collection: &domainmusic.Collection{
				Name:          "My Playlist",
				SelectedTrack: 1,
			},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	result, err := service.EnqueueQuery(context.Background(), "guild-1", "https://example.com/playlist?selected=2")
	if err != nil {
		t.Fatalf("enqueue query returned error: %v", err)
	}
	if result.LeadTrack.ID != "track-2" {
		t.Fatalf("expected selected track to play first, got %s", result.LeadTrack.ID)
	}
	if len(backend.played) != 1 || backend.played[0].ID != "track-2" {
		t.Fatalf("unexpected played tracks: %+v", backend.played)
	}

	queue, found, err := repo.Get(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("load queue returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected queue to be saved")
	}
	if queue.Current == nil || queue.Current.ID != "track-2" {
		t.Fatalf("expected track-2 to be playing, got %+v", queue.Current)
	}
	if len(queue.Upcoming) != 2 || queue.Upcoming[0].ID != "track-3" || queue.Upcoming[1].ID != "track-1" {
		t.Fatalf("unexpected upcoming order: %+v", queue.Upcoming)
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

func TestRemoveUpcomingTrackRemovesSelectedEntry(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-3", Title: "Track 3", Author: "Artist", Duration: 5 * time.Minute, Reference: "encoded-3"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "third"); err != nil {
		t.Fatalf("third enqueue returned error: %v", err)
	}

	removedTrack, queue, err := service.RemoveUpcomingTrack(context.Background(), "guild-1", 0)
	if err != nil {
		t.Fatalf("remove upcoming returned error: %v", err)
	}
	if removedTrack.ID != "track-2" {
		t.Fatalf("unexpected removed track id: %s", removedTrack.ID)
	}
	if len(queue.Upcoming) != 1 || queue.Upcoming[0].ID != "track-3" {
		t.Fatalf("unexpected remaining queue after remove: %+v", queue.Upcoming)
	}
}

func TestJumpToUpcomingTrackStartsSelectedEntryImmediately(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-3", Title: "Track 3", Author: "Artist", Duration: 5 * time.Minute, Reference: "encoded-3"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "third"); err != nil {
		t.Fatalf("third enqueue returned error: %v", err)
	}

	jumpedTrack, queue, err := service.JumpToUpcomingTrack(context.Background(), "guild-1", 1)
	if err != nil {
		t.Fatalf("jump to upcoming returned error: %v", err)
	}
	if jumpedTrack.ID != "track-3" {
		t.Fatalf("unexpected jumped track id: %s", jumpedTrack.ID)
	}
	if queue.Current == nil || queue.Current.ID != "track-3" {
		t.Fatalf("expected track-3 to be current after jump, got %+v", queue.Current)
	}
	if len(queue.Upcoming) != 2 || queue.Upcoming[0].ID != "track-1" || queue.Upcoming[1].ID != "track-2" {
		t.Fatalf("unexpected queue order after jump: %+v", queue.Upcoming)
	}
	if len(backend.played) != 2 || backend.played[1].ID != "track-3" {
		t.Fatalf("expected jump to trigger immediate playback of track-3, got %+v", backend.played)
	}
}

func TestMoveUpcomingTrackReordersQueue(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	repo := memory.NewQueueRepository()
	service := NewService(repo, backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	for i := 2; i <= 4; i++ {
		backend.searchResults = []domainmusic.Track{
			{
				ID:        "track-" + strconv.Itoa(i),
				Title:     "Track " + strconv.Itoa(i),
				Author:    "Artist",
				Duration:  time.Duration(i) * time.Minute,
				Reference: "encoded-" + strconv.Itoa(i),
			},
		}
		if _, _, err := service.Enqueue(context.Background(), "guild-1", "track"); err != nil {
			t.Fatalf("enqueue %d returned error: %v", i, err)
		}
	}

	queue, err := service.MoveUpcomingTrack(context.Background(), "guild-1", 2, 0)
	if err != nil {
		t.Fatalf("move upcoming returned error: %v", err)
	}

	if got := []string{queue.Upcoming[0].ID, queue.Upcoming[1].ID, queue.Upcoming[2].ID}; got[0] != "track-4" || got[1] != "track-2" || got[2] != "track-3" {
		t.Fatalf("unexpected queue order after move: %+v", got)
	}
}

func TestClearUpcomingTracksLeavesCurrentTrackPlaying(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	removedCount, queue, err := service.ClearUpcomingTracks(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("clear upcoming returned error: %v", err)
	}
	if removedCount != 1 {
		t.Fatalf("expected 1 removed track, got %d", removedCount)
	}
	if queue.Current == nil || queue.Current.ID != "track-1" {
		t.Fatalf("expected current track to remain playing, got %+v", queue.Current)
	}
	if len(queue.Upcoming) != 0 {
		t.Fatalf("expected upcoming queue to be cleared, got %+v", queue.Upcoming)
	}
}

func TestRemoveUpcomingRangeDeletesInclusiveWindow(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	for i := 2; i <= 5; i++ {
		backend.searchResults = []domainmusic.Track{
			{
				ID:        "track-" + strconv.Itoa(i),
				Title:     "Track " + strconv.Itoa(i),
				Author:    "Artist",
				Duration:  time.Duration(i) * time.Minute,
				Reference: "encoded-" + strconv.Itoa(i),
			},
		}
		if _, _, err := service.Enqueue(context.Background(), "guild-1", "track"); err != nil {
			t.Fatalf("enqueue %d returned error: %v", i, err)
		}
	}

	removedCount, queue, err := service.RemoveUpcomingRange(context.Background(), "guild-1", 1, 2)
	if err != nil {
		t.Fatalf("remove range returned error: %v", err)
	}
	if removedCount != 2 {
		t.Fatalf("expected 2 removed tracks, got %d", removedCount)
	}
	if got := []string{queue.Upcoming[0].ID, queue.Upcoming[1].ID}; got[0] != "track-2" || got[1] != "track-5" {
		t.Fatalf("unexpected queue order after remove range: %+v", got)
	}
}

func TestCycleRepeatModeCyclesThroughAllModes(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	queue, err := service.CycleRepeatMode(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("first cycle repeat returned error: %v", err)
	}
	if queue.RepeatMode != domainmusic.RepeatModeTrack {
		t.Fatalf("expected repeat mode track, got %s", queue.RepeatMode)
	}

	queue, err = service.CycleRepeatMode(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("second cycle repeat returned error: %v", err)
	}
	if queue.RepeatMode != domainmusic.RepeatModeQueue {
		t.Fatalf("expected repeat mode queue, got %s", queue.RepeatMode)
	}

	queue, err = service.CycleRepeatMode(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("third cycle repeat returned error: %v", err)
	}
	if queue.RepeatMode != domainmusic.RepeatModeOff {
		t.Fatalf("expected repeat mode off, got %s", queue.RepeatMode)
	}
}

func TestAdvanceAfterTrackEndRepeatsCurrentTrackWhenRepeatTrackEnabled(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}
	if _, err := service.CycleRepeatMode(context.Background(), "guild-1"); err != nil {
		t.Fatalf("cycle repeat returned error: %v", err)
	}

	nextTrack, hasNext, err := service.AdvanceAfterTrackEnd(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("advance after track end returned error: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected repeat track to keep playback going")
	}
	if nextTrack.ID != "track-1" {
		t.Fatalf("expected the same track to repeat, got %s", nextTrack.ID)
	}
	if len(backend.played) != 2 || backend.played[1].ID != "track-1" {
		t.Fatalf("expected playback to restart the same track, got %+v", backend.played)
	}
}

func TestSkipDoesNotReplaySameTrackWhenRepeatTrackEnabled(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	if _, err := service.CycleRepeatMode(context.Background(), "guild-1"); err != nil {
		t.Fatalf("cycle repeat returned error: %v", err)
	}

	nextTrack, hasNext, err := service.Skip(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("skip returned error: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected skip to move to the next queued track")
	}
	if nextTrack.ID != "track-2" {
		t.Fatalf("expected skip to play track-2, got %s", nextTrack.ID)
	}
}

func TestAdvanceAfterTrackEndLoopsQueueWhenRepeatQueueEnabled(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	backend.searchResults = []domainmusic.Track{
		{ID: "track-2", Title: "Track 2", Author: "Artist", Duration: 4 * time.Minute, Reference: "encoded-2"},
	}
	if _, _, err := service.Enqueue(context.Background(), "guild-1", "second"); err != nil {
		t.Fatalf("second enqueue returned error: %v", err)
	}

	if _, err := service.CycleRepeatMode(context.Background(), "guild-1"); err != nil {
		t.Fatalf("first cycle repeat returned error: %v", err)
	}
	if _, err := service.CycleRepeatMode(context.Background(), "guild-1"); err != nil {
		t.Fatalf("second cycle repeat returned error: %v", err)
	}

	nextTrack, hasNext, err := service.AdvanceAfterTrackEnd(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("advance after track end returned error: %v", err)
	}
	if !hasNext {
		t.Fatalf("expected repeat queue to continue playback")
	}
	if nextTrack.ID != "track-2" {
		t.Fatalf("expected next track in loop to be track-2, got %s", nextTrack.ID)
	}

	queue, err := service.Queue(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("queue returned error: %v", err)
	}
	if queue.Current == nil || queue.Current.ID != "track-2" {
		t.Fatalf("expected track-2 as current after looping, got %+v", queue.Current)
	}
	if len(queue.Upcoming) != 1 || queue.Upcoming[0].ID != "track-1" {
		t.Fatalf("expected previous track to rotate to queue tail, got %+v", queue.Upcoming)
	}
}

func TestShuffleUpcomingTracksPreservesAllQueuedItems(t *testing.T) {
	backend := &fakeAudioBackend{
		searchResults: []domainmusic.Track{
			{ID: "track-1", Title: "Track 1", Author: "Artist", Duration: 3 * time.Minute, Reference: "encoded-1"},
		},
	}

	service := NewService(memory.NewQueueRepository(), backend)

	if _, _, err := service.Enqueue(context.Background(), "guild-1", "first"); err != nil {
		t.Fatalf("first enqueue returned error: %v", err)
	}

	for i := 2; i <= 5; i++ {
		backend.searchResults = []domainmusic.Track{
			{
				ID:        "track-" + strconv.Itoa(i),
				Title:     "Track " + strconv.Itoa(i),
				Author:    "Artist",
				Duration:  time.Duration(i) * time.Minute,
				Reference: "encoded-" + strconv.Itoa(i),
			},
		}
		if _, _, err := service.Enqueue(context.Background(), "guild-1", "track"); err != nil {
			t.Fatalf("enqueue %d returned error: %v", i, err)
		}
	}

	queue, err := service.ShuffleUpcomingTracks(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("shuffle upcoming returned error: %v", err)
	}

	if len(queue.Upcoming) != 4 {
		t.Fatalf("expected 4 upcoming tracks after shuffle, got %d", len(queue.Upcoming))
	}

	seen := map[string]bool{}
	for _, track := range queue.Upcoming {
		seen[track.ID] = true
	}
	for _, expectedID := range []string{"track-2", "track-3", "track-4", "track-5"} {
		if !seen[expectedID] {
			t.Fatalf("expected track %s to remain after shuffle", expectedID)
		}
	}
}
