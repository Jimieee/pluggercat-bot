package musicapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	domainmusic "pluggercat/internal/domain/music"
)

const (
	autocompleteDefaultLimit   = 10
	autocompleteMaxChoices     = 25
	autocompleteMinQueryRunes  = 3
	autocompleteSearchCacheTTL = 12 * time.Second
	autocompleteSelectionTTL   = 15 * time.Minute
	autocompleteThrottleWindow = 350 * time.Millisecond
	autocompleteRecentStateTTL = 2 * time.Second
	selectionTokenPrefix       = "pcat:"
)

type SearchSuggestion struct {
	Label string
	Value string
}

type trackSearchFunc func(ctx context.Context, query string) ([]domainmusic.Track, error)

type searchAssistant struct {
	mu             sync.Mutex
	cachedByQuery  map[string]cachedSuggestions
	selectedTracks map[string]selectedTrack
	recentByUserID map[string]recentSuggestionState
}

type cachedSuggestions struct {
	suggestions []SearchSuggestion
	expiresAt   time.Time
}

type selectedTrack struct {
	track     domainmusic.Track
	expiresAt time.Time
}

type recentSuggestionState struct {
	normalizedQuery string
	suggestions     []SearchSuggestion
	recordedAt      time.Time
}

func newSearchAssistant() *searchAssistant {
	return &searchAssistant{
		cachedByQuery:  map[string]cachedSuggestions{},
		selectedTracks: map[string]selectedTrack{},
		recentByUserID: map[string]recentSuggestionState{},
	}
}

func (a *searchAssistant) Suggest(
	ctx context.Context,
	userID string,
	query string,
	limit int,
	search trackSearchFunc,
) ([]SearchSuggestion, error) {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" || a.IsSelectionToken(trimmedQuery) {
		return nil, nil
	}
	if isDirectURL(trimmedQuery) {
		return nil, nil
	}
	if utf8.RuneCountInString(trimmedQuery) < autocompleteMinQueryRunes {
		return nil, nil
	}

	limit = clampSuggestionLimit(limit)
	normalizedQuery := normalizeSuggestionCacheKey(trimmedQuery)
	now := time.Now()

	a.mu.Lock()
	a.pruneExpiredLocked(now)

	if cached, ok := a.cachedByQuery[normalizedQuery]; ok && now.Before(cached.expiresAt) {
		suggestions := copySuggestions(cached.suggestions, limit)
		a.rememberRecentLocked(userID, normalizedQuery, suggestions, now)
		a.mu.Unlock()
		return suggestions, nil
	}
	if suggestions, ok := a.reuseCachedPrefixSuggestionsLocked(userID, normalizedQuery, limit, now); ok {
		a.mu.Unlock()
		return suggestions, nil
	}

	if suggestions, ok := a.reuseRecentSuggestionsLocked(userID, normalizedQuery, limit, now); ok {
		a.mu.Unlock()
		return suggestions, nil
	}
	a.mu.Unlock()

	tracks, err := search(ctx, trimmedQuery)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, nil
	}

	suggestions := make([]SearchSuggestion, 0, min(limit, len(tracks)))

	a.mu.Lock()
	defer a.mu.Unlock()

	now = time.Now()
	a.pruneExpiredLocked(now)

	for _, track := range tracks {
		if len(suggestions) == limit {
			break
		}

		token, err := a.storeSelectedTrackLocked(track, now)
		if err != nil {
			return nil, err
		}

		suggestions = append(suggestions, SearchSuggestion{
			Label: formatSuggestionLabel(track),
			Value: token,
		})
	}

	if len(suggestions) == 0 {
		return nil, nil
	}

	a.cachedByQuery[normalizedQuery] = cachedSuggestions{
		suggestions: copySuggestions(suggestions, len(suggestions)),
		expiresAt:   now.Add(autocompleteSearchCacheTTL),
	}
	a.rememberRecentLocked(userID, normalizedQuery, suggestions, now)

	return copySuggestions(suggestions, len(suggestions)), nil
}

func (a *searchAssistant) ResolveSelection(query string) (domainmusic.Track, bool) {
	trimmedQuery := strings.TrimSpace(query)
	if !a.IsSelectionToken(trimmedQuery) {
		return domainmusic.Track{}, false
	}

	now := time.Now()

	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneExpiredLocked(now)

	selected, ok := a.selectedTracks[trimmedQuery]
	if !ok || !now.Before(selected.expiresAt) {
		return domainmusic.Track{}, false
	}

	return selected.track, true
}

func (a *searchAssistant) IsSelectionToken(query string) bool {
	return strings.HasPrefix(strings.TrimSpace(query), selectionTokenPrefix)
}

func (a *searchAssistant) storeSelectedTrackLocked(track domainmusic.Track, now time.Time) (string, error) {
	tokenBytes := make([]byte, 8)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate autocomplete token: %w", err)
	}

	token := selectionTokenPrefix + hex.EncodeToString(tokenBytes)
	a.selectedTracks[token] = selectedTrack{
		track:     track,
		expiresAt: now.Add(autocompleteSelectionTTL),
	}
	return token, nil
}

func (a *searchAssistant) reuseRecentSuggestionsLocked(
	userID string,
	normalizedQuery string,
	limit int,
	now time.Time,
) ([]SearchSuggestion, bool) {
	if strings.TrimSpace(userID) == "" {
		return nil, false
	}

	recent, ok := a.recentByUserID[userID]
	if !ok || now.Sub(recent.recordedAt) > autocompleteThrottleWindow {
		return nil, false
	}
	if recent.normalizedQuery == normalizedQuery {
		suggestions := copySuggestions(recent.suggestions, limit)
		a.rememberRecentLocked(userID, normalizedQuery, suggestions, now)
		return suggestions, true
	}
	if !strings.HasPrefix(normalizedQuery, recent.normalizedQuery) {
		return nil, false
	}

	filtered := filterSuggestions(recent.suggestions, normalizedQuery, limit)
	if len(filtered) == 0 {
		return nil, false
	}

	a.rememberRecentLocked(userID, normalizedQuery, filtered, now)
	return filtered, true
}

func (a *searchAssistant) reuseCachedPrefixSuggestionsLocked(
	userID string,
	normalizedQuery string,
	limit int,
	now time.Time,
) ([]SearchSuggestion, bool) {
	bestKey := ""
	var filtered []SearchSuggestion

	for key, cached := range a.cachedByQuery {
		if !now.Before(cached.expiresAt) {
			continue
		}
		if key == normalizedQuery || !strings.HasPrefix(normalizedQuery, key) {
			continue
		}

		candidate := filterSuggestions(cached.suggestions, normalizedQuery, limit)
		if len(candidate) == 0 {
			continue
		}
		if len(key) <= len(bestKey) {
			continue
		}

		bestKey = key
		filtered = candidate
	}

	if len(filtered) == 0 {
		return nil, false
	}

	copied := copySuggestions(filtered, len(filtered))
	a.cachedByQuery[normalizedQuery] = cachedSuggestions{
		suggestions: copied,
		expiresAt:   now.Add(autocompleteSearchCacheTTL),
	}
	a.rememberRecentLocked(userID, normalizedQuery, copied, now)
	return copySuggestions(copied, limit), true
}

func (a *searchAssistant) rememberRecentLocked(userID string, normalizedQuery string, suggestions []SearchSuggestion, now time.Time) {
	if strings.TrimSpace(userID) == "" {
		return
	}

	a.recentByUserID[userID] = recentSuggestionState{
		normalizedQuery: normalizedQuery,
		suggestions:     copySuggestions(suggestions, len(suggestions)),
		recordedAt:      now,
	}
}

func (a *searchAssistant) pruneExpiredLocked(now time.Time) {
	for key, cached := range a.cachedByQuery {
		if !now.Before(cached.expiresAt) {
			delete(a.cachedByQuery, key)
		}
	}

	for token, selected := range a.selectedTracks {
		if !now.Before(selected.expiresAt) {
			delete(a.selectedTracks, token)
		}
	}

	for userID, recent := range a.recentByUserID {
		if now.Sub(recent.recordedAt) > autocompleteRecentStateTTL {
			delete(a.recentByUserID, userID)
		}
	}
}

func clampSuggestionLimit(limit int) int {
	if limit <= 0 {
		return autocompleteDefaultLimit
	}
	if limit > autocompleteMaxChoices {
		return autocompleteMaxChoices
	}
	return limit
}

func copySuggestions(suggestions []SearchSuggestion, limit int) []SearchSuggestion {
	if len(suggestions) == 0 || limit <= 0 {
		return nil
	}

	if limit > len(suggestions) {
		limit = len(suggestions)
	}

	copied := make([]SearchSuggestion, limit)
	copy(copied, suggestions[:limit])
	return copied
}

func filterSuggestions(suggestions []SearchSuggestion, normalizedQuery string, limit int) []SearchSuggestion {
	filtered := make([]SearchSuggestion, 0, min(limit, len(suggestions)))
	for _, suggestion := range suggestions {
		if len(filtered) == limit {
			break
		}

		if strings.Contains(normalizeSuggestionMatch(suggestion.Label), normalizedQuery) {
			filtered = append(filtered, suggestion)
		}
	}
	return filtered
}

func formatSuggestionLabel(track domainmusic.Track) string {
	label := track.Title
	if track.Author != "" {
		label = track.Author + " - " + track.Title
	}
	if track.Source != "" {
		label += " [" + track.Source + "]"
	}
	return truncateString(label, 100)
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

func normalizeSuggestionCacheKey(query string) string {
	trimmed := strings.TrimSpace(query)
	if isDirectURL(trimmed) {
		return trimmed
	}
	return normalizeSuggestionMatch(trimmed)
}

func normalizeSuggestionMatch(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isDirectURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
