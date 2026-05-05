package music

// represents the state of a music queue for a guild, including the currently playing track (if any), the list of upcoming tracks, and whether playback is paused
func NewQueue(guildID string) Queue {
	return Queue{
		GuildID:  guildID,
		Upcoming: []Track{},
	}
}

// adds a track to the end of the queue
func (q Queue) Enqueue(track Track) Queue {
	q.Upcoming = append(q.Upcoming, track)
	return q
}

// starts playback of the next track in the queue if nothing is currently playing, returning the track that was started and whether playback was started
func (q Queue) StartNext() (Track, Queue, bool) {
	// if a track is currently playing or there are no upcoming tracks, do nothing
	if q.Current != nil || len(q.Upcoming) == 0 {
		return Track{}, q, false
	}

	// start playback of the next track
	nextTrack := q.Upcoming[0]
	q.Upcoming = append([]Track(nil), q.Upcoming[1:]...)
	q.Current = &nextTrack
	q.Paused = false
	return nextTrack, q, true
}

// advances to the next track in the queue, returning the track that was started and whether there was a next track to play
func (q Queue) Advance() (Track, Queue, bool) {
	q.Current = nil
	q.Paused = false
	return q.StartNext()
}

// pauses playback if a track is currently playing
func (q Queue) Pause() Queue {
	q.Paused = true
	return q
}

// resumes playback if a track is currently paused
func (q Queue) Resume() Queue {
	q.Paused = false
	return q
}

// stops playback and clears the queue
func (q Queue) Length() int {
	if q.Current == nil {
		return len(q.Upcoming)
	}
	return 1 + len(q.Upcoming)
}
