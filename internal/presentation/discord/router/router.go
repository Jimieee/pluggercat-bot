package router

import (
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

type HandlerFunc func(event *events.ApplicationCommandInteractionCreate) error

type Router struct {
	logger   *slog.Logger
	order    []string
	commands map[string]discord.ApplicationCommandCreate
	handlers map[string]HandlerFunc
}

// this is the responsible for routing incoming discord interactions to the correct handler
func New(logger *slog.Logger) *Router {
	return &Router{
		logger:   logger.With(slog.String("component", "discord_router")),
		order:    []string{},
		commands: map[string]discord.ApplicationCommandCreate{},
		handlers: map[string]HandlerFunc{},
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
