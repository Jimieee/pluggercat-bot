package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"

	discordrouter "pluggercat/internal/presentation/discord/router"
)

func RegisterBase(router *discordrouter.Router) {
	router.Register(
		"ping",
		discord.SlashCommandCreate{
			Name:        "ping",
			Description: "checks if the bot is alive (if u dont get pong back well rip.)",
		},
		func(event *events.ApplicationCommandInteractionCreate) error {
			return event.CreateMessage(
				discord.NewMessageCreate().
					WithContent("pong"),
			)
		},
	)
}
