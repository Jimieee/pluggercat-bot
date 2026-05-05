package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	App      AppConfig
	Discord  DiscordConfig
	Lavalink LavalinkConfig
}

type AppConfig struct {
	Name                     string
	LogLevel                 string
	QueueIdleDisconnectAfter time.Duration
	AloneDisconnectAfter     time.Duration
}

type DiscordConfig struct {
	Token      string
	DevGuildID string
}

type LavalinkConfig struct {
	NodeName string
	Address  string
	Password string
	Secure   bool
	Enabled  bool
}

// Load reads configuration from environment variables and returns a Config struct. It validates required fields and applies default values where necessary.
func Load() (Config, error) {
	discordToken, err := requiredEnv("DISCORD_TOKEN")
	if err != nil {
		return Config{}, err
	}

	// read config from env variables, applying defaults where appropriate
	cfg := Config{
		App: AppConfig{
			Name:     envOrDefault("APP_NAME", "pluggercat"),
			LogLevel: strings.ToLower(envOrDefault("LOG_LEVEL", "info")),
		},
		Discord: DiscordConfig{
			Token:      discordToken,
			DevGuildID: os.Getenv("DISCORD_DEV_GUILD_ID"),
		},
		Lavalink: LavalinkConfig{
			NodeName: envOrDefault("LAVALINK_NODE_NAME", "main"),
			Address:  os.Getenv("LAVALINK_ADDRESS"),
			Password: os.Getenv("LAVALINK_PASSWORD"),
			Secure:   false,
		},
	}

	queueIdleDisconnectAfter, err := durationEnvOrDefault("VOICE_QUEUE_IDLE_DISCONNECT_AFTER", 3*time.Minute)
	if err != nil {
		return cfg, fmt.Errorf("parse VOICE_QUEUE_IDLE_DISCONNECT_AFTER: %w", err)
	}
	cfg.App.QueueIdleDisconnectAfter = queueIdleDisconnectAfter

	aloneDisconnectAfter, err := durationEnvOrDefault("VOICE_ALONE_DISCONNECT_AFTER", 3*time.Minute)
	if err != nil {
		return cfg, fmt.Errorf("parse VOICE_ALONE_DISCONNECT_AFTER: %w", err)
	}
	cfg.App.AloneDisconnectAfter = aloneDisconnectAfter

	// if LAVALINK_SECURE is set, parse it as a boolean and override the default value of false
	secure, found, err := lookupBool("LAVALINK_SECURE")
	if err != nil {
		return cfg, fmt.Errorf("parse LAVALINK_SECURE: %w", err)
	}

	// only override the default value if the environment variable was set, otherwise keep the default of false
	if found {
		cfg.Lavalink.Secure = secure
	}

	// if Lavalink address and password are set, enable Lavalink integration, otherwise disable it
	cfg.Lavalink.Enabled = cfg.Lavalink.Address != "" && cfg.Lavalink.Password != ""

	return cfg, nil
}

// returns the value of the specified environment variable, or the provided fallback if the variable is not set or is empty
func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func requiredEnv(key string) (string, error) {
	value, found := os.LookupEnv(key)
	if !found || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func durationEnvOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

// looks up the specified environment variable and attempts to parse it as a boolean. It returns the parsed value, a boolean indicating whether the variable was set, and any error that occurred during parsing.
func lookupBool(key string) (bool, bool, error) {
	raw, found := os.LookupEnv(key)
	// if the variable is not set or is empty, we return false and indicate that it was not found, without an error
	if !found || raw == "" {
		return false, false, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, true, err
	}
	return value, true, nil
}
