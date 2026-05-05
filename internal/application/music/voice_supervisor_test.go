package musicapp

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"pluggercat/internal/adapters/store/memory"
	"pluggercat/internal/ports"
)

type fakeDiscordGateway struct {
	mu            sync.Mutex
	channelID     *string
	humanCount    int
	leaveCalls    int
	voiceHandlers []ports.VoiceStateHandler
}

func (f *fakeDiscordGateway) Open(context.Context) error {
	return nil
}

func (f *fakeDiscordGateway) Close(context.Context) {}

func (f *fakeDiscordGateway) SyncCommands(context.Context) error {
	return nil
}

func (f *fakeDiscordGateway) JoinVoiceChannel(context.Context, string, string) error {
	return nil
}

func (f *fakeDiscordGateway) LeaveVoiceChannel(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.leaveCalls++
	f.channelID = nil
	return nil
}

func (f *fakeDiscordGateway) CurrentVoiceChannelID(context.Context, string) (*string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.channelID == nil {
		return nil, nil
	}

	value := *f.channelID
	return &value, nil
}

func (f *fakeDiscordGateway) CountHumanMembersInVoiceChannel(context.Context, string, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.humanCount, nil
}

func (f *fakeDiscordGateway) AddVoiceStateHandler(handler ports.VoiceStateHandler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.voiceHandlers = append(f.voiceHandlers, handler)
}

func (f *fakeDiscordGateway) BotID() string {
	return "bot-user"
}

func (f *fakeDiscordGateway) LeaveCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leaveCalls
}

func (f *fakeDiscordGateway) SetHumanCount(humanCount int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.humanCount = humanCount
}

func TestVoiceSupervisorLeavesAfterQueueIdleTimeout(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	channelID := "voice-1"
	discord := &fakeDiscordGateway{
		channelID:  &channelID,
		humanCount: 1,
	}

	supervisor := NewVoiceSupervisor(discardLogger(), discord, service, 20*time.Millisecond, time.Minute)
	supervisor.OnQueueDrained("guild-1")

	waitUntil(t, 300*time.Millisecond, func() bool {
		return discord.LeaveCalls() == 1
	})

	if backend.stopped != 1 {
		t.Fatalf("expected stop to be called once, got %d", backend.stopped)
	}
}

func TestVoiceSupervisorCancelsQueueIdleDisconnectWhenPlaybackResumes(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	channelID := "voice-1"
	discord := &fakeDiscordGateway{
		channelID:  &channelID,
		humanCount: 1,
	}

	supervisor := NewVoiceSupervisor(discardLogger(), discord, service, 60*time.Millisecond, time.Minute)
	supervisor.OnQueueDrained("guild-1")

	time.Sleep(10 * time.Millisecond)
	supervisor.OnPlaybackStarted("guild-1")
	time.Sleep(90 * time.Millisecond)

	if discord.LeaveCalls() != 0 {
		t.Fatalf("expected queue idle disconnect to be canceled, got %d leave calls", discord.LeaveCalls())
	}
	if backend.stopped != 0 {
		t.Fatalf("expected stop not to be called, got %d", backend.stopped)
	}
}

func TestVoiceSupervisorLeavesAfterChannelStaysEmpty(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	channelID := "voice-1"
	discord := &fakeDiscordGateway{
		channelID:  &channelID,
		humanCount: 0,
	}

	supervisor := NewVoiceSupervisor(discardLogger(), discord, service, time.Minute, 20*time.Millisecond)
	supervisor.OnVoiceStateChanged(context.Background(), "guild-1")

	waitUntil(t, 300*time.Millisecond, func() bool {
		return discord.LeaveCalls() == 1
	})

	if backend.stopped != 1 {
		t.Fatalf("expected stop to be called once, got %d", backend.stopped)
	}
}

func TestVoiceSupervisorCancelsAloneDisconnectWhenSomeoneReturns(t *testing.T) {
	backend := &fakeAudioBackend{}
	service := NewService(memory.NewQueueRepository(), backend)

	channelID := "voice-1"
	discord := &fakeDiscordGateway{
		channelID:  &channelID,
		humanCount: 0,
	}

	supervisor := NewVoiceSupervisor(discardLogger(), discord, service, time.Minute, 60*time.Millisecond)
	supervisor.OnVoiceStateChanged(context.Background(), "guild-1")

	time.Sleep(10 * time.Millisecond)
	discord.SetHumanCount(1)
	supervisor.OnVoiceStateChanged(context.Background(), "guild-1")
	time.Sleep(90 * time.Millisecond)

	if discord.LeaveCalls() != 0 {
		t.Fatalf("expected alone disconnect to be canceled, got %d leave calls", discord.LeaveCalls())
	}
	if backend.stopped != 0 {
		t.Fatalf("expected stop not to be called, got %d", backend.stopped)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("condition was not met within %s", timeout)
}
