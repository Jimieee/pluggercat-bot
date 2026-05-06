package disgolinkadapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	disgolink "github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"

	"pluggercat/internal/config"
	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/ports"
)

var _ ports.AudioBackend = (*Client)(nil)

var ErrLavalinkNotConfigured = errors.New("lavalink is not configured")

var searchPrefixPattern = regexp.MustCompile(`^[a-z0-9]+search:`)

type Client struct {
	logger *slog.Logger
	cfg    config.LavalinkConfig
	raw    disgolink.Client
}

type TrackEndHandler func(ctx context.Context, guildID string) error

// creates a new lavalink client with the given config and logger
func New(cfg config.LavalinkConfig, logger *slog.Logger, botID string) (*Client, error) {
	userID, err := snowflake.Parse(botID)
	if err != nil {
		return nil, fmt.Errorf("parse bot id: %w", err)
	}

	return &Client{
		logger: logger.With(slog.String("component", "lavalink")),
		cfg:    cfg,
		raw:    disgolink.New(userID),
	}, nil
}

// connects to the lavalink node if not already connected, and returns an error if connection fails
func (c *Client) Connect(ctx context.Context) error {
	// if lavalink is not enabled, skip connection and log a warning
	if !c.cfg.Enabled {
		c.logger.Warn("lavalink is disabled until LAVALINK_ADDRESS and LAVALINK_PASSWORD are configured")
		return nil
	}

	// if already connected to a node, skip connection
	if c.raw.BestNode() != nil {
		return nil
	}

	// connect to the lavalink node with the provided config
	_, err := c.raw.AddNode(ctx, disgolink.NodeConfig{
		Name:     c.cfg.NodeName,
		Address:  c.cfg.Address,
		Password: c.cfg.Password,
		Secure:   c.cfg.Secure,
	})
	if err != nil {
		return fmt.Errorf("add lavalink node: %w", err)
	}

	c.logger.Info(
		"connected lavalink node",
		slog.String("node_name", c.cfg.NodeName),
		slog.String("address", c.cfg.Address),
		slog.Bool("secure", c.cfg.Secure),
	)
	return nil
}

// closes the lavalink client connection
func (c *Client) Close(context.Context) error {
	c.raw.Close()
	return nil
}

// when a track ends and may start the next one, this handler will be called with the guild ID of the player that ended the track, and can be used to advance the queue and play the next track if there is one
func (c *Client) OnTrackMayStartNext(handler TrackEndHandler) {
	c.raw.AddListeners(disgolink.NewListenerFunc(func(_ disgolink.Player, event lavalink.TrackEndEvent) {
		if !event.Reason.MayStartNext() {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		guildID := event.GuildID().String()
		if err := handler(ctx, guildID); err != nil {
			c.logger.Error(
				"failed to handle track end event",
				slog.String("guild_id", guildID),
				slog.String("reason", string(event.Reason)),
				slog.Any("err", err),
			)
		}
	}))
}

// searches for tracks matching the query using the lavalink node, and returns a list of matching tracks or an error if the search fails
func (c *Client) SearchTracks(ctx context.Context, query string) ([]domainmusic.Track, error) {
	node, err := c.readyNode()
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, identifier := range candidateIdentifiers(query) {
		tracks, found, err := c.loadTracks(ctx, node, identifier)
		if err != nil {
			lastErr = err
			continue
		}
		if found {
			return tracks, nil
		}
	}

	return nil, lastErr
}

// joins the specified voice channel in the specified guild by updating the bot's voice state
func (c *Client) PlayTrack(ctx context.Context, guildID string, track domainmusic.Track) error {
	if _, err := c.readyNode(); err != nil {
		return err
	}

	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parse guild id: %w", err)
	}

	player := c.raw.Player(parsedGuildID)
	return player.Update(ctx, lavalink.WithEncodedTrack(track.Reference))
}

// syncs the bot's application commands with Discord by registering them globally or to a specific guild if a dev guild ID is set in the config
func (c *Client) Pause(ctx context.Context, guildID string) error {
	return c.updatePlayer(ctx, guildID, lavalink.WithPaused(true))
}

// syncs the bot's application commands with Discord by registering them globally or to a specific guild if a dev guild ID is set in the config
func (c *Client) Resume(ctx context.Context, guildID string) error {
	return c.updatePlayer(ctx, guildID, lavalink.WithPaused(false))
}

// syncs the bot's application commands with Discord by registering them globally or to a specific guild if a dev guild ID is set in the config
func (c *Client) Stop(ctx context.Context, guildID string) error {
	return c.updatePlayer(ctx, guildID, lavalink.WithNullTrack())
}

// syncs the bot's application commands with Discord by registering them globally or to a specific guild if a dev guild ID is set in the config
func (c *Client) ForwardVoiceState(ctx context.Context, guildID string, channelID *string, sessionID string) error {
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parse guild id: %w", err)
	}

	// if channelID is nil, it means the bot has left the voice channel, so we can forward a nil channel ID to lavalink to update the voice state accordingly
	var parsedChannelID *snowflake.ID
	if channelID != nil {
		value, err := snowflake.Parse(*channelID)
		if err != nil {
			return fmt.Errorf("parse channel id: %w", err)
		}
		parsedChannelID = &value
	}

	c.raw.OnVoiceStateUpdate(ctx, parsedGuildID, parsedChannelID, sessionID)
	return nil
}

// syncs the bot's application commands with Discord by registering them globally or to a specific guild if a dev guild ID is set in the config
func (c *Client) ForwardVoiceServer(ctx context.Context, guildID string, token string, endpoint string) error {
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parse guild id: %w", err)
	}

	c.raw.OnVoiceServerUpdate(ctx, parsedGuildID, token, endpoint)
	return nil
}

// syncs the bot's application commands with Discord by registering them globally or to a specific guild if a dev guild ID is set in the config
func (c *Client) updatePlayer(ctx context.Context, guildID string, opts ...lavalink.PlayerUpdateOpt) error {
	if _, err := c.readyNode(); err != nil {
		return err
	}

	// parse the guild ID from string to snowflake ID, and return an error if parsing fails
	parsedGuildID, err := snowflake.Parse(guildID)
	if err != nil {
		return fmt.Errorf("parse guild id: %w", err)
	}

	player := c.raw.Player(parsedGuildID)
	return player.Update(ctx, opts...)
}

// checks if the lavalink client is enabled and has a connected node, and returns the best node or an error if not ready
func (c *Client) readyNode() (disgolink.Node, error) {
	if !c.cfg.Enabled {
		return nil, ErrLavalinkNotConfigured
	}

	// get the best node from the lavalink client, which is the node with the lowest ping and is connected, and return an error if no node is connected
	node := c.raw.BestNode()
	if node == nil {
		return nil, errors.New("no lavalink node connected")
	}
	return node, nil
}

// maps a lavalink track to a domain music track by extracting the relevant information from the lavalink track and returning a new domain music track with the mapped fields
func mapTrack(track lavalink.Track) domainmusic.Track {
	artwork := ""
	if track.Info.ArtworkURL != nil {
		artwork = *track.Info.ArtworkURL
	}

	isrc := ""
	if track.Info.ISRC != nil {
		isrc = *track.Info.ISRC
	}

	return domainmusic.Track{
		ID:        track.Info.Identifier,
		Title:     track.Info.Title,
		Author:    track.Info.Author,
		URL:       trackURL(track),
		Source:    track.Info.SourceName,
		Artwork:   artwork,
		ISRC:      isrc,
		Duration:  time.Duration(track.Info.Length.Milliseconds()) * time.Millisecond,
		Reference: track.Encoded,
	}
}

// extracts the track URL from a lavalink track by checking the URI field, then the identifier field for a valid URL, and returns an empty string if no valid URL is found
func trackURL(track lavalink.Track) string {
	if track.Info.URI != nil {
		return *track.Info.URI
	}
	if strings.HasPrefix(track.Info.Identifier, "http://") || strings.HasPrefix(track.Info.Identifier, "https://") {
		return track.Info.Identifier
	}
	if _, err := url.ParseRequestURI(track.Info.Identifier); err == nil {
		return track.Info.Identifier
	}
	return ""
}

func candidateIdentifiers(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if isDirectURL(query) || searchPrefixPattern.MatchString(strings.ToLower(query)) {
		return []string{query}
	}
	return []string{
		"ytmsearch:" + query,
		"ytsearch:" + query,
	}
}

func (c *Client) loadTracks(ctx context.Context, node disgolink.Node, identifier string) ([]domainmusic.Track, bool, error) {
	result, err := node.LoadTracks(ctx, identifier)
	if err != nil {
		return nil, false, fmt.Errorf("load tracks %q: %w", identifier, err)
	}

	switch data := result.Data.(type) {
	case lavalink.Track:
		return []domainmusic.Track{mapTrack(data)}, true, nil
	case lavalink.Search:
		tracks := make([]domainmusic.Track, 0, len(data))
		for _, track := range data {
			tracks = append(tracks, mapTrack(track))
		}
		return tracks, len(tracks) > 0, nil
	case lavalink.Playlist:
		tracks := make([]domainmusic.Track, 0, len(data.Tracks))
		for _, track := range data.Tracks {
			tracks = append(tracks, mapTrack(track))
		}
		return tracks, len(tracks) > 0, nil
	case lavalink.Empty:
		return nil, false, nil
	case lavalink.Exception:
		return nil, false, data
	default:
		return nil, false, fmt.Errorf("unsupported lavalink load result for %q: %T", identifier, data)
	}
}

func isDirectURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
