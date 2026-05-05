package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"

	musicapp "pluggercat/internal/application/music"
	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/ports"
	discordrouter "pluggercat/internal/presentation/discord/router"
)

func RegisterMusic(
	router *discordrouter.Router,
	service *musicapp.Service,
	discordGateway ports.DiscordGateway,
	voiceSupervisor *musicapp.VoiceSupervisor,
) {
	router.Register(
		"play",
		discord.SlashCommandCreate{
			Name:        "play",
			Description: "plays a track from a URL or search query",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "query",
					Description: "URL or search query",
					Required:    true,
				},
			},
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			channelID, ok := userVoiceChannelID(event, *guildID)
			if !ok {
				return ephemeralMessage(event, "You must be in a voice channel to use `/play`.")
			}

			query := strings.TrimSpace(event.SlashCommandInteractionData().String("query"))
			if query == "" {
				return ephemeralMessage(event, "You must specify a URL or search query.")
			}

			if err := discordGateway.JoinVoiceChannel(context.Background(), guildID.String(), channelID.String()); err != nil {
				return ephemeralMessage(event, "You could not join your voice channel.")
			}

			track, started, err := service.Enqueue(context.Background(), guildID.String(), query)
			if err != nil {
				if errors.Is(err, musicapp.ErrNoTracksFound) {
					return ephemeralMessage(event, "I could not find any results for that search.")
				}
				return ephemeralMessage(event, "You could not add that track to the queue.")
			}

			message := fmt.Sprintf("Added to the queue: %s", formatTrack(track))
			if started {
				voiceSupervisor.OnPlaybackStarted(guildID.String())
				message = fmt.Sprintf("Now playing: %s", formatTrack(track))
			}

			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent(message),
			)
		},
	)

	router.Register(
		"queue",
		discord.SlashCommandCreate{
			Name:        "queue",
			Description: "Shows the current queue for the server",
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			queue, err := service.Queue(context.Background(), guildID.String())
			if err != nil {
				return ephemeralMessage(event, "I could not read the current queue.")
			}

			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent(formatQueue(queue)),
			)
		},
	)

	router.Register(
		"pause",
		discord.SlashCommandCreate{
			Name:        "pause",
			Description: "Pauses the current playback",
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			err := service.Pause(context.Background(), guildID.String())
			if err != nil {
				if errors.Is(err, musicapp.ErrNothingPlaying) {
					return ephemeralMessage(event, "Nothing is currently playing.")
				}
				return ephemeralMessage(event, "You could not pause the playback.")
			}

			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("Playback paused."),
			)
		},
	)

	router.Register(
		"resume",
		discord.SlashCommandCreate{
			Name:        "resume",
			Description: "Resumes the current playback",
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			err := service.Resume(context.Background(), guildID.String())
			if err != nil {
				if errors.Is(err, musicapp.ErrNothingPlaying) {
					return ephemeralMessage(event, "Nothing is currently playing.")
				}
				return ephemeralMessage(event, "You could not resume the playback.")
			}

			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("Playback resumed."),
			)
		},
	)

	router.Register(
		"skip",
		discord.SlashCommandCreate{
			Name:        "skip",
			Description: "Skips the current track and plays the next one in the queue",
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			nextTrack, hasNext, err := service.Skip(context.Background(), guildID.String())
			if err != nil {
				if errors.Is(err, musicapp.ErrNothingPlaying) {
					return ephemeralMessage(event, "Nothing is currently playing.")
				}
				return ephemeralMessage(event, "You could not skip the current track.")
			}

			message := "Track skipped. The queue is now empty."
			if hasNext {
				voiceSupervisor.OnPlaybackStarted(guildID.String())
				message = fmt.Sprintf("Skipping. Now playing: %s", formatTrack(nextTrack))
			} else {
				voiceSupervisor.OnQueueDrained(guildID.String())
			}

			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent(message),
			)
		},
	)

	router.Register(
		"leave",
		discord.SlashCommandCreate{
			Name:        "leave",
			Description: "Removes the bot from the voice channel and clears the queue",
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			if err := service.StopAndClear(context.Background(), guildID.String()); err != nil {
				return ephemeralMessage(event, "You could not stop the current playback.")
			}
			if err := discordGateway.LeaveVoiceChannel(context.Background(), guildID.String()); err != nil {
				return ephemeralMessage(event, "You could not leave the voice channel.")
			}
			voiceSupervisor.OnDisconnected(guildID.String())

			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("Left the voice channel and cleared the queue."),
			)
		},
	)
}

func requireGuild(event *events.ApplicationCommandInteractionCreate) (*snowflake.ID, bool) {
	guildID := event.GuildID()
	if guildID == nil {
		_ = ephemeralMessage(event, "This command only works within a server.")
		return nil, false
	}
	return guildID, true
}

func userVoiceChannelID(event *events.ApplicationCommandInteractionCreate, guildID snowflake.ID) (*snowflake.ID, bool) {
	userID := event.User().ID

	if voiceState, ok := event.Client().Caches.VoiceState(guildID, userID); ok && voiceState.ChannelID != nil {
		return voiceState.ChannelID, true
	}

	voiceState, err := event.Client().Rest.GetUserVoiceState(guildID, userID)
	if err != nil || voiceState == nil || voiceState.ChannelID == nil {
		return nil, false
	}

	return voiceState.ChannelID, true
}

func ephemeralMessage(event *events.ApplicationCommandInteractionCreate, content string) error {
	return event.CreateMessage(
		discord.NewMessageCreate().
			WithContent(content).
			WithEphemeral(true),
	)
}

func formatQueue(queue domainmusic.Queue) string {
	if queue.Current == nil && len(queue.Upcoming) == 0 {
		return "The queue is empty."
	}

	lines := []string{}
	if queue.Current != nil {
		lines = append(lines, "Now playing: "+formatTrack(*queue.Current))
	} else {
		lines = append(lines, "Now playing: nothing yet")
	}

	if queue.Paused {
		lines = append(lines, "Status: paused")
	}

	if len(queue.Upcoming) == 0 {
		lines = append(lines, "Next: no tracks in queue")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, fmt.Sprintf("Next: %d track(s)", len(queue.Upcoming)))
	for i, track := range queue.Upcoming {
		if i == 5 {
			lines = append(lines, fmt.Sprintf("... and %d more", len(queue.Upcoming)-5))
			break
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, formatTrack(track)))
	}

	return strings.Join(lines, "\n")
}

func formatTrack(track domainmusic.Track) string {
	duration := formatDuration(track.Duration)
	if track.Author == "" {
		return fmt.Sprintf("%s (%s)", track.Title, duration)
	}
	return fmt.Sprintf("%s - %s (%s)", track.Author, track.Title, duration)
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "unknown duration"
	}

	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
