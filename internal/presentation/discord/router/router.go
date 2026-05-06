package router

import (
	"log/slog"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

type HandlerFunc func(event *events.ApplicationCommandInteractionCreate) error
type AutocompleteHandlerFunc func(event *events.AutocompleteInteractionCreate) error
type ComponentHandlerFunc func(event *events.ComponentInteractionCreate) error
type ModalHandlerFunc func(event *events.ModalSubmitInteractionCreate) error

type componentRoute struct {
	prefix  string
	handler ComponentHandlerFunc
}

type modalRoute struct {
	prefix  string
	handler ModalHandlerFunc
}

type Router struct {
	logger               *slog.Logger
	order                []string
	commands             map[string]discord.ApplicationCommandCreate
	handlers             map[string]HandlerFunc
	autocompleteHandlers map[string]AutocompleteHandlerFunc
	componentHandlers    []componentRoute
	modalHandlers        []modalRoute
}

// this is the responsible for routing incoming discord interactions to the correct handler
func New(logger *slog.Logger) *Router {
	return &Router{
		logger:               logger.With(slog.String("component", "discord_router")),
		order:                []string{},
		commands:             map[string]discord.ApplicationCommandCreate{},
		handlers:             map[string]HandlerFunc{},
		autocompleteHandlers: map[string]AutocompleteHandlerFunc{},
		componentHandlers:    []componentRoute{},
		modalHandlers:        []modalRoute{},
	}
}

// registers a command and its handler to the router
func (r *Router) Register(name string, command discord.ApplicationCommandCreate, handler HandlerFunc) {
	// command already exists, just update the handler
	if _, exists := r.commands[name]; !exists {
		r.order = append(r.order, name)
	}

	r.commands[name] = command
	r.handlers[name] = handler
}

func (r *Router) RegisterAutocomplete(name string, handler AutocompleteHandlerFunc) {
	r.autocompleteHandlers[name] = handler
}

func (r *Router) RegisterComponentPrefix(prefix string, handler ComponentHandlerFunc) {
	r.componentHandlers = append(r.componentHandlers, componentRoute{
		prefix:  prefix,
		handler: handler,
	})
}

func (r *Router) RegisterModalPrefix(prefix string, handler ModalHandlerFunc) {
	r.modalHandlers = append(r.modalHandlers, modalRoute{
		prefix:  prefix,
		handler: handler,
	})
}

// returns the registered commands in order
func (r *Router) Commands() []discord.ApplicationCommandCreate {
	commands := make([]discord.ApplicationCommandCreate, 0, len(r.order))
	for _, name := range r.order {
		commands = append(commands, r.commands[name])
	}
	return commands
}

// main entry point for handling incoming interactions
func (r *Router) Handle(event *events.ApplicationCommandInteractionCreate) {
	data := event.SlashCommandInteractionData()
	handler, ok := r.handlers[data.CommandName()]

	// this should never happen, jut in case for those people who like to break things
	if !ok {
		r.logger.Warn("received command without handler", slog.String("command", data.CommandName()))
		_ = event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("Command not have handler.").
				WithEphemeral(true),
		)
		return
	}

	// execute the handler and log any error
	if err := handler(event); err != nil {
		r.logger.Error("command handler failed", slog.String("command", data.CommandName()), slog.Any("err", err))
		_ = event.CreateMessage(
			discord.NewMessageCreate().
				WithContent("There was an error executing the command. Check the logs.").
				WithEphemeral(true),
		)
	}
}

func (r *Router) HandleAutocomplete(event *events.AutocompleteInteractionCreate) {
	handler, ok := r.autocompleteHandlers[event.Data.CommandName]
	if !ok {
		if err := event.AutocompleteResult([]discord.AutocompleteChoice{}); err != nil {
			r.logger.Error("autocomplete handler missing and empty response failed", slog.String("command", event.Data.CommandName), slog.Any("err", err))
		}
		return
	}

	if err := handler(event); err != nil {
		r.logger.Error("autocomplete handler failed", slog.String("command", event.Data.CommandName), slog.Any("err", err))
		if respondErr := event.AutocompleteResult([]discord.AutocompleteChoice{}); respondErr != nil {
			r.logger.Error("autocomplete empty response failed", slog.String("command", event.Data.CommandName), slog.Any("err", respondErr))
		}
	}
}

func (r *Router) HandleComponent(event *events.ComponentInteractionCreate) {
	customID := event.Data.CustomID()
	for _, route := range r.componentHandlers {
		if !strings.HasPrefix(customID, route.prefix) {
			continue
		}

		if err := route.handler(event); err != nil {
			r.logger.Error("component handler failed", slog.String("custom_id", customID), slog.Any("err", err))
			_ = event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("There was an error handling this interaction. Please try again.").
					WithEphemeral(true),
			)
		}
		return
	}

	_ = event.CreateMessage(
		discord.NewMessageCreate().
			WithContent("This interaction is no longer available.").
			WithEphemeral(true),
	)
}

func (r *Router) HandleModal(event *events.ModalSubmitInteractionCreate) {
	customID := event.Data.CustomID
	for _, route := range r.modalHandlers {
		if !strings.HasPrefix(customID, route.prefix) {
			continue
		}

		if err := route.handler(event); err != nil {
			r.logger.Error("modal handler failed", slog.String("custom_id", customID), slog.Any("err", err))
			_ = event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("There was an error handling this form. Please try again.").
					WithEphemeral(true),
			)
		}
		return
	}

	_ = event.CreateMessage(
		discord.NewMessageCreate().
			WithContent("This form is no longer available.").
			WithEphemeral(true),
	)
}
