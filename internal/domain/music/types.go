package music

import "time"

type RepeatMode string

const (
	RepeatModeOff   RepeatMode = "off"
	RepeatModeTrack RepeatMode = "track"
	RepeatModeQueue RepeatMode = "queue"
)

type Track struct {
	ID        string
	Title     string
	Author    string
	URL       string
	Source    string
	Artwork   string
	ISRC      string
	Duration  time.Duration
	Reference string
}

type Queue struct {
	GuildID    string
	Current    *Track
	Upcoming   []Track
	Paused     bool
	RepeatMode RepeatMode
}
