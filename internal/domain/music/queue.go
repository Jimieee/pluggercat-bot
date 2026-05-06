package music

import randv2 "math/rand/v2"

// NewQueue returns an empty queue state for a guild.
func NewQueue(guildID string) Queue {
	return Queue{
		GuildID:    guildID,
		Upcoming:   []Track{},
		RepeatMode: RepeatModeOff,
	}
}

// Enqueue appends a track to the end of the queue.
func (q Queue) Enqueue(track Track) Queue {
	q.Upcoming = append(q.Upcoming, track)
	return q
}

// StartNext starts the next queued track if playback is currently idle.
func (q Queue) StartNext() (Track, Queue, bool) {
	q.RepeatMode = normalizeRepeatMode(q.RepeatMode)
	if q.Current != nil || len(q.Upcoming) == 0 {
		return Track{}, q, false
	}

	nextTrack := q.Upcoming[0]
	q.Upcoming = append([]Track(nil), q.Upcoming[1:]...)
	q.Current = &nextTrack
	q.Paused = false
	return nextTrack, q, true
}

// Advance stops the current track and starts the next one when available.
func (q Queue) Advance() (Track, Queue, bool) {
	q.RepeatMode = normalizeRepeatMode(q.RepeatMode)
	q.Current = nil
	q.Paused = false
	return q.StartNext()
}

// AdvanceAfterTrackEnd advances playback while honoring the configured repeat mode.
func (q Queue) AdvanceAfterTrackEnd() (Track, Queue, bool) {
	q.RepeatMode = normalizeRepeatMode(q.RepeatMode)

	if q.Current == nil {
		return q.StartNext()
	}

	switch q.RepeatMode {
	case RepeatModeTrack:
		q.Paused = false
		return *q.Current, q, true

	case RepeatModeQueue:
		currentTrack := *q.Current
		q.Upcoming = append(q.Upcoming, currentTrack)
		q.Current = nil
		q.Paused = false
		return q.StartNext()

	default:
		return q.Advance()
	}
}

// AdvanceAfterSkip advances playback after an explicit skip action.
func (q Queue) AdvanceAfterSkip() (Track, Queue, bool) {
	q.RepeatMode = normalizeRepeatMode(q.RepeatMode)

	if q.Current == nil {
		return Track{}, q, false
	}

	if q.RepeatMode == RepeatModeQueue {
		currentTrack := *q.Current
		q.Upcoming = append(q.Upcoming, currentTrack)
	}

	q.Current = nil
	q.Paused = false
	return q.StartNext()
}

// RemoveUpcoming removes a track from the upcoming queue by zero-based index.
func (q Queue) RemoveUpcoming(index int) (Track, Queue, bool) {
	if index < 0 || index >= len(q.Upcoming) {
		return Track{}, q, false
	}

	removed := q.Upcoming[index]
	updated := append([]Track(nil), q.Upcoming[:index]...)
	updated = append(updated, q.Upcoming[index+1:]...)
	q.Upcoming = updated
	return removed, q, true
}

// RemoveUpcomingRange removes an inclusive range from the upcoming queue.
func (q Queue) RemoveUpcomingRange(start int, end int) ([]Track, Queue, bool) {
	if start < 0 || end < start || end >= len(q.Upcoming) {
		return nil, q, false
	}

	removed := append([]Track(nil), q.Upcoming[start:end+1]...)
	updated := append([]Track(nil), q.Upcoming[:start]...)
	updated = append(updated, q.Upcoming[end+1:]...)
	q.Upcoming = updated
	return removed, q, true
}

// ClearUpcoming removes every queued track after the current one.
func (q Queue) ClearUpcoming() Queue {
	q.Upcoming = []Track{}
	return q
}

// ShuffleUpcoming randomizes the order of tracks waiting in the queue.
func (q Queue) ShuffleUpcoming() Queue {
	if len(q.Upcoming) <= 1 {
		return q
	}

	shuffled := append([]Track(nil), q.Upcoming...)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := randv2.IntN(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	q.Upcoming = shuffled
	return q
}

// JumpToUpcoming makes a queued track play immediately while preserving the rest of the queue order.
func (q Queue) JumpToUpcoming(index int) (Track, Queue, bool) {
	selected, updatedQueue, ok := q.RemoveUpcoming(index)
	if !ok {
		return Track{}, q, false
	}

	if updatedQueue.Current != nil {
		previousCurrent := *updatedQueue.Current
		updatedQueue.Upcoming = append([]Track{previousCurrent}, updatedQueue.Upcoming...)
	}

	updatedQueue.Current = &selected
	updatedQueue.Paused = false
	return selected, updatedQueue, true
}

// MoveUpcoming moves a queued track to a new zero-based position in the upcoming queue.
func (q Queue) MoveUpcoming(fromIndex int, toIndex int) (Queue, bool) {
	if fromIndex < 0 || fromIndex >= len(q.Upcoming) || toIndex < 0 || toIndex >= len(q.Upcoming) {
		return q, false
	}
	if fromIndex == toIndex {
		return q, true
	}

	track := q.Upcoming[fromIndex]
	updated := append([]Track(nil), q.Upcoming[:fromIndex]...)
	updated = append(updated, q.Upcoming[fromIndex+1:]...)

	if toIndex >= len(updated) {
		updated = append(updated, track)
	} else {
		updated = append(updated[:toIndex], append([]Track{track}, updated[toIndex:]...)...)
	}

	q.Upcoming = updated
	return q, true
}

// CycleRepeatMode advances the repeat mode through off, track, and queue.
func (q Queue) CycleRepeatMode() Queue {
	switch normalizeRepeatMode(q.RepeatMode) {
	case RepeatModeOff:
		q.RepeatMode = RepeatModeTrack
	case RepeatModeTrack:
		q.RepeatMode = RepeatModeQueue
	default:
		q.RepeatMode = RepeatModeOff
	}
	return q
}

// Pause marks the queue as paused.
func (q Queue) Pause() Queue {
	q.Paused = true
	return q
}

// Resume marks the queue as actively playing.
func (q Queue) Resume() Queue {
	q.Paused = false
	return q
}

// Length returns the number of tracks represented by the queue state.
func (q Queue) Length() int {
	if q.Current == nil {
		return len(q.Upcoming)
	}
	return 1 + len(q.Upcoming)
}

func normalizeRepeatMode(mode RepeatMode) RepeatMode {
	switch mode {
	case RepeatModeTrack, RepeatModeQueue:
		return mode
	default:
		return RepeatModeOff
	}
}
