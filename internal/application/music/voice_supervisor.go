package musicapp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"pluggercat/internal/ports"
)

type VoiceSupervisor struct {
	logger                   *slog.Logger
	discord                  ports.DiscordGateway
	service                  *Service
	queueIdleDisconnectAfter time.Duration
	aloneDisconnectAfter     time.Duration

	mu     sync.Mutex
	timers map[string]*guildTimers
}

type guildTimers struct {
	queueIdle *time.Timer
	alone     *time.Timer
}

func NewVoiceSupervisor(
	logger *slog.Logger,
	discord ports.DiscordGateway,
	service *Service,
	queueIdleDisconnectAfter time.Duration,
	aloneDisconnectAfter time.Duration,
) *VoiceSupervisor {
	return &VoiceSupervisor{
		logger:                   logger.With(slog.String("component", "voice_supervisor")),
		discord:                  discord,
		service:                  service,
		queueIdleDisconnectAfter: queueIdleDisconnectAfter,
		aloneDisconnectAfter:     aloneDisconnectAfter,
		timers:                   map[string]*guildTimers{},
	}
}

func (s *VoiceSupervisor) OnPlaybackStarted(guildID string) {
	s.cancelQueueIdleTimer(guildID)
}

func (s *VoiceSupervisor) OnQueueDrained(guildID string) {
	if s.queueIdleDisconnectAfter <= 0 {
		return
	}

	s.mu.Lock()
	timers := s.ensureGuildTimersLocked(guildID)
	if timers.queueIdle != nil {
		s.mu.Unlock()
		return
	}
	timers.queueIdle = time.AfterFunc(s.queueIdleDisconnectAfter, func() {
		s.handleQueueIdleTimeout(guildID)
	})
	s.mu.Unlock()

	s.logger.Info(
		"scheduled queue idle disconnect",
		slog.String("guild_id", guildID),
		slog.Duration("after", s.queueIdleDisconnectAfter),
	)
}

func (s *VoiceSupervisor) OnVoiceStateChanged(ctx context.Context, guildID string) {
	channelID, err := s.discord.CurrentVoiceChannelID(ctx, guildID)
	if err != nil {
		s.logger.Error("failed to read bot voice state", slog.String("guild_id", guildID), slog.Any("err", err))
		return
	}
	if channelID == nil {
		s.OnDisconnected(guildID)
		return
	}

	humans, err := s.discord.CountHumanMembersInVoiceChannel(ctx, guildID, *channelID)
	if err != nil {
		s.logger.Error("failed to count humans in voice channel", slog.String("guild_id", guildID), slog.Any("err", err))
		return
	}

	if humans == 0 {
		s.scheduleAloneDisconnect(guildID)
		return
	}

	s.cancelAloneTimer(guildID)
}

func (s *VoiceSupervisor) OnDisconnected(guildID string) {
	s.cancelQueueIdleTimer(guildID)
	s.cancelAloneTimer(guildID)
}

func (s *VoiceSupervisor) scheduleAloneDisconnect(guildID string) {
	if s.aloneDisconnectAfter <= 0 {
		return
	}

	s.mu.Lock()
	timers := s.ensureGuildTimersLocked(guildID)
	if timers.alone != nil {
		s.mu.Unlock()
		return
	}
	timers.alone = time.AfterFunc(s.aloneDisconnectAfter, func() {
		s.handleAloneTimeout(guildID)
	})
	s.mu.Unlock()

	s.logger.Info(
		"scheduled alone disconnect",
		slog.String("guild_id", guildID),
		slog.Duration("after", s.aloneDisconnectAfter),
	)
}

func (s *VoiceSupervisor) handleQueueIdleTimeout(guildID string) {
	s.clearQueueIdleTimer(guildID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queue, err := s.service.Queue(ctx, guildID)
	if err != nil {
		s.logger.Error("failed to inspect queue before idle disconnect", slog.String("guild_id", guildID), slog.Any("err", err))
		return
	}
	if queue.Current != nil || len(queue.Upcoming) > 0 {
		return
	}

	channelID, err := s.discord.CurrentVoiceChannelID(ctx, guildID)
	if err != nil {
		s.logger.Error("failed to inspect bot voice channel before idle disconnect", slog.String("guild_id", guildID), slog.Any("err", err))
		return
	}
	if channelID == nil {
		s.OnDisconnected(guildID)
		return
	}

	s.leaveAndClear(ctx, guildID, "queue idle timeout reached")
}

func (s *VoiceSupervisor) handleAloneTimeout(guildID string) {
	s.clearAloneTimer(guildID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	channelID, err := s.discord.CurrentVoiceChannelID(ctx, guildID)
	if err != nil {
		s.logger.Error("failed to inspect bot voice channel before alone disconnect", slog.String("guild_id", guildID), slog.Any("err", err))
		return
	}
	if channelID == nil {
		s.OnDisconnected(guildID)
		return
	}

	humans, err := s.discord.CountHumanMembersInVoiceChannel(ctx, guildID, *channelID)
	if err != nil {
		s.logger.Error("failed to recount humans before alone disconnect", slog.String("guild_id", guildID), slog.Any("err", err))
		return
	}
	if humans > 0 {
		return
	}

	s.leaveAndClear(ctx, guildID, "channel stayed empty of humans")
}

func (s *VoiceSupervisor) leaveAndClear(ctx context.Context, guildID string, reason string) {
	if err := s.service.StopAndClear(ctx, guildID); err != nil {
		s.logger.Error(
			"failed to stop and clear queue during auto disconnect",
			slog.String("guild_id", guildID),
			slog.String("reason", reason),
			slog.Any("err", err),
		)
		return
	}
	if err := s.discord.LeaveVoiceChannel(ctx, guildID); err != nil {
		s.logger.Error(
			"failed to leave voice channel during auto disconnect",
			slog.String("guild_id", guildID),
			slog.String("reason", reason),
			slog.Any("err", err),
		)
		return
	}

	s.OnDisconnected(guildID)
	s.logger.Info("auto disconnected from voice", slog.String("guild_id", guildID), slog.String("reason", reason))
}

func (s *VoiceSupervisor) cancelQueueIdleTimer(guildID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopQueueIdleTimerLocked(guildID)
}

func (s *VoiceSupervisor) cancelAloneTimer(guildID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopAloneTimerLocked(guildID)
}

func (s *VoiceSupervisor) clearQueueIdleTimer(guildID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if timers, ok := s.timers[guildID]; ok {
		timers.queueIdle = nil
		s.cleanupGuildTimersLocked(guildID)
	}
}

func (s *VoiceSupervisor) clearAloneTimer(guildID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if timers, ok := s.timers[guildID]; ok {
		timers.alone = nil
		s.cleanupGuildTimersLocked(guildID)
	}
}

func (s *VoiceSupervisor) stopQueueIdleTimerLocked(guildID string) {
	if timers, ok := s.timers[guildID]; ok && timers.queueIdle != nil {
		timers.queueIdle.Stop()
		timers.queueIdle = nil
		s.cleanupGuildTimersLocked(guildID)
	}
}

func (s *VoiceSupervisor) stopAloneTimerLocked(guildID string) {
	if timers, ok := s.timers[guildID]; ok && timers.alone != nil {
		timers.alone.Stop()
		timers.alone = nil
		s.cleanupGuildTimersLocked(guildID)
	}
}

func (s *VoiceSupervisor) ensureGuildTimersLocked(guildID string) *guildTimers {
	timers, ok := s.timers[guildID]
	if !ok {
		timers = &guildTimers{}
		s.timers[guildID] = timers
	}
	return timers
}

func (s *VoiceSupervisor) cleanupGuildTimersLocked(guildID string) {
	timers, ok := s.timers[guildID]
	if !ok {
		return
	}
	if timers.queueIdle == nil && timers.alone == nil {
		delete(s.timers, guildID)
	}
}
