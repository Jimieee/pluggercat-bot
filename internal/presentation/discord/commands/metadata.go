package commands

import "github.com/disgoorg/disgo/discord"

func guildOnlyCommandMetadata() (integrationTypes []discord.ApplicationIntegrationType, contexts []discord.InteractionContextType) {
	return []discord.ApplicationIntegrationType{
			discord.ApplicationIntegrationTypeGuildInstall,
		}, []discord.InteractionContextType{
			discord.InteractionContextTypeGuild,
		}
}
