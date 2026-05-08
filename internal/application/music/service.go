package musicapp

import (
	"context"
	"errors"
	"fmt"

	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/ports"
)

var (
	ErrNoTracksFound                = errors.New("no tracks found")
	ErrNothingPlaying               = errors.New("nothing is currently playing")
	ErrAutocompleteSelectionExpired = errors.New("autocomplete selection expired")
	ErrQueuePositionOutOfRange      = errors.New("queue position out of range")
	ErrQueueRangeInvalid            = errors.New("queue range is invalid")
)

type Service struct {
	queues ports.QueueRepository
	audio  ports.AudioBackend
	search *searchAssistant
}

// create a new music service with the given queue repository and audio backend
func NewService(queues ports.QueueRepository, audio ports.AudioBackend) *Service {
	return &Service{
		queues: queues,
		audio:  audio,
		search: newSearchAssistant(),
	}
}

// Enqueue keeps the original single-track contract used by existing flows. For
// richer handling of playlists/albums, prefer EnqueueQuery.
func (s *Service) Enqueue(ctx context.Context, guildID string, query string) (domainmusic.Track, bool, error) {
	result, err := s.EnqueueQuery(ctx, guildID, query)
	if err != nil {
		return domainmusic.Track{}, false, err
	}
	return result.LeadTrack, result.Started, nil
}

// EnqueueQuery resolves a query or URL, preserves the Lavalink load result type,
// and enqueues the full playback order when a playlist/album-like collection is loaded.
func (s *Service) EnqueueQuery(ctx context.Context, guildID string, query string) (EnqueueResult, error) {
	selectedTrack, selected := s.search.ResolveSelection(query)

	if selected {
		leadTrack, started, err := s.enqueueTracks(ctx, guildID, []domainmusic.Track{selectedTrack})
		if err != nil {
			return EnqueueResult{}, err
		}
		return EnqueueResult{
			LeadTrack:  leadTrack,
			Started:    started,
			AddedCount: 1,
			LoadType:   domainmusic.LoadResultTypeTrack,
		}, nil
	}
	if s.search.IsSelectionToken(query) {
		return EnqueueResult{}, ErrAutocompleteSelectionExpired
	}

	loadResult, err := s.audio.LoadTracks(ctx, query)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("load tracks: %w", err)
	}
	if !loadResult.HasTracks() {
		return EnqueueResult{}, ErrNoTracksFound
	}

	orderedTracks := loadResult.EnqueueOrder()
	leadTrack, started, err := s.enqueueTracks(ctx, guildID, orderedTracks)
	if err != nil {
		return EnqueueResult{}, err
	}

	result := EnqueueResult{
		LeadTrack:  leadTrack,
		Started:    started,
		AddedCount: len(orderedTracks),
		LoadType:   loadResult.Type,
	}
	if loadResult.Collection != nil {
		result.CollectionName = loadResult.Collection.Name
	}
	return result, nil
}

func (s *Service) EnqueueTrack(ctx context.Context, guildID string, track domainmusic.Track) (domainmusic.Track, bool, error) {
	return s.enqueueTracks(ctx, guildID, []domainmusic.Track{track})
}

func (s *Service) Search(ctx context.Context, query string) ([]domainmusic.Track, error) {
	tracks, err := s.audio.SearchTracks(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search tracks: %w", err)
	}
	if len(tracks) == 0 {
		return nil, ErrNoTracksFound
	}
	return tracks, nil
}

func (s *Service) enqueueTracks(ctx context.Context, guildID string, tracks []domainmusic.Track) (domainmusic.Track, bool, error) {
	if len(tracks) == 0 {
		return domainmusic.Track{}, false, ErrNoTracksFound
	}

	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Track{}, false, err
	}

	for _, track := range tracks {
		queue = queue.Enqueue(track)
	}

	leadTrack := tracks[0]
	started := false
	if queue.Current == nil {
		nextTrack, updatedQueue, ok := queue.StartNext()
		if ok {
			if err := s.audio.PlayTrack(ctx, guildID, nextTrack); err != nil {
				return domainmusic.Track{}, false, fmt.Errorf("start playback: %w", err)
			}
			queue = updatedQueue
			leadTrack = nextTrack
			started = true
		}
	}

	if err := s.queues.Save(ctx, queue); err != nil {
		return domainmusic.Track{}, false, fmt.Errorf("save queue: %w", err)
	}

	return leadTrack, started, nil
}

func (s *Service) Autocomplete(ctx context.Context, userID string, query string, limit int) ([]SearchSuggestion, error) {
	suggestions, err := s.search.Suggest(ctx, userID, query, limit, s.audio.SearchTracks)
	if err != nil {
		return nil, fmt.Errorf("autocomplete suggestions: %w", err)
	}
	return suggestions, nil
}

// pauses playback if a track is currently playing
func (s *Service) Pause(ctx context.Context, guildID string) error {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return err
	}
	if queue.Current == nil {
		return ErrNothingPlaying
	}

	if err := s.audio.Pause(ctx, guildID); err != nil {
		return fmt.Errorf("pause playback: %w", err)
	}

	queue = queue.Pause()
	return s.queues.Save(ctx, queue)
}

// resumes playback if a track is currently paused
func (s *Service) Resume(ctx context.Context, guildID string) error {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return err
	}
	if queue.Current == nil {
		return ErrNothingPlaying
	}

	if err := s.audio.Resume(ctx, guildID); err != nil {
		return fmt.Errorf("resume playback: %w", err)
	}

	queue = queue.Resume()
	return s.queues.Save(ctx, queue)
}

// stops playback and clears the queue
func (s *Service) Skip(ctx context.Context, guildID string) (domainmusic.Track, bool, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Track{}, false, err
	}
	if queue.Current == nil {
		return domainmusic.Track{}, false, ErrNothingPlaying
	}

	nextTrack, updatedQueue, hasNext := queue.AdvanceAfterSkip()
	if hasNext {
		if err := s.audio.PlayTrack(ctx, guildID, nextTrack); err != nil {
			return domainmusic.Track{}, false, fmt.Errorf("play next track: %w", err)
		}
	} else {
		if err := s.audio.Stop(ctx, guildID); err != nil {
			return domainmusic.Track{}, false, fmt.Errorf("stop playback: %w", err)
		}
	}

	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Track{}, false, fmt.Errorf("save queue after skip: %w", err)
	}

	return nextTrack, hasNext, nil
}

// returns the current queue state for the guild
func (s *Service) Queue(ctx context.Context, guildID string) (domainmusic.Queue, error) {
	return s.loadQueue(ctx, guildID)
}

func (s *Service) RemoveUpcomingTrack(
	ctx context.Context,
	guildID string,
	upcomingIndex int,
) (domainmusic.Track, domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Track{}, domainmusic.Queue{}, err
	}

	removedTrack, updatedQueue, ok := queue.RemoveUpcoming(upcomingIndex)
	if !ok {
		return domainmusic.Track{}, domainmusic.Queue{}, ErrQueuePositionOutOfRange
	}

	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Track{}, domainmusic.Queue{}, fmt.Errorf("save queue after remove: %w", err)
	}

	return removedTrack, updatedQueue, nil
}

func (s *Service) RemoveUpcomingRange(
	ctx context.Context,
	guildID string,
	startIndex int,
	endIndex int,
) (int, domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return 0, domainmusic.Queue{}, err
	}

	removedTracks, updatedQueue, ok := queue.RemoveUpcomingRange(startIndex, endIndex)
	if !ok {
		return 0, domainmusic.Queue{}, ErrQueueRangeInvalid
	}

	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return 0, domainmusic.Queue{}, fmt.Errorf("save queue after remove range: %w", err)
	}

	return len(removedTracks), updatedQueue, nil
}

func (s *Service) ClearUpcomingTracks(ctx context.Context, guildID string) (int, domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return 0, domainmusic.Queue{}, err
	}

	removedCount := len(queue.Upcoming)
	updatedQueue := queue.ClearUpcoming()
	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return 0, domainmusic.Queue{}, fmt.Errorf("save queue after clear: %w", err)
	}

	return removedCount, updatedQueue, nil
}

func (s *Service) ShuffleUpcomingTracks(ctx context.Context, guildID string) (domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Queue{}, err
	}

	updatedQueue := queue.ShuffleUpcoming()
	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Queue{}, fmt.Errorf("save queue after shuffle: %w", err)
	}

	return updatedQueue, nil
}

func (s *Service) JumpToUpcomingTrack(
	ctx context.Context,
	guildID string,
	upcomingIndex int,
) (domainmusic.Track, domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Track{}, domainmusic.Queue{}, err
	}

	selectedTrack, updatedQueue, ok := queue.JumpToUpcoming(upcomingIndex)
	if !ok {
		return domainmusic.Track{}, domainmusic.Queue{}, ErrQueuePositionOutOfRange
	}

	if err := s.audio.PlayTrack(ctx, guildID, selectedTrack); err != nil {
		return domainmusic.Track{}, domainmusic.Queue{}, fmt.Errorf("jump to queued track: %w", err)
	}
	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Track{}, domainmusic.Queue{}, fmt.Errorf("save queue after jump: %w", err)
	}

	return selectedTrack, updatedQueue, nil
}

func (s *Service) MoveUpcomingTrack(
	ctx context.Context,
	guildID string,
	fromIndex int,
	toIndex int,
) (domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Queue{}, err
	}

	updatedQueue, ok := queue.MoveUpcoming(fromIndex, toIndex)
	if !ok {
		return domainmusic.Queue{}, ErrQueuePositionOutOfRange
	}

	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Queue{}, fmt.Errorf("save queue after move: %w", err)
	}

	return updatedQueue, nil
}

func (s *Service) CycleRepeatMode(ctx context.Context, guildID string) (domainmusic.Queue, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Queue{}, err
	}

	updatedQueue := queue.CycleRepeatMode()
	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Queue{}, fmt.Errorf("save queue after repeat change: %w", err)
	}

	return updatedQueue, nil
}

// advances the queue and starts playback of the next track if there is one, typically called when a track ends to automatically play the next one in the queue
func (s *Service) AdvanceAfterTrackEnd(ctx context.Context, guildID string) (domainmusic.Track, bool, error) {
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Track{}, false, err
	}

	if queue.Current == nil {
		if len(queue.Upcoming) == 0 {
			return domainmusic.Track{}, false, nil
		}

		nextTrack, updatedQueue, ok := queue.StartNext()
		if !ok {
			return domainmusic.Track{}, false, nil
		}
		if err := s.audio.PlayTrack(ctx, guildID, nextTrack); err != nil {
			return domainmusic.Track{}, false, fmt.Errorf("play next track after end: %w", err)
		}
		if err := s.queues.Save(ctx, updatedQueue); err != nil {
			return domainmusic.Track{}, false, fmt.Errorf("save queue after end: %w", err)
		}
		return nextTrack, true, nil
	}

	nextTrack, updatedQueue, hasNext := queue.AdvanceAfterTrackEnd()
	if !hasNext {
		if err := s.queues.Delete(ctx, guildID); err != nil {
			return domainmusic.Track{}, false, fmt.Errorf("delete queue after end: %w", err)
		}
		return domainmusic.Track{}, false, nil
	}

	if err := s.audio.PlayTrack(ctx, guildID, nextTrack); err != nil {
		return domainmusic.Track{}, false, fmt.Errorf("play next track after end: %w", err)
	}
	if err := s.queues.Save(ctx, updatedQueue); err != nil {
		return domainmusic.Track{}, false, fmt.Errorf("save queue after end: %w", err)
	}

	return nextTrack, true, nil
}

// stops playback and clears the queue, typically called when leaving a voice channel to ensure no music is left playing in an empty channel
func (s *Service) StopAndClear(ctx context.Context, guildID string) error {
	if err := s.audio.Stop(ctx, guildID); err != nil {
		return fmt.Errorf("stop playback: %w", err)
	}
	if err := s.queues.Delete(ctx, guildID); err != nil {
		return fmt.Errorf("delete queue: %w", err)
	}
	return nil
}

// helper method to load the queue for a guild, returning an empty queue if not found
func (s *Service) loadQueue(ctx context.Context, guildID string) (domainmusic.Queue, error) {
	queue, found, err := s.queues.Get(ctx, guildID)
	if err != nil {
		return domainmusic.Queue{}, fmt.Errorf("load queue: %w", err)
	}
	if !found {
		return domainmusic.NewQueue(guildID), nil
	}
	return queue, nil
}
