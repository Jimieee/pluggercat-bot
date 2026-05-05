package ports

import "context"

type VoiceStateHandler func(ctx context.Context, guildID string)

// defines the interface for the discord gateway
// main entry point for sending and receiving data to/from discord
type DiscordGateway interface {
	Open(ctx context.Context) error
	Close(ctx context.Context)
	SyncCommands(ctx context.Context) error
	JoinVoiceChannel(ctx context.Context, guildID string, channelID string) error
	LeaveVoiceChannel(ctx context.Context, guildID string) error
	CurrentVoiceChannelID(ctx context.Context, guildID string) (*string, error)
	CountHumanMembersInVoiceChannel(ctx context.Context, guildID string, channelID string) (int, error)
	AddVoiceStateHandler(handler VoiceStateHandler)
	BotID() string
}
