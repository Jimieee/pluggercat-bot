package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	disgoadapter "pluggercat/internal/adapters/discord/disgo"
	disgolinkadapter "pluggercat/internal/adapters/lavalink/disgolink"
	"pluggercat/internal/adapters/store/memory"
	musicapp "pluggercat/internal/application/music"
	"pluggercat/internal/config"
	"pluggercat/internal/ports"
	discordcommands "pluggercat/internal/presentation/discord/commands"
	discordrouter "pluggercat/internal/presentation/discord/router"
	"pluggercat/internal/presentation/discord/ui/pagination"
)

type App struct {
	logger  *slog.Logger
	discord ports.DiscordGateway
	audio   ports.AudioBackend
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	logger = logger.With(slog.String("app", cfg.App.Name))

	router := discordrouter.New(logger)
	pager := pagination.New(logger, 0)
	router.RegisterComponentPrefix(pagination.Prefix, pager.HandleComponent)

	discordClient, err := disgoadapter.New(
		cfg.Discord,
		logger,
		router.Handle,
		router.HandleAutocomplete,
		router.HandleComponent,
		router.HandleModal,
	)
	if err != nil {
		return nil, fmt.Errorf("create discord adapter: %w", err)
	}

	audioClient, err := disgolinkadapter.New(cfg.Lavalink, logger, discordClient.BotID())
	if err != nil {
		return nil, fmt.Errorf("create lavalink adapter: %w", err)
	}

	discordClient.SetVoiceBridge(audioClient)

	queueRepository := memory.NewQueueRepository()
	musicService := musicapp.NewService(queueRepository, audioClient)
	voiceSupervisor := musicapp.NewVoiceSupervisor(
		logger,
		discordClient,
		musicService,
		cfg.App.QueueIdleDisconnectAfter,
		cfg.App.AloneDisconnectAfter,
	)

	audioClient.OnTrackMayStartNext(func(ctx context.Context, guildID string) error {
		nextTrack, hasNext, err := musicService.AdvanceAfterTrackEnd(ctx, guildID)
		if err != nil {
			return err
		}

		if hasNext {
			voiceSupervisor.OnPlaybackStarted(guildID)
			logger.Info(
				"advanced queue after track end",
				slog.String("guild_id", guildID),
				slog.String("next_track", nextTrack.Title),
			)
		} else {
			voiceSupervisor.OnQueueDrained(guildID)
			logger.Info(
				"queue finished after track end",
				slog.String("guild_id", guildID),
			)
		}

		return nil
	})
	discordClient.AddVoiceStateHandler(func(ctx context.Context, guildID string) {
		voiceSupervisor.OnVoiceStateChanged(ctx, guildID)
	})

	discordcommands.RegisterBase(router)
	discordcommands.RegisterMusic(router, musicService, discordClient, voiceSupervisor, pager)
	discordClient.SetCommands(router.Commands())

	return &App{
		logger:  logger,
		discord: discordClient,
		audio:   audioClient,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.connectAudioBackend(ctx); err != nil {
		return fmt.Errorf("connect audio backend: %w", err)
	}

	if err := a.discord.SyncCommands(ctx); err != nil {
		return fmt.Errorf("sync application commands: %w", err)
	}

	if err := a.discord.Open(ctx); err != nil {
		return fmt.Errorf("open discord gateway: %w", err)
	}

	a.logger.Info("pluggercat is running")
	return nil
}

func (a *App) Close(ctx context.Context) {
	if err := a.audio.Close(ctx); err != nil {
		a.logger.Error("error while closing audio backend", slog.Any("err", err))
	}

	a.discord.Close(ctx)
	a.logger.Info("pluggercat stopped")
}

func (a *App) connectAudioBackend(ctx context.Context) error {
	const (
		maxAttempts = 10
		backoff     = 3 * time.Second
	)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := a.audio.Connect(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt == maxAttempts {
			break
		}

		a.logger.Warn(
			"audio backend not ready yet, retrying",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", maxAttempts),
			slog.Duration("retry_in", backoff),
			slog.Any("err", lastErr),
		)

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return lastErr
}
