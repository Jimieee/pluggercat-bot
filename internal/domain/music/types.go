package music

import "time"

type Track struct {
	ID        string
	Title     string
	Author    string
	URL       string
	Source    string
	Duration  time.Duration
	Reference string
}

type Queue struct {
	GuildID  string
	Current  *Track
	Upcoming []Track
	Paused   bool
}
