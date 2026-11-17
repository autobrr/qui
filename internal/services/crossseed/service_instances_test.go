// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
)

type orderedInstanceStore struct {
	ordered []*models.Instance
	byID    map[int]*models.Instance
	listErr error
}

func newOrderedInstanceStore(instances ...*models.Instance) *orderedInstanceStore {
	byID := make(map[int]*models.Instance, len(instances))
	for _, instance := range instances {
		byID[instance.ID] = instance
	}
	return &orderedInstanceStore{
		ordered: instances,
		byID:    byID,
	}
}

func (s *orderedInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	instance, ok := s.byID[id]
	if !ok {
		return nil, models.ErrInstanceNotFound
	}
	return instance, nil
}

func (s *orderedInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	instances := make([]*models.Instance, len(s.ordered))
	copy(instances, s.ordered)
	return instances, nil
}

func TestResolveInstances_SkipsDisabledInstances(t *testing.T) {
	t.Parallel()

	active := &models.Instance{ID: 1, Name: "active", IsActive: true}
	disabled := &models.Instance{ID: 2, Name: "disabled", IsActive: false}

	svc := &Service{
		instanceStore: newOrderedInstanceStore(active, disabled),
	}

	tests := []struct {
		name      string
		requested []int
	}{
		{name: "global", requested: nil},
		{name: "targeted", requested: []int{active.ID, disabled.ID}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instances, err := svc.resolveInstances(context.Background(), tt.requested)
			require.NoError(t, err)
			require.Len(t, instances, 1)
			require.Equal(t, active.ID, instances[0].ID)
		})
	}
}

type findCrossSeedsSyncManager struct {
	localMatchSyncManager
	sourceTorrent     qbt.Torrent
	cachedInstanceIDs []int
}

func (m *findCrossSeedsSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	m.cachedInstanceIDs = append(m.cachedInstanceIDs, instanceID)
	if instanceID == 1 {
		return []internalqb.CrossInstanceTorrentView{{TorrentView: &internalqb.TorrentView{Torrent: &m.sourceTorrent}, InstanceID: 1}}, nil
	}
	return nil, nil
}

func TestFindCrossSeeds_SkipsDisabledInstances(t *testing.T) {
	t.Parallel()

	active := &models.Instance{ID: 1, Name: "active", IsActive: true}
	disabled := &models.Instance{ID: 2, Name: "disabled", IsActive: false}
	source := qbt.Torrent{
		Hash:        "abc123def456abc123def456abc123def456abc1",
		Name:        "Movie.2025.1080p.BluRay.x264-GRP",
		SavePath:    "/downloads",
		ContentPath: "/downloads/Movie.2025.1080p.BluRay.x264-GRP.mkv",
	}

	syncManager := &findCrossSeedsSyncManager{
		sourceTorrent: source,
	}

	svc := &Service{
		instanceStore: newOrderedInstanceStore(active, disabled),
		syncManager:   syncManager,
		releaseCache:  NewReleaseCache(),
	}

	resp, err := svc.FindCrossSeeds(context.Background(), active.ID, source.Hash)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, []int{active.ID}, syncManager.cachedInstanceIDs)
}
