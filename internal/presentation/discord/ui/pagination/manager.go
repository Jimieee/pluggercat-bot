package pagination

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

const (
	Prefix             = "pager:"
	actionPrev         = "prev"
	actionNext         = "next"
	actionClose        = "close"
	actionSelect       = "select"
	actionCustomPrefix = "action:"
	defaultSessionTTL  = 15 * time.Minute
)

type SelectMenu struct {
	Placeholder string
	Options     []discord.StringSelectMenuOption
	Disabled    bool
}

type ActionButton struct {
	ID       string
	Label    string
	Style    discord.ButtonStyle
	Disabled bool
}

type Page struct {
	Content string
	Embeds  []discord.Embed
	Select  *SelectMenu
	Actions []ActionButton
}

type RenderedPage struct {
	Page       Page
	PageIndex  int
	TotalPages int
}

type InteractionResult struct {
	Update   *discord.MessageUpdate
	Close    bool
	Rerender bool
	Handled  bool
}

type Session struct {
	ID          string
	OwnerID     string
	ExpiresAt   time.Time
	CurrentPage int
	Render      func(page int) (RenderedPage, error)
	OnSelect    func(event *events.ComponentInteractionCreate, value string) (*InteractionResult, error)
	OnAction    func(event *events.ComponentInteractionCreate, actionID string) (*InteractionResult, error)
}

type Manager struct {
	logger *slog.Logger
	ttl    time.Duration

	mu       sync.Mutex
	sessions map[string]*Session
}

func New(logger *slog.Logger, ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}

	return &Manager{
		logger:   logger.With(slog.String("component", "pagination")),
		ttl:      ttl,
		sessions: map[string]*Session{},
	}
}

func (m *Manager) CreateMessage(session *Session) (discord.MessageCreate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.pruneExpiredLocked(now)

	if session.ID == "" {
		sessionID, err := newSessionID()
		if err != nil {
			return discord.MessageCreate{}, err
		}
		session.ID = sessionID
	}
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = now.Add(m.ttl)
	}

	rendered, err := m.renderLocked(session, session.CurrentPage)
	if err != nil {
		return discord.MessageCreate{}, err
	}

	cloned := *session
	m.sessions[session.ID] = &cloned

	return discord.NewMessageCreate().
		WithContent(rendered.Page.Content).
		WithEmbeds(rendered.Page.Embeds...).
		WithComponents(m.componentsForSessionLocked(&cloned, rendered)...), nil
}

func (m *Manager) HandleComponent(event *events.ComponentInteractionCreate) error {
	sessionID, action, ok := parseCustomID(event.Data.CustomID())
	if !ok {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This interaction is not valid anymore.").
				WithEphemeral(true),
		)
	}

	m.mu.Lock()
	now := time.Now()
	m.pruneExpiredLocked(now)

	session, ok := m.sessions[sessionID]
	if !ok || !now.Before(session.ExpiresAt) {
		if ok {
			delete(m.sessions, sessionID)
		}
		m.mu.Unlock()
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This view expired. Please run the command again.").
				WithEphemeral(true),
		)
	}

	if session.OwnerID != "" && event.User().ID.String() != session.OwnerID {
		m.mu.Unlock()
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("Only the user who opened this view can interact with it.").
				WithEphemeral(true),
		)
	}

	switch action {
	case actionPrev:
		rendered, err := m.renderLocked(session, session.CurrentPage-1)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		update := discord.NewMessageUpdate().
			WithContent(rendered.Page.Content).
			WithEmbeds(rendered.Page.Embeds...).
			WithComponents(m.componentsForSessionLocked(session, rendered)...)
		m.mu.Unlock()
		return event.UpdateMessage(update)

	case actionNext:
		rendered, err := m.renderLocked(session, session.CurrentPage+1)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		update := discord.NewMessageUpdate().
			WithContent(rendered.Page.Content).
			WithEmbeds(rendered.Page.Embeds...).
			WithComponents(m.componentsForSessionLocked(session, rendered)...)
		m.mu.Unlock()
		return event.UpdateMessage(update)

	case actionClose:
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return event.UpdateMessage(discord.NewMessageUpdate().ClearComponents())

	case actionSelect:
		handler := session.OnSelect
		if handler == nil {
			m.mu.Unlock()
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("There is nothing to select in this view.").
					WithEphemeral(true),
			)
		}
		values := event.StringSelectMenuInteractionData().Values
		m.mu.Unlock()

		if len(values) == 0 {
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("Please choose an option first.").
					WithEphemeral(true),
			)
		}

		result, err := handler(event, values[0])
		if err != nil {
			return err
		}
		return m.handleInteractionResult(event, sessionID, result)

	default:
		if strings.HasPrefix(action, actionCustomPrefix) {
			handler := session.OnAction
			if handler == nil {
				m.mu.Unlock()
				return event.CreateMessage(
					discord.NewMessageCreate().
						WithContent("This action is not available in this view.").
						WithEphemeral(true),
				)
			}

			actionID := strings.TrimPrefix(action, actionCustomPrefix)
			m.mu.Unlock()

			result, err := handler(event, actionID)
			if err != nil {
				return err
			}
			return m.handleInteractionResult(event, sessionID, result)
		}

		m.mu.Unlock()
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This interaction is not supported.").
				WithEphemeral(true),
		)
	}
}

func (m *Manager) RenderUpdate(sessionID string) (discord.MessageUpdate, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.pruneExpiredLocked(now)

	session, ok := m.sessions[sessionID]
	if !ok || !now.Before(session.ExpiresAt) {
		if ok {
			delete(m.sessions, sessionID)
		}
		return discord.MessageUpdate{}, false, nil
	}

	rendered, err := m.renderLocked(session, session.CurrentPage)
	if err != nil {
		return discord.MessageUpdate{}, true, err
	}

	update := discord.NewMessageUpdate().
		WithContent(rendered.Page.Content).
		WithEmbeds(rendered.Page.Embeds...).
		WithComponents(m.componentsForSessionLocked(session, rendered)...)
	return update, true, nil
}

func (m *Manager) renderLocked(session *Session, requestedPage int) (RenderedPage, error) {
	rendered, err := session.Render(requestedPage)
	if err != nil {
		return RenderedPage{}, err
	}

	if rendered.TotalPages <= 0 {
		rendered.TotalPages = 1
	}
	if rendered.PageIndex < 0 {
		rendered.PageIndex = 0
	}
	if rendered.PageIndex >= rendered.TotalPages {
		rendered.PageIndex = rendered.TotalPages - 1
	}

	session.CurrentPage = rendered.PageIndex
	session.ExpiresAt = time.Now().Add(m.ttl)
	return rendered, nil
}

func (m *Manager) componentsForSessionLocked(session *Session, rendered RenderedPage) []discord.LayoutComponent {
	components := make([]discord.LayoutComponent, 0, 4)

	if rendered.Page.Select != nil {
		selectComponent := discord.NewStringSelectMenu(
			customID(session.ID, actionSelect),
			rendered.Page.Select.Placeholder,
			rendered.Page.Select.Options...,
		).WithMinValues(1).WithMaxValues(1).WithDisabled(rendered.Page.Select.Disabled)
		components = append(components, discord.NewActionRow(selectComponent))
	}

	if len(rendered.Page.Actions) > 0 {
		for _, chunk := range chunkActionButtons(rendered.Page.Actions, 5) {
			buttons := make([]discord.InteractiveComponent, 0, len(chunk))
			for _, actionButton := range chunk {
				button := discord.NewButton(
					buttonStyleOrDefault(actionButton.Style),
					actionButton.Label,
					customID(session.ID, actionCustomPrefix+actionButton.ID),
					"",
					0,
				).WithDisabled(actionButton.Disabled)
				buttons = append(buttons, button)
			}
			components = append(components, discord.NewActionRow(buttons...))
		}
	}

	if rendered.TotalPages > 1 {
		pageLabel := fmt.Sprintf("%d/%d", rendered.PageIndex+1, rendered.TotalPages)
		controls := discord.NewActionRow(
			discord.NewSecondaryButton("Prev", customID(session.ID, actionPrev)).WithDisabled(rendered.PageIndex == 0),
			discord.NewSecondaryButton(pageLabel, customID(session.ID, "page")).WithDisabled(true),
			discord.NewSecondaryButton("Next", customID(session.ID, actionNext)).WithDisabled(rendered.PageIndex >= rendered.TotalPages-1),
			discord.NewDangerButton("Close", customID(session.ID, actionClose)),
		)
		components = append(components, controls)
		return components
	}

	if rendered.Page.Select != nil || len(rendered.Page.Actions) > 0 {
		components = append(components, discord.NewActionRow(
			discord.NewDangerButton("Close", customID(session.ID, actionClose)),
		))
	}

	return components
}

func (m *Manager) handleInteractionResult(
	event *events.ComponentInteractionCreate,
	sessionID string,
	result *InteractionResult,
) error {
	if result == nil {
		return event.DeferUpdateMessage()
	}
	if result.Handled {
		return nil
	}

	if result.Close {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
	}

	if result.Update != nil {
		update := *result.Update
		if result.Close {
			update = update.ClearComponents()
		}
		return event.UpdateMessage(update)
	}

	if result.Close {
		return event.UpdateMessage(discord.NewMessageUpdate().ClearComponents())
	}

	if result.Rerender {
		update, ok, err := m.RenderUpdate(sessionID)
		if err != nil {
			return err
		}
		if !ok {
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("This view expired. Please run the command again.").
					WithEphemeral(true),
			)
		}
		return event.UpdateMessage(update)
	}

	return event.DeferUpdateMessage()
}

func (m *Manager) pruneExpiredLocked(now time.Time) {
	for id, session := range m.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(m.sessions, id)
		}
	}
}

func customID(sessionID string, action string) string {
	return Prefix + sessionID + ":" + action
}

func parseCustomID(value string) (sessionID string, action string, ok bool) {
	if !strings.HasPrefix(value, Prefix) {
		return "", "", false
	}

	rest := strings.TrimPrefix(value, Prefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func newSessionID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func chunkActionButtons(actions []ActionButton, size int) [][]ActionButton {
	if len(actions) == 0 || size <= 0 {
		return nil
	}

	chunks := make([][]ActionButton, 0, (len(actions)+size-1)/size)
	for start := 0; start < len(actions); start += size {
		end := start + size
		if end > len(actions) {
			end = len(actions)
		}
		chunks = append(chunks, actions[start:end])
	}
	return chunks
}

func buttonStyleOrDefault(style discord.ButtonStyle) discord.ButtonStyle {
	if style == 0 {
		return discord.ButtonStyleSecondary
	}
	return style
}
