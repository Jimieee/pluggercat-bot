package ports

import (
	"context"

	domainmusic "pluggercat/internal/domain/music"
)

// defines the interfaces for the audio backend and voice bridge, which are used by the application layer to interact with the audio system and discord gateway respectively
type VoiceBridge interface {
	ForwardVoiceState(ctx context.Context, guildID string, channelID *string, sessionID string) error
	ForwardVoiceServer(ctx context.Context, guildID string, token string, endpoint string) error
}

// defines the interface for the audio backend, which abstracts the underlying audio system (e.g. Lavalink) and provides methods for connecting, searching tracks, controlling playback, and forwarding voice updates to the discord gateway
type AudioBackend interface {
	VoiceBridge
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	LoadTracks(ctx context.Context, query string) (domainmusic.LoadResult, error)
	SearchTracks(ctx context.Context, query string) ([]domainmusic.Track, error)
	PlayTrack(ctx context.Context, guildID string, track domainmusic.Track) error
	Pause(ctx context.Context, guildID string) error
	Resume(ctx context.Context, guildID string) error
	Stop(ctx context.Context, guildID string) error
}

// defines the interface for the queue repository, which abstracts the storage mechanism for music queues and provides methods for retrieving, saving, and deleting queues based on guild ID
type QueueRepository interface {
	Get(ctx context.Context, guildID string) (domainmusic.Queue, bool, error)
	Save(ctx context.Context, queue domainmusic.Queue) error
	Delete(ctx context.Context, guildID string) error
}
