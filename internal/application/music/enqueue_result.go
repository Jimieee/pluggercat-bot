package musicapp

import domainmusic "pluggercat/internal/domain/music"

type EnqueueResult struct {
	LeadTrack      domainmusic.Track
	Started        bool
	AddedCount     int
	LoadType       domainmusic.LoadResultType
	CollectionName string
}

func (r EnqueueResult) IsCollection() bool {
	return r.LoadType == domainmusic.LoadResultTypePlaylist && r.AddedCount > 1
}
