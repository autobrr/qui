package crossseed

import (
	"context"
	"slices"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

type completionSearchStandIn struct {
	script func(context.Context, *qbt.Torrent) error

	mu       sync.Mutex
	torrents []qbt.Torrent
}

func newCompletionSearchStandIn(script func(context.Context, *qbt.Torrent) error) *completionSearchStandIn {
	return &completionSearchStandIn{script: script}
}

func (s *completionSearchStandIn) executeCompletionSearch(ctx context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
	s.mu.Lock()
	s.torrents = append(s.torrents, *torrent)
	s.mu.Unlock()
	return s.script(ctx, torrent)
}

func (s *completionSearchStandIn) Searches() []qbt.Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.torrents)
}
