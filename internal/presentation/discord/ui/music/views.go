package musicui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"

	musicapp "pluggercat/internal/application/music"
	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/presentation/discord/ui/pagination"
)

const (
	accentColor                  = 0x1DB954
	queuePageSize                = 10
	searchPageSize               = 10
	discordEmbedFieldValueLimit  = 1024
	discordEmbedDescriptionLimit = 4096
	queueTrackLineLimit          = 88
	searchTrackLineLimit         = 120
)

type queueActionMode string

const (
	queueActionModeBrowse queueActionMode = "browse"
	queueActionModeJump   queueActionMode = "jump"
	queueActionModeMove   queueActionMode = "move"
	queueActionModeRemove queueActionMode = "remove"
)

func BuildPlaybackEmbed(title string, track domainmusic.Track, footerText string) discord.Embed {
	embed := discord.NewEmbed().
		WithTitle(title).
		WithColor(accentColor).
		WithDescription(trackSummary(track)).
		WithFooterText(footerText)

	if track.URL != "" {
		embed = embed.WithURL(track.URL)
	}
	if track.Artwork != "" {
		embed = embed.WithThumbnail(track.Artwork)
	}

	embed = embed.
		AddField("Duration", formatDuration(track.Duration), true).
		AddField("Source", safeFieldValue(track.Source), true)

	if track.ISRC != "" {
		embed = embed.AddField("ISRC", track.ISRC, true)
	}

	return embed
}

func BuildEnqueueEmbed(title string, result musicapp.EnqueueResult, footerText string) discord.Embed {
	embed := BuildPlaybackEmbed(title, result.LeadTrack, footerText)

	if result.AddedCount > 1 {
		embed = embed.AddField("Tracks Added", strconv.Itoa(result.AddedCount), true)
		if strings.TrimSpace(result.CollectionName) != "" {
			embed = embed.AddField("Collection", truncateString(result.CollectionName, 1024), true)
		}
	}

	return embed
}

func BuildQueueEmptyEmbed() discord.Embed {
	return discord.NewEmbed().
		WithTitle("Queue").
		WithDescription("The queue is empty.").
		WithColor(accentColor)
}

func BuildSearchPage(query string, tracks []domainmusic.Track, page int) pagination.RenderedPage {
	total := totalPages(len(tracks), searchPageSize)
	page = clampPage(page, total)

	start, end := pageBounds(len(tracks), page, searchPageSize)
	pageTracks := tracks[start:end]

	embed := discord.NewEmbed().
		WithTitle("Search Results").
		WithColor(accentColor).
		WithDescription(searchResultsLines(pageTracks, start)).
		WithFooterTextf("Page %d/%d • Select a track from the menu below", page+1, total)

	if query != "" {
		embed = embed.WithAuthorNamef("Results for: %s", truncateString(query, 200))
	}
	if len(pageTracks) > 0 {
		if pageTracks[0].Artwork != "" {
			embed = embed.WithThumbnail(pageTracks[0].Artwork)
		}
		if pageTracks[0].URL != "" {
			embed = embed.WithURL(pageTracks[0].URL)
		}
	}

	return pagination.RenderedPage{
		Page: pagination.Page{
			Embeds: []discord.Embed{embed},
			Select: &pagination.SelectMenu{
				Placeholder: "Choose a track to play or enqueue",
				Options:     buildSearchSelectOptions(pageTracks, start),
			},
		},
		PageIndex:  page,
		TotalPages: total,
	}
}

func AutocompleteChoicesFromSuggestions(suggestions []musicapp.SearchSuggestion) []discord.AutocompleteChoice {
	if len(suggestions) == 0 {
		return []discord.AutocompleteChoice{}
	}

	choices := make([]discord.AutocompleteChoice, 0, len(suggestions))
	for _, suggestion := range suggestions {
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  suggestion.Label,
			Value: suggestion.Value,
		})
	}
	return choices
}

func TrackFromSelection(tracks []domainmusic.Track, value string) (domainmusic.Track, bool) {
	index, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || index < 0 || index >= len(tracks) {
		return domainmusic.Track{}, false
	}
	return tracks[index], true
}

func buildQueuePage(queue domainmusic.Queue, page int, mode queueActionMode) pagination.RenderedPage {
	if queue.Current == nil && len(queue.Upcoming) == 0 {
		return pagination.RenderedPage{
			Page: pagination.Page{
				Embeds: []discord.Embed{BuildQueueEmptyEmbed()},
			},
			PageIndex:  0,
			TotalPages: 1,
		}
	}

	total := totalPages(len(queue.Upcoming), queuePageSize)
	page = clampPage(page, total)
	hasUpcoming := len(queue.Upcoming) > 0

	embed := discord.NewEmbed().
		WithTitle("Queue").
		WithColor(accentColor).
		WithDescription(queueModeDescription(mode))

	currentLine := "Nothing is playing right now."
	if queue.Current != nil {
		currentLine = truncateString(formatTrack(*queue.Current), discordEmbedFieldValueLimit)
		if queue.Current.Artwork != "" {
			embed = embed.WithThumbnail(queue.Current.Artwork)
		}
		if queue.Current.URL != "" {
			embed = embed.WithURL(queue.Current.URL)
		}
	}
	embed = embed.AddField("Now Playing", currentLine, false)
	embed = embed.AddField("Repeat", repeatModeLabel(queue.RepeatMode), true)

	if queue.Paused {
		embed = embed.AddField("Status", "Paused", true)
	} else {
		embed = embed.AddField("Status", "Playing", true)
	}

	if !hasUpcoming {
		embed = embed.
			AddField("Upcoming", "No tracks are waiting in the queue.", false).
			WithFooterText("Use /play or /search to add more music.")

		return pagination.RenderedPage{
			Page: pagination.Page{
				Embeds:  []discord.Embed{embed},
				Actions: buildQueueActionButtons(mode, queue.RepeatMode, false),
			},
			PageIndex:  0,
			TotalPages: 1,
		}
	}

	start, end := pageBounds(len(queue.Upcoming), page, queuePageSize)
	pageTracks := queue.Upcoming[start:end]
	embed = embed.
		AddField("Upcoming", queueSliceLines(queue.Upcoming, page, queuePageSize), false).
		WithFooterTextf("Page %d/%d • %d upcoming track(s)", page+1, total, len(queue.Upcoming))

	return pagination.RenderedPage{
		Page: pagination.Page{
			Embeds: []discord.Embed{embed},
			Select: &pagination.SelectMenu{
				Placeholder: queueSelectPlaceholder(mode),
				Options:     buildQueueSelectOptions(pageTracks, start),
				Disabled:    mode == queueActionModeBrowse,
			},
			Actions: buildQueueActionButtons(mode, queue.RepeatMode, true),
		},
		PageIndex:  page,
		TotalPages: total,
	}
}

func buildSearchSelectOptions(tracks []domainmusic.Track, start int) []discord.StringSelectMenuOption {
	options := make([]discord.StringSelectMenuOption, 0, len(tracks))
	for i, track := range tracks {
		label := truncateString(formatTrackLabel(track), 100)
		description := truncateString(fmt.Sprintf("%s • %s", formatDuration(track.Duration), safeFieldValue(track.Source)), 100)
		value := strconv.Itoa(start + i)
		options = append(options, discord.NewStringSelectMenuOption(label, value).WithDescription(description))
	}
	return options
}

func buildQueueSelectOptions(tracks []domainmusic.Track, start int) []discord.StringSelectMenuOption {
	options := make([]discord.StringSelectMenuOption, 0, len(tracks))
	for i, track := range tracks {
		position := start + i + 1
		label := truncateString(fmt.Sprintf("%02d. %s", position, formatTrackLabel(track)), 100)
		description := truncateString(fmt.Sprintf("%s • %s", formatDuration(track.Duration), safeFieldValue(track.Source)), 100)
		options = append(options, discord.NewStringSelectMenuOption(label, strconv.Itoa(start+i)).WithDescription(description))
	}
	return options
}

func buildQueueActionButtons(
	mode queueActionMode,
	repeatMode domainmusic.RepeatMode,
	hasUpcoming bool,
) []pagination.ActionButton {
	return []pagination.ActionButton{
		{
			ID:       string(queueActionModeBrowse),
			Label:    "Browse",
			Style:    discord.ButtonStyleSecondary,
			Disabled: mode == queueActionModeBrowse,
		},
		{
			ID:       string(queueActionModeJump),
			Label:    "Play Now",
			Style:    discord.ButtonStyleSuccess,
			Disabled: !hasUpcoming || mode == queueActionModeJump,
		},
		{
			ID:       string(queueActionModeMove),
			Label:    "Move",
			Style:    discord.ButtonStylePrimary,
			Disabled: !hasUpcoming || mode == queueActionModeMove,
		},
		{
			ID:       string(queueActionModeRemove),
			Label:    "Remove",
			Style:    discord.ButtonStyleDanger,
			Disabled: !hasUpcoming || mode == queueActionModeRemove,
		},
		{
			ID:    queueActionRefresh,
			Label: "Refresh",
			Style: discord.ButtonStyleSecondary,
		},
		{
			ID:       queueActionShuffle,
			Label:    "Shuffle",
			Style:    discord.ButtonStylePrimary,
			Disabled: !hasUpcoming,
		},
		{
			ID:       queueActionClear,
			Label:    "Clear Upcoming",
			Style:    discord.ButtonStyleDanger,
			Disabled: !hasUpcoming,
		},
		{
			ID:    queueActionRepeat,
			Label: "Repeat: " + repeatModeLabelShort(repeatMode),
			Style: discord.ButtonStyleSecondary,
		},
		{
			ID:       queueActionRemoveRange,
			Label:    "Remove Range",
			Style:    discord.ButtonStyleDanger,
			Disabled: !hasUpcoming,
		},
	}
}

func queueSelectPlaceholder(mode queueActionMode) string {
	switch mode {
	case queueActionModeJump:
		return "Choose a track to play now"
	case queueActionModeMove:
		return "Choose a track to move"
	case queueActionModeRemove:
		return "Choose a track to remove"
	default:
		return "Choose Play Now, Move, or Remove first"
	}
}

func queueModeDescription(mode queueActionMode) string {
	switch mode {
	case queueActionModeJump:
		return "Play Now mode: select an upcoming track to start it immediately."
	case queueActionModeMove:
		return "Move mode: select an upcoming track, then choose its new queue position."
	case queueActionModeRemove:
		return "Remove mode: select an upcoming track to delete it from the queue."
	default:
		return "Browse mode: use the buttons below to manage the queue, repeat mode, and bulk actions."
	}
}

func repeatModeLabel(mode domainmusic.RepeatMode) string {
	switch normalizeRepeatMode(mode) {
	case domainmusic.RepeatModeTrack:
		return "Repeat current track"
	case domainmusic.RepeatModeQueue:
		return "Repeat full queue"
	default:
		return "Off"
	}
}

func repeatModeLabelShort(mode domainmusic.RepeatMode) string {
	switch normalizeRepeatMode(mode) {
	case domainmusic.RepeatModeTrack:
		return "Track"
	case domainmusic.RepeatModeQueue:
		return "Queue"
	default:
		return "Off"
	}
}

func queueSliceLines(tracks []domainmusic.Track, page int, pageSize int) string {
	if len(tracks) == 0 {
		return "No tracks are waiting in the queue."
	}

	start, end := pageBounds(len(tracks), page, pageSize)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, truncateString(formatTrack(tracks[i]), queueTrackLineLimit)))
	}
	return joinLinesWithinLimit(lines, discordEmbedFieldValueLimit)
}

func searchResultsLines(tracks []domainmusic.Track, start int) string {
	if len(tracks) == 0 {
		return "No results found."
	}

	lines := make([]string, 0, len(tracks))
	for i, track := range tracks {
		lines = append(lines, fmt.Sprintf("`%02d` %s", start+i+1, truncateString(formatTrack(track), searchTrackLineLimit)))
	}
	return joinLinesWithinLimit(lines, discordEmbedDescriptionLimit)
}

func trackSummary(track domainmusic.Track) string {
	parts := []string{}
	if track.Author != "" {
		parts = append(parts, fmt.Sprintf("**Artist:** %s", track.Author))
	}
	if track.Title != "" {
		if track.URL != "" {
			parts = append(parts, fmt.Sprintf("**Track:** [%s](%s)", track.Title, track.URL))
		} else {
			parts = append(parts, fmt.Sprintf("**Track:** %s", track.Title))
		}
	}
	return strings.Join(parts, "\n")
}

func linkedTrackSummary(track domainmusic.Track) string {
	label := formatTrack(track)
	if track.URL == "" {
		return label
	}
	return fmt.Sprintf("[%s](%s)", label, track.URL)
}

func formatTrackLabel(track domainmusic.Track) string {
	if track.Author == "" {
		return track.Title
	}
	return track.Author + " - " + track.Title
}

func formatTrack(track domainmusic.Track) string {
	duration := formatDuration(track.Duration)
	if track.Author == "" {
		return fmt.Sprintf("%s (%s)", track.Title, duration)
	}
	return fmt.Sprintf("%s - %s (%s)", track.Author, track.Title, duration)
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "unknown duration"
	}

	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func safeFieldValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}

func totalPages(totalItems int, pageSize int) int {
	if pageSize <= 0 {
		return 1
	}
	if totalItems <= 0 {
		return 1
	}
	return (totalItems + pageSize - 1) / pageSize
}

func clampPage(page int, totalPages int) int {
	if totalPages <= 0 {
		return 0
	}
	if page < 0 {
		return 0
	}
	if page >= totalPages {
		return totalPages - 1
	}
	return page
}

func pageBounds(totalItems int, page int, pageSize int) (int, int) {
	if totalItems <= 0 {
		return 0, 0
	}
	page = clampPage(page, totalPages(totalItems, pageSize))
	start := page * pageSize
	if start > totalItems {
		start = totalItems
	}
	end := start + pageSize
	if end > totalItems {
		end = totalItems
	}
	return start, end
}

func truncateString(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}

	runes := []rune(value)
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func joinLinesWithinLimit(lines []string, maxRunes int) string {
	if len(lines) == 0 {
		return ""
	}
	if maxRunes <= 0 {
		return strings.Join(lines, "\n")
	}

	joined := make([]string, 0, len(lines))
	used := 0
	truncated := false

	for _, line := range lines {
		lineRunes := utf8.RuneCountInString(line)
		extra := lineRunes
		if len(joined) > 0 {
			extra++
		}
		if used+extra > maxRunes {
			truncated = true
			break
		}
		joined = append(joined, line)
		used += extra
	}

	if !truncated {
		return strings.Join(joined, "\n")
	}

	ellipsis := "..."
	if len(joined) == 0 {
		return truncateString(ellipsis, maxRunes)
	}

	lastIndex := len(joined) - 1
	lastLine := joined[lastIndex]
	remaining := maxRunes - (used - utf8.RuneCountInString(lastLine))
	if len(joined) > 1 {
		remaining--
	}
	joined[lastIndex] = truncateString(lastLine, max(remaining, 1))
	result := strings.Join(joined, "\n")
	if utf8.RuneCountInString(result) <= maxRunes {
		return result
	}
	return truncateString(result, maxRunes)
}

func normalizeRepeatMode(mode domainmusic.RepeatMode) domainmusic.RepeatMode {
	switch mode {
	case domainmusic.RepeatModeTrack, domainmusic.RepeatModeQueue:
		return mode
	default:
		return domainmusic.RepeatModeOff
	}
}
