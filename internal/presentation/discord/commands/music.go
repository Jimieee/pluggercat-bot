package commands

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"

	musicapp "pluggercat/internal/application/music"
	"pluggercat/internal/ports"
	discordrouter "pluggercat/internal/presentation/discord/router"
	musicui "pluggercat/internal/presentation/discord/ui/music"
	"pluggercat/internal/presentation/discord/ui/pagination"
)

func RegisterMusic(
	router *discordrouter.Router,
	service *musicapp.Service,
	discordGateway ports.DiscordGateway,
	voiceSupervisor *musicapp.VoiceSupervisor,
	pager *pagination.Manager,
) {
	queueUI := musicui.NewQueueManager(service, voiceSupervisor, pager)
	router.RegisterModalPrefix(musicui.QueueModalPrefix, queueUI.HandleModal)

	router.Register(
		"play",
		discord.SlashCommandCreate{
			Name:        "play",
			Description: "plays a track from a URL or search query",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:         "query",
					Description:  "URL or search query",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			channelID, err := discordGateway.UserVoiceChannelID(context.Background(), guildID.String(), event.User().ID.String())
			if err != nil || channelID == nil {
				return ephemeralMessage(event, "You must be in a voice channel to use `/play`.")
			}

			query := strings.TrimSpace(event.SlashCommandInteractionData().String("query"))
			if query == "" {
				return ephemeralMessage(event, "You must specify a URL or search query.")
			}

			if err := event.DeferCreateMessage(false); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			if err := discordGateway.JoinVoiceChannel(ctx, guildID.String(), *channelID); err != nil {
				return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("I could not join your voice channel."))
			}

			result, err := service.EnqueueQuery(ctx, guildID.String(), query)
			if err != nil {
				if errors.Is(err, musicapp.ErrNoTracksFound) {
					return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("I could not find any results for that search."))
				}
				if errors.Is(err, musicapp.ErrAutocompleteSelectionExpired) {
					return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("That suggestion expired. Please run `/play` again and choose a fresh result."))
				}
				return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("You could not add that track to the queue."))
			}

			title := "Added to Queue"
			if result.Started {
				voiceSupervisor.OnPlaybackStarted(guildID.String())
				title = "Now Playing"
			}

			return updateInteractionResponse(
				event,
				discord.NewMessageUpdate().
					WithEmbeds(musicui.BuildEnqueueEmbed(title, result, "Requested by "+event.User().EffectiveName())),
			)
		},
	)

	router.RegisterAutocomplete("play", func(event *events.AutocompleteInteractionCreate) error {
		query := strings.TrimSpace(event.Data.String("query"))
		if query == "" || strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
			return event.AutocompleteResult([]discord.AutocompleteChoice{})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		suggestions, err := service.Autocomplete(ctx, event.User().ID.String(), query, 10)
		if err != nil {
			return err
		}

		return event.AutocompleteResult(musicui.AutocompleteChoicesFromSuggestions(suggestions))
	})

	router.Register(
		"search",
		discord.SlashCommandCreate{
			Name:        "search",
			Description: "Searches tracks and lets you pick one from a paginated result view",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "query",
					Description: "Song name or URL",
					Required:    true,
				},
			},
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			guildID, ok := requireGuild(event)
			if !ok {
				return nil
			}

			query := strings.TrimSpace(event.SlashCommandInteractionData().String("query"))
			if query == "" {
				return ephemeralMessage(event, "You must specify a song name or URL.")
			}

			if err := event.DeferCreateMessage(false); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			tracks, err := service.Search(ctx, query)
			if err != nil {
				if errors.Is(err, musicapp.ErrNoTracksFound) {
					return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("I could not find any results for that search."))
				}
				return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("I could not complete that search right now."))
			}

			session := &pagination.Session{
				OwnerID: event.User().ID.String(),
				Render: func(page int) (pagination.RenderedPage, error) {
					return musicui.BuildSearchPage(query, tracks, page), nil
				},
				OnSelect: func(componentEvent *events.ComponentInteractionCreate, value string) (*pagination.InteractionResult, error) {
					track, found := musicui.TrackFromSelection(tracks, value)
					if !found {
						return &pagination.InteractionResult{
							Update: messageUpdateWithContent("That search result is no longer available."),
							Close:  true,
						}, nil
					}

					actionCtx, actionCancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer actionCancel()

					channelID, err := discordGateway.UserVoiceChannelID(actionCtx, guildID.String(), componentEvent.User().ID.String())
					if err != nil || channelID == nil {
						if createErr := componentEvent.CreateMessage(
							discord.NewMessageCreate().
								WithContent("You must be in a voice channel to play a search result.").
								WithEphemeral(true),
						); createErr != nil {
							return nil, createErr
						}
						return &pagination.InteractionResult{Handled: true}, nil
					}

					if err := discordGateway.JoinVoiceChannel(actionCtx, guildID.String(), *channelID); err != nil {
						if createErr := componentEvent.CreateMessage(
							discord.NewMessageCreate().
								WithContent("I could not join your voice channel.").
								WithEphemeral(true),
						); createErr != nil {
							return nil, createErr
						}
						return &pagination.InteractionResult{Handled: true}, nil
					}

					queuedTrack, started, err := service.EnqueueTrack(actionCtx, guildID.String(), track)
					if err != nil {
						if createErr := componentEvent.CreateMessage(
							discord.NewMessageCreate().
								WithContent("I could not queue that track.").
								WithEphemeral(true),
						); createErr != nil {
							return nil, createErr
						}
						return &pagination.InteractionResult{Handled: true}, nil
					}

					title := "Added to Queue"
					if started {
						voiceSupervisor.OnPlaybackStarted(guildID.String())
						title = "Now Playing"
					}

					update := discord.NewMessageUpdate().WithEmbeds(
						musicui.BuildPlaybackEmbed(title, queuedTrack, "Requested by "+componentEvent.User().EffectiveName()),
					)
					return &pagination.InteractionResult{
						Update: &update,
						Close:  true,
					}, nil
				},
			}

			message, err := pager.CreateMessage(session)
			if err != nil {
				return updateInteractionResponse(event, discord.NewMessageUpdate().WithContent("I could not build the search view right now."))
			}

			return updateInteractionResponse(
				event,
				discord.NewMessageUpdate().
					WithContent(message.Content).
					WithEmbeds(message.Embeds...).
					WithComponents(message.Components...),
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
			if queue.Current == nil && len(queue.Upcoming) == 0 {
				return event.CreateMessage(
					discord.NewMessageCreate().
						WithEmbeds(musicui.BuildQueueEmptyEmbed()),
				)
			}

			message, err := pager.CreateMessage(
				queueUI.NewSession(guildID.String(), event.User().ID.String()),
			)
			if err != nil {
				return ephemeralMessage(event, "I could not build the queue view right now.")
			}

			return event.CreateMessage(message)
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

			if hasNext {
				voiceSupervisor.OnPlaybackStarted(guildID.String())
				return event.CreateMessage(
					discord.NewMessageCreate().
						WithEmbeds(musicui.BuildPlaybackEmbed("Now Playing", nextTrack, "Skipped by "+event.User().EffectiveName())),
				)
			}

			voiceSupervisor.OnQueueDrained(guildID.String())
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("Track skipped. The queue is now empty."),
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

func ephemeralMessage(event *events.ApplicationCommandInteractionCreate, content string) error {
	return event.CreateMessage(
		discord.NewMessageCreate().
			WithContent(content).
			WithEphemeral(true),
	)
}

func updateInteractionResponse(event *events.ApplicationCommandInteractionCreate, update discord.MessageUpdate) error {
	_, err := event.Client().Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), update)
	return err
}

func messageUpdateWithContent(content string) *discord.MessageUpdate {
	update := discord.NewMessageUpdate().WithContent(content)
	return &update
}
