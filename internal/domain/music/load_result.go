package music

type LoadResultType string

const (
	LoadResultTypeTrack    LoadResultType = "track"
	LoadResultTypeSearch   LoadResultType = "search"
	LoadResultTypePlaylist LoadResultType = "playlist"
)

type Collection struct {
	Name          string
	SelectedTrack int
}

type LoadResult struct {
	Type       LoadResultType
	Tracks     []Track
	Collection *Collection
}

func (r LoadResult) HasTracks() bool {
	return len(r.Tracks) > 0
}

// PlaybackOrder returns the tracks in the preferred playback order. When Lavalink
// marks a selected item inside a playlist, that track is played first while the
// remaining items keep their relative order.
func (r LoadResult) PlaybackOrder() []Track {
	if len(r.Tracks) == 0 {
		return nil
	}

	ordered := append([]Track(nil), r.Tracks...)
	if r.Type != LoadResultTypePlaylist || r.Collection == nil {
		return ordered
	}

	selected := r.Collection.SelectedTrack
	if selected <= 0 || selected >= len(ordered) {
		return ordered
	}

	reordered := append([]Track(nil), ordered[selected:]...)
	reordered = append(reordered, ordered[:selected]...)
	return reordered
}

// EnqueueOrder returns the tracks that should actually be queued for a user
// request. Search-style results should only enqueue the primary match, while
// real collections like playlists and albums keep their full playback order.
func (r LoadResult) EnqueueOrder() []Track {
	if len(r.Tracks) == 0 {
		return nil
	}

	if r.Type == LoadResultTypePlaylist {
		return r.PlaybackOrder()
	}

	primary, ok := r.PrimaryTrack()
	if !ok {
		return nil
	}
	return []Track{primary}
}

func (r LoadResult) PrimaryTrack() (Track, bool) {
	ordered := r.PlaybackOrder()
	if len(ordered) == 0 {
		return Track{}, false
	}
	return ordered[0], true
}
