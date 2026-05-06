package musicui

import (
	"strings"
	"testing"
	"time"

	domainmusic "pluggercat/internal/domain/music"
)

func TestBuildQueuePageKeepsUpcomingFieldWithinDiscordLimit(t *testing.T) {
	queue := domainmusic.NewQueue("guild-1")
	current := domainmusic.Track{
		ID:        "current",
		Title:     "Current Track",
		Author:    "Current Artist",
		Duration:  3 * time.Minute,
		Reference: "encoded-current",
		URL:       "https://soundcloud.com/example/current-track",
	}
	queue.Current = &current

	for i := 0; i < 10; i++ {
		queue.Upcoming = append(queue.Upcoming, domainmusic.Track{
			ID:        "track-" + strings.Repeat("x", 20),
			Title:     "A very long SoundCloud title " + strings.Repeat("title-", 15),
			Author:    "A very long artist name " + strings.Repeat("artist-", 10),
			Duration:  time.Duration(180+i) * time.Second,
			Reference: "encoded-upcoming",
			URL:       "https://soundcloud.com/example/really/long/upcoming/track/url/" + strings.Repeat("segment-", 12),
		})
	}

	page := buildQueuePage(queue, 0, queueActionModeBrowse)
	if len(page.Page.Embeds) != 1 {
		t.Fatalf("expected one embed, got %d", len(page.Page.Embeds))
	}

	embed := page.Page.Embeds[0]
	if len(embed.Fields) < 4 {
		t.Fatalf("expected at least 4 fields, got %d", len(embed.Fields))
	}

	upcoming := embed.Fields[3].Value
	if len([]rune(upcoming)) > discordEmbedFieldValueLimit {
		t.Fatalf("expected upcoming field to stay within %d runes, got %d", discordEmbedFieldValueLimit, len([]rune(upcoming)))
	}
	if strings.Contains(upcoming, "](http") {
		t.Fatalf("expected upcoming field to avoid markdown links, got %q", upcoming)
	}
}
