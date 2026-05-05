package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"pluggercat/internal/app"
	"pluggercat/internal/config"
	"pluggercat/internal/platform/logging"
)

// main entry point for the app
// loads the config, initializes the app, and starts it
func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("unable to load .env", slog.Any("err", err))
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("unable to load config", slog.Any("err", err))
		os.Exit(1)
	}

	logger := logging.New(cfg.App.LogLevel)
	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("unable to build application", slog.Any("err", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		logger.Error("unable to start application", slog.Any("err", err))
		os.Exit(1)
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	application.Close(shutdownCtx)
}
