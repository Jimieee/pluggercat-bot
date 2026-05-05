package musicapp

import (
	"context"
	"errors"
	"fmt"

	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/ports"
)

var (
	ErrNoTracksFound  = errors.New("no tracks found")
	ErrNothingPlaying = errors.New("nothing is currently playing")
)

type Service struct {
	queues ports.QueueRepository
	audio  ports.AudioBackend
}

// create a new music service with the given queue repository and audio backend
func NewService(queues ports.QueueRepository, audio ports.AudioBackend) *Service {
	return &Service{
		queues: queues,
		audio:  audio,
	}
}

// searches for tracks matching the query, enqueues the first result, and starts playback if nothing is currently playing
func (s *Service) Enqueue(ctx context.Context, guildID string, query string) (domainmusic.Track, bool, error) {
	// search for tracks matching the query
	tracks, err := s.audio.SearchTracks(ctx, query)
	if err != nil {
		return domainmusic.Track{}, false, fmt.Errorf("search tracks: %w", err)
	}
	if len(tracks) == 0 {
		return domainmusic.Track{}, false, ErrNoTracksFound
	}

	// load the current queue for the guild
	queue, err := s.loadQueue(ctx, guildID)
	if err != nil {
		return domainmusic.Track{}, false, err
	}

	// enqueue the first search result
	selectedTrack := tracks[0]
	queue = queue.Enqueue(selectedTrack)

	// if nothing is currently playing, start playback of the newly enqueued track
	started := false
	if queue.Current == nil {
		nextTrack, updatedQueue, ok := queue.StartNext()
		if ok {
			if err := s.audio.PlayTrack(ctx, guildID, nextTrack); err != nil {
				return domainmusic.Track{}, false, fmt.Errorf("start playback: %w", err)
			}
			queue = updatedQueue
			started = true
		}
	}

	// save the updated queue state
	if err := s.queues.Save(ctx, queue); err != nil {
		return domainmusic.Track{}, false, fmt.Errorf("save queue: %w", err)
	}

	return selectedTrack, started, nil
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

	nextTrack, updatedQueue, hasNext := queue.Advance()
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

	nextTrack, updatedQueue, hasNext := queue.Advance()
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
