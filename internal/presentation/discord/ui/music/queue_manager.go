package musicui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"

	musicapp "pluggercat/internal/application/music"
	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/presentation/discord/ui/pagination"
)

const (
	QueueModalPrefix            = "queue-modal:"
	queueActionRefresh          = "refresh"
	queueActionShuffle          = "shuffle"
	queueActionClear            = "clear"
	queueActionRepeat           = "repeat"
	queueActionRemoveRange      = "remove-range"
	queueMoveModalAction        = "move"
	queueMoveModalPositionField = "destination"
	queueRangeStartField        = "start"
	queueRangeEndField          = "end"
	queueRequestTTL             = 15 * time.Minute
)

type QueueManager struct {
	service         *musicapp.Service
	voiceSupervisor *musicapp.VoiceSupervisor
	pager           *pagination.Manager

	mu              sync.Mutex
	pendingRequests map[string]pendingQueueRequest
}

type queueSessionController struct {
	manager *QueueManager
	guildID string
	ownerID string
	mode    queueActionMode
}

type queueModalRequest struct {
	Action string
	Token  string
}

type pendingQueueRequest struct {
	Action     string
	SessionID  string
	GuildID    string
	OwnerID    string
	TrackKey   string
	Occurrence int
	CreatedAt  time.Time
}

func NewQueueManager(
	service *musicapp.Service,
	voiceSupervisor *musicapp.VoiceSupervisor,
	pager *pagination.Manager,
) *QueueManager {
	return &QueueManager{
		service:         service,
		voiceSupervisor: voiceSupervisor,
		pager:           pager,
		pendingRequests: map[string]pendingQueueRequest{},
	}
}

func (m *QueueManager) NewSession(guildID string, ownerID string) *pagination.Session {
	controller := &queueSessionController{
		manager: m,
		guildID: guildID,
		ownerID: ownerID,
		mode:    queueActionModeBrowse,
	}

	session := &pagination.Session{
		OwnerID: ownerID,
	}
	session.Render = controller.render
	session.OnAction = func(event *events.ComponentInteractionCreate, actionID string) (*pagination.InteractionResult, error) {
		return controller.onAction(session, event, actionID)
	}
	session.OnSelect = func(event *events.ComponentInteractionCreate, value string) (*pagination.InteractionResult, error) {
		return controller.onSelect(session, event, value)
	}
	return session
}

func (m *QueueManager) HandleModal(event *events.ModalSubmitInteractionCreate) error {
	request, ok := parseQueueModalRequest(event.Data.CustomID)
	if !ok {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This queue form is no longer valid.").
				WithEphemeral(true),
		)
	}

	pendingRequest, ok := m.takePendingRequest(request.Token)
	if !ok {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This queue request expired. Please open `/queue` again.").
				WithEphemeral(true),
		)
	}

	if request.Action != pendingRequest.Action {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This queue form no longer matches the requested action.").
				WithEphemeral(true),
		)
	}

	if event.User().ID.String() != pendingRequest.OwnerID {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("Only the user who opened this queue view can submit this form.").
				WithEphemeral(true),
		)
	}

	switch pendingRequest.Action {
	case queueMoveModalAction:
		return m.handleMoveModal(event, pendingRequest)
	case queueActionRemoveRange:
		return m.handleRemoveRangeModal(event, pendingRequest)
	default:
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("This queue form action is not supported.").
				WithEphemeral(true),
		)
	}
}

func (c *queueSessionController) render(page int) (pagination.RenderedPage, error) {
	queue, err := c.manager.service.Queue(context.Background(), c.guildID)
	if err != nil {
		return pagination.RenderedPage{}, err
	}

	return buildQueuePage(queue, page, c.mode), nil
}

func (c *queueSessionController) onAction(
	session *pagination.Session,
	event *events.ComponentInteractionCreate,
	actionID string,
) (*pagination.InteractionResult, error) {
	switch actionID {
	case string(queueActionModeBrowse):
		c.mode = queueActionModeBrowse
		return &pagination.InteractionResult{Rerender: true}, nil

	case string(queueActionModeJump):
		c.mode = queueActionModeJump
		return &pagination.InteractionResult{Rerender: true}, nil

	case string(queueActionModeMove):
		c.mode = queueActionModeMove
		return &pagination.InteractionResult{Rerender: true}, nil

	case string(queueActionModeRemove):
		c.mode = queueActionModeRemove
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionRefresh:
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionShuffle:
		updatedQueue, err := c.manager.service.ShuffleUpcomingTracks(context.Background(), c.guildID)
		if err != nil {
			return c.handledEphemeral(event, "I could not shuffle the queue right now.")
		}
		if updatedQueue.Current == nil && len(updatedQueue.Upcoming) == 0 {
			c.manager.voiceSupervisor.OnQueueDrained(c.guildID)
		}
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionClear:
		_, updatedQueue, err := c.manager.service.ClearUpcomingTracks(context.Background(), c.guildID)
		if err != nil {
			return c.handledEphemeral(event, "I could not clear the queue right now.")
		}
		if updatedQueue.Current == nil && len(updatedQueue.Upcoming) == 0 {
			c.manager.voiceSupervisor.OnQueueDrained(c.guildID)
		}
		c.mode = queueActionModeBrowse
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionRepeat:
		_, err := c.manager.service.CycleRepeatMode(context.Background(), c.guildID)
		if err != nil {
			return c.handledEphemeral(event, "I could not change the repeat mode right now.")
		}
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionRemoveRange:
		requestToken, err := c.manager.storePendingRequest(pendingQueueRequest{
			Action:    queueActionRemoveRange,
			SessionID: session.ID,
			GuildID:   c.guildID,
			OwnerID:   c.ownerID,
		})
		if err != nil {
			return nil, err
		}

		modal := discord.NewModalCreate(
			buildQueueModalID(queueActionRemoveRange, requestToken),
			"Remove Range",
		).
			AddLabel(
				"Start position",
				discord.NewShortTextInput(queueRangeStartField).
					WithPlaceholder("1"),
			).
			AddLabel(
				"End position",
				discord.NewShortTextInput(queueRangeEndField).
					WithPlaceholder("3"),
			)

		if err := event.Modal(modal); err != nil {
			return nil, err
		}
		return &pagination.InteractionResult{Handled: true}, nil

	default:
		return c.handledEphemeral(event, "That queue action is not available anymore.")
	}
}

func (c *queueSessionController) onSelect(
	session *pagination.Session,
	event *events.ComponentInteractionCreate,
	value string,
) (*pagination.InteractionResult, error) {
	queueIndex, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || queueIndex < 0 {
		return c.handledEphemeral(event, "That queue entry is not valid anymore.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch c.mode {
	case queueActionModeBrowse:
		return c.handledEphemeral(event, "Choose `Play Now`, `Move`, or `Remove` first.")

	case queueActionModeJump:
		_, _, err := c.manager.service.JumpToUpcomingTrack(ctx, c.guildID, queueIndex)
		if err != nil {
			if errors.Is(err, musicapp.ErrQueuePositionOutOfRange) {
				return c.handledEphemeral(event, "That queue entry is outside the current queue.")
			}
			return c.handledEphemeral(event, "I could not start that queued track right now.")
		}
		c.manager.voiceSupervisor.OnPlaybackStarted(c.guildID)
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionModeRemove:
		_, updatedQueue, err := c.manager.service.RemoveUpcomingTrack(ctx, c.guildID, queueIndex)
		if err != nil {
			if errors.Is(err, musicapp.ErrQueuePositionOutOfRange) {
				return c.handledEphemeral(event, "That queue entry is outside the current queue.")
			}
			return c.handledEphemeral(event, "I could not remove that queued track right now.")
		}
		if updatedQueue.Current == nil && len(updatedQueue.Upcoming) == 0 {
			c.manager.voiceSupervisor.OnQueueDrained(c.guildID)
		}
		return &pagination.InteractionResult{Rerender: true}, nil

	case queueActionModeMove:
		queue, err := c.manager.service.Queue(ctx, c.guildID)
		if err != nil {
			return c.handledEphemeral(event, "I could not refresh the queue before moving that track.")
		}

		trackKey, occurrence, ok := findTrackOccurrence(queue.Upcoming, queueIndex)
		if !ok {
			return c.handledEphemeral(event, "That queue entry is not valid anymore.")
		}

		moveToken, err := c.manager.storePendingRequest(pendingQueueRequest{
			Action:     queueMoveModalAction,
			SessionID:  session.ID,
			GuildID:    c.guildID,
			OwnerID:    c.ownerID,
			TrackKey:   trackKey,
			Occurrence: occurrence,
		})
		if err != nil {
			return nil, err
		}

		modal := discord.NewModalCreate(
			buildQueueModalID(queueMoveModalAction, moveToken),
			"Move Track",
		).AddLabel(
			"New queue position",
			discord.NewShortTextInput(queueMoveModalPositionField).
				WithPlaceholder("Enter a number like 1").
				WithValue(strconv.Itoa(queueIndex+1)),
		)
		if err := event.Modal(modal); err != nil {
			return nil, err
		}
		return &pagination.InteractionResult{Handled: true}, nil

	default:
		return c.handledEphemeral(event, "That queue action is not available anymore.")
	}
}

func (c *queueSessionController) handledEphemeral(
	event *events.ComponentInteractionCreate,
	content string,
) (*pagination.InteractionResult, error) {
	if err := event.CreateMessage(
		discord.NewMessageCreate().
			WithContent(content).
			WithEphemeral(true),
	); err != nil {
		return nil, err
	}
	return &pagination.InteractionResult{Handled: true}, nil
}

func (m *QueueManager) handleMoveModal(
	event *events.ModalSubmitInteractionCreate,
	request pendingQueueRequest,
) error {
	destinationValue := strings.TrimSpace(event.Data.Text(queueMoveModalPositionField))
	destinationPosition, err := strconv.Atoi(destinationValue)
	if err != nil || destinationPosition <= 0 {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("Enter a valid queue position starting at 1.").
				WithEphemeral(true),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queue, err := m.service.Queue(ctx, request.GuildID)
	if err != nil {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("I could not refresh the queue before moving that track.").
				WithEphemeral(true),
		)
	}

	sourceIndex, ok := findUpcomingIndexByOccurrence(queue.Upcoming, request.TrackKey, request.Occurrence)
	if !ok {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("That track is no longer at a movable spot in the queue. Please open `/queue` again.").
				WithEphemeral(true),
		)
	}

	_, err = m.service.MoveUpcomingTrack(ctx, request.GuildID, sourceIndex, destinationPosition-1)
	if err != nil {
		if errors.Is(err, musicapp.ErrQueuePositionOutOfRange) {
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("That queue position is outside the current queue.").
					WithEphemeral(true),
			)
		}
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("I could not move that track right now.").
				WithEphemeral(true),
		)
	}

	return m.refreshQueueView(event, request.SessionID)
}

func (m *QueueManager) handleRemoveRangeModal(
	event *events.ModalSubmitInteractionCreate,
	request pendingQueueRequest,
) error {
	startValue := strings.TrimSpace(event.Data.Text(queueRangeStartField))
	endValue := strings.TrimSpace(event.Data.Text(queueRangeEndField))

	startPosition, err := strconv.Atoi(startValue)
	if err != nil || startPosition <= 0 {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("The start position must be a valid number starting at 1.").
				WithEphemeral(true),
		)
	}

	endPosition, err := strconv.Atoi(endValue)
	if err != nil || endPosition <= 0 {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("The end position must be a valid number starting at 1.").
				WithEphemeral(true),
		)
	}

	if endPosition < startPosition {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("The end position must be greater than or equal to the start position.").
				WithEphemeral(true),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, updatedQueue, err := m.service.RemoveUpcomingRange(ctx, request.GuildID, startPosition-1, endPosition-1)
	if err != nil {
		if errors.Is(err, musicapp.ErrQueueRangeInvalid) {
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("That range is outside the current queue.").
					WithEphemeral(true),
			)
		}
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("I could not remove that range right now.").
				WithEphemeral(true),
		)
	}

	if updatedQueue.Current == nil && len(updatedQueue.Upcoming) == 0 {
		m.voiceSupervisor.OnQueueDrained(request.GuildID)
	}

	return m.refreshQueueView(event, request.SessionID)
}

func (m *QueueManager) refreshQueueView(
	event *events.ModalSubmitInteractionCreate,
	sessionID string,
) error {
	update, ok, err := m.pager.RenderUpdate(sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("That queue view expired. Please run `/queue` again.").
				WithEphemeral(true),
		)
	}

	return event.UpdateMessage(update)
}

func buildQueueModalID(action string, token string) string {
	return fmt.Sprintf("%s%s:%s", QueueModalPrefix, action, token)
}

func parseQueueModalRequest(customID string) (queueModalRequest, bool) {
	if !strings.HasPrefix(customID, QueueModalPrefix) {
		return queueModalRequest{}, false
	}

	parts := strings.Split(customID, ":")
	if len(parts) != 3 || strings.TrimSpace(parts[2]) == "" {
		return queueModalRequest{}, false
	}

	return queueModalRequest{
		Action: parts[1],
		Token:  parts[2],
	}, true
}

func (m *QueueManager) storePendingRequest(request pendingQueueRequest) (string, error) {
	tokenBytes := make([]byte, 8)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate queue request token: %w", err)
	}

	token := hex.EncodeToString(tokenBytes)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.prunePendingRequestsLocked(now)
	request.CreatedAt = now
	m.pendingRequests[token] = request
	return token, nil
}

func (m *QueueManager) takePendingRequest(token string) (pendingQueueRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.prunePendingRequestsLocked(now)

	request, ok := m.pendingRequests[token]
	if !ok {
		return pendingQueueRequest{}, false
	}

	delete(m.pendingRequests, token)
	return request, true
}

func (m *QueueManager) prunePendingRequestsLocked(now time.Time) {
	for token, request := range m.pendingRequests {
		if now.Sub(request.CreatedAt) >= queueRequestTTL {
			delete(m.pendingRequests, token)
		}
	}
}

func findTrackOccurrence(tracks []domainmusic.Track, index int) (string, int, bool) {
	if index < 0 || index >= len(tracks) {
		return "", 0, false
	}

	selectedKey := trackKey(tracks[index])
	occurrence := 0
	for i := 0; i <= index; i++ {
		if trackKey(tracks[i]) == selectedKey {
			occurrence++
		}
	}

	return selectedKey, occurrence, true
}

func findUpcomingIndexByOccurrence(tracks []domainmusic.Track, key string, occurrence int) (int, bool) {
	if occurrence <= 0 || strings.TrimSpace(key) == "" {
		return 0, false
	}

	seen := 0
	for index, track := range tracks {
		if trackKey(track) != key {
			continue
		}
		seen++
		if seen == occurrence {
			return index, true
		}
	}

	return 0, false
}

func trackKey(track domainmusic.Track) string {
	switch {
	case strings.TrimSpace(track.ID) != "":
		return "id:" + strings.TrimSpace(track.ID)
	case strings.TrimSpace(track.Reference) != "":
		return "ref:" + strings.TrimSpace(track.Reference)
	default:
		return "meta:" + strings.TrimSpace(track.Author) + "|" + strings.TrimSpace(track.Title)
	}
}
