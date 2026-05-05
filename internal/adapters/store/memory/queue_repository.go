package memory

import (
	"context"
	"sync"

	domainmusic "pluggercat/internal/domain/music"
	"pluggercat/internal/ports"
)

var _ ports.QueueRepository = (*QueueRepository)(nil)

type QueueRepository struct {
	mu     sync.RWMutex
	queues map[string]domainmusic.Queue
}

// creates a new in-memory queue repository with an empty map of queues
func NewQueueRepository() *QueueRepository {
	return &QueueRepository{
		queues: map[string]domainmusic.Queue{},
	}
}

// retrieves the music queue for the given guild ID from the in-memory map, and returns the queue, a boolean indicating if the queue was found, and an error if any occurs
func (r *QueueRepository) Get(_ context.Context, guildID string) (domainmusic.Queue, bool, error) {
	// acquire a read lock to safely access the queues map concurrently, and defer unlocking until the function returns
	r.mu.RLock()
	defer r.mu.RUnlock()

	// look up the queue for the given guild ID in the map, and return the queue, a boolean indicating if it was found, and nil error
	queue, ok := r.queues[guildID]
	return queue, ok, nil
}

// saves the given music queue for the guild ID in the in-memory map, and returns an error if any occurs
func (r *QueueRepository) Save(_ context.Context, queue domainmusic.Queue) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.queues[queue.GuildID] = queue
	return nil
}

// deletes the music queue for the given guild ID from the in-memory map, and returns an error if any occurs
func (r *QueueRepository) Delete(_ context.Context, guildID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.queues, guildID)
	return nil
}
