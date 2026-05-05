package disgoadapter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"

	"pluggercat/internal/config"
	"pluggercat/internal/ports"
)

var _ ports.DiscordGateway = (*Client)(nil)

type Client struct {
	logger        *slog.Logger
	cfg           config.DiscordConfig
	raw           *bot.Client
	commands      []discord.ApplicationCommandCreate
	voiceBridge   ports.VoiceBridge
	voiceHandlers []ports.VoiceStateHandler
	voiceMu       sync.RWMutex
	commandHandle func(*events.ApplicationCommandInteractionCreate)
}

// creates a new discord client with the given config and logger
func New(
	cfg config.DiscordConfig,
	logger *slog.Logger,
	commandHandler func(*events.ApplicationCommandInteractionCreate),
) (*Client, error) {
	client := &Client{
		logger:        logger.With(slog.String("component", "discord")),
		cfg:           cfg,
		commandHandle: commandHandler,
	}

	// build disgo client with the provided config and logger
	opts := []bot.ConfigOpt{
		bot.WithLogger(client.logger),
		bot.WithDefaultGateway(),
		bot.WithGatewayConfigOpts(gateway.WithIntents(
			gateway.IntentGuilds,
			gateway.IntentGuildVoiceStates,
		)),
		bot.WithEventListenerFunc(client.onApplicationCommand),
		bot.WithEventListenerFunc(client.onVoiceStateUpdate),
		bot.WithEventListenerFunc(client.onVoiceServerUpdate),
	}

	// if dev guild id is set, use guild commands for faster sync
	raw, err := disgo.New(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("build disgo client: %w", err)
	}

	client.raw = raw
	return client, nil
}

// sets the application commands to be registered on startup
func (c *Client) SetCommands(commands []discord.ApplicationCommandCreate) {
	c.commands = commands
}

// sets the voice bridge to forward voice state and server updates to Lavalink
func (c *Client) SetVoiceBridge(voiceBridge ports.VoiceBridge) {
	c.voiceBridge = voiceBridge
}

func (c *Client) AddVoiceStateHandler(handler ports.VoiceStateHandler) {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	c.voiceHandlers = append(c.voiceHandlers, handler)
}

// opens the discord gateway connection and starts listening for events
func (c *Client) Open(ctx context.Context) error {
	return c.raw.OpenGateway(ctx)
}

// closes the discord gateway connection and cleans up resources
func (c *Client) Close(ctx context.Context) {
	c.raw.Close(ctx)
}

// syncs the registered application commands with Discord, either globally or to a specific guild if dev guild id is set
func (c *Client) SyncCommands(ctx context.Context) error {
	if len(c.commands) == 0 {
		return nil
	}

	// if dev guild id is set, register commands to that guild for faster sync, otherwise register globally
	if c.cfg.DevGuildID != "" {
		guildID, err := snowflake.Parse(c.cfg.DevGuildID)
		if err != nil {
			return fmt.Errorf("parse DISCORD_DEV_GUILD_ID: %w", err)
		}

		_, err = c.raw.Rest.SetGuildCommands(c.raw.ApplicationID, guildID, c.commands)
		if err != nil {
			return fmt.Errorf("set guild commands: %w", err)
		}

		c.logger.Info("synced guild commands", slog.String("guild_id", guildID.String()), slog.Int("count", len(c.commands)))
		return nil
	}

	_, err := c.raw.Rest.SetGlobalCommands(c.raw.ApplicationID, c.commands)
	if err != nil {
		return fmt.Errorf("set global commands: %w", err)
	}

	c.logger.Info("synced global commands", slog.Int("count", len(c.commands)))
	return nil
}

// joins the specified voice channel in the specified guild by updating the bot's voice state
func (c *Client) JoinVoiceChannel(ctx context.Context, guildID string, channelID string) error {
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parse guild id: %w", err)
	}

	// channelID can be empty when leaving a voice channel, but JoinVoiceChannel should require it to avoid mistakes
	parsedChannelID, err := snowflake.Parse(channelID)
	if err != nil {
		return fmt.Errorf("parse channel id: %w", err)
	}

	return c.raw.UpdateVoiceState(ctx, parsedGuildID, &parsedChannelID, false, false)
}

// leaves the voice channel in the specified guild by updating the bot's voice state with a nil channel ID
func (c *Client) LeaveVoiceChannel(ctx context.Context, guildID string) error {
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parse guild id: %w", err)
	}

	return c.raw.UpdateVoiceState(ctx, parsedGuildID, nil, false, false)
}

func (c *Client) CurrentVoiceChannelID(_ context.Context, guildID string) (*string, error) {
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return nil, fmt.Errorf("parse guild id: %w", err)
	}

	voiceState, ok := c.raw.Caches.VoiceState(parsedGuildID, c.raw.ApplicationID)
	if !ok || voiceState.ChannelID == nil {
		return nil, nil
	}

	value := voiceState.ChannelID.String()
	return &value, nil
}

func (c *Client) CountHumanMembersInVoiceChannel(_ context.Context, guildID string, channelID string) (int, error) {
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return 0, fmt.Errorf("parse guild id: %w", err)
	}

	parsedChannelID, err := snowflake.Parse(channelID)
	if err != nil {
		return 0, fmt.Errorf("parse channel id: %w", err)
	}

	humans := 0
	for voiceState := range c.raw.Caches.VoiceStates(parsedGuildID) {
		if voiceState.UserID == c.raw.ApplicationID || voiceState.ChannelID == nil {
			continue
		}
		if *voiceState.ChannelID == parsedChannelID {
			humans++
		}
	}

	return humans, nil
}

// returns the bot's user ID as a string
func (c *Client) BotID() string {
	return c.raw.ApplicationID.String()
}

// event handler for application command interactions, which calls the provided command handler function if set
func (c *Client) onApplicationCommand(event *events.ApplicationCommandInteractionCreate) {
	if c.commandHandle != nil {
		c.commandHandle(event)
	}
}

// event handler for voice state updates, which forwards the relevant information to the voice bridge if set
func (c *Client) onVoiceStateUpdate(event *events.GuildVoiceStateUpdate) {
	if c.voiceBridge == nil {
		c.dispatchVoiceStateHandlers(event.VoiceState.GuildID.String())
		return
	}

	var channelID *string

	if event.VoiceState.ChannelID != nil {
		value := event.VoiceState.ChannelID.String()
		channelID = &value
	}

	if event.VoiceState.UserID == event.Client().ID() {
		if err := c.voiceBridge.ForwardVoiceState(
			context.Background(),
			event.VoiceState.GuildID.String(),
			channelID,
			event.VoiceState.SessionID,
		); err != nil {
			c.logger.Error("unable to forward voice state update", slog.Any("err", err))
		}
	}

	c.dispatchVoiceStateHandlers(event.VoiceState.GuildID.String())
}

// event handler for voice server updates, which forwards the relevant information to the voice bridge if set
func (c *Client) onVoiceServerUpdate(event *events.VoiceServerUpdate) {
	// if the voice bridge is not set or the event does not contain an endpoint, we cannot forward the update to Lavalink, so we return early
	if c.voiceBridge == nil || event.Endpoint == nil {
		return
	}

	if err := c.voiceBridge.ForwardVoiceServer(
		context.Background(),
		event.GuildID.String(),
		event.Token,
		*event.Endpoint,
	); err != nil {
		c.logger.Error("unable to forward voice server update", slog.Any("err", err))
	}
}

func (c *Client) dispatchVoiceStateHandlers(guildID string) {
	c.voiceMu.RLock()
	handlers := append([]ports.VoiceStateHandler(nil), c.voiceHandlers...)
	c.voiceMu.RUnlock()

	for _, handler := range handlers {
		go func(handler ports.VoiceStateHandler) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			handler(ctx, guildID)
		}(handler)
	}
}
