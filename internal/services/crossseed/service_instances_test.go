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
	"github.com/autobrr/qui/pkg/stringutils"
)

type orderedInstanceStore struct {
	ordered []*models.Instance
	byID    map[int]*models.Instance
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

type findLocalMatchesSyncManager struct {
	localMatchSyncManager
	sourceTorrent     qbt.Torrent
	cachedInstanceIDs []int
	cachedByInstance  map[int][]internalqb.CrossInstanceTorrentView
}

//nolint:gocritic // Interface requires value type for TorrentFilterOptions
func (m *findLocalMatchesSyncManager) GetTorrents(_ context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if instanceID == 1 && len(filter.Hashes) == 1 && normalizeHash(filter.Hashes[0]) == normalizeHash(m.sourceTorrent.Hash) {
		return []qbt.Torrent{m.sourceTorrent}, nil
	}
	return nil, nil
}

func (m *findLocalMatchesSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	m.cachedInstanceIDs = append(m.cachedInstanceIDs, instanceID)
	if m.cachedByInstance == nil {
		return nil, nil
	}
	return m.cachedByInstance[instanceID], nil
}

func TestFindLocalMatches_SkipsDisabledInstances(t *testing.T) {
	t.Parallel()

	active := &models.Instance{ID: 1, Name: "active", IsActive: true}
	disabled := &models.Instance{ID: 2, Name: "disabled", IsActive: false}
	source := qbt.Torrent{
		Hash:        "abc123def456abc123def456abc123def456abc1",
		Name:        "Movie.2025.1080p.BluRay.x264-GRP",
		SavePath:    "/downloads",
		ContentPath: "/downloads/Movie.2025.1080p.BluRay.x264-GRP.mkv",
	}

	syncManager := &findLocalMatchesSyncManager{
		sourceTorrent: source,
	}

	svc := &Service{
		instanceStore: newOrderedInstanceStore(active, disabled),
		syncManager:   syncManager,
		releaseCache:  NewReleaseCache(),
	}

	resp, err := svc.FindLocalMatches(context.Background(), active.ID, source.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, []int{active.ID}, syncManager.cachedInstanceIDs)
}

func TestFindDeleteAffectedMatches_ContentPathOnly(t *testing.T) {
	t.Parallel()

	active := &models.Instance{ID: 1, Name: "active", IsActive: true}
	other := &models.Instance{ID: 2, Name: "other", IsActive: true}
	source := qbt.Torrent{
		Hash:        "abc123def456abc123def456abc123def456abc1",
		Name:        "Movie.2025.1080p.WEB.h264",
		SavePath:    "/downloads/source",
		ContentPath: "/downloads/source/Movie.2025.1080p.WEB.h264.mkv",
	}

	syncManager := &findLocalMatchesSyncManager{
		sourceTorrent: source,
	}
	syncManager.files = map[string]qbt.TorrentFiles{
		normalizeHash(source.Hash): {
			{Name: "Movie.2025.1080p.WEB.h264.mkv", Size: 100},
		},
	}
	syncManager.cachedInstanceIDs = nil

	syncManager.files[normalizeHash("sameinst-content")] = qbt.TorrentFiles{
		{Name: "Movie.2025.1080p.WEB.h264.mkv", Size: 100},
	}

	sameInstance := []internalqb.CrossInstanceTorrentView{
		{
			TorrentView: &internalqb.TorrentView{Torrent: &qbt.Torrent{
				Hash:        "sameinst-name",
				Name:        source.Name,
				SavePath:    "/downloads/name",
				ContentPath: "/downloads/name/another-copy.mkv",
			}},
			InstanceID: active.ID,
		},
		{
			TorrentView: &internalqb.TorrentView{Torrent: &qbt.Torrent{
				Hash:        "sameinst-release",
				Name:        "Movie.2025.1080p.WEB.h264-OTHER",
				SavePath:    "/downloads/release",
				ContentPath: "/downloads/release/movie-copy.mkv",
			}},
			InstanceID: active.ID,
		},
		{
			TorrentView: &internalqb.TorrentView{Torrent: &qbt.Torrent{
				Hash:        "sameinst-content",
				Name:        "Movie.2025.1080p.WEB.h264.Sample",
				SavePath:    "/downloads/content",
				ContentPath: source.ContentPath,
			}},
			InstanceID: active.ID,
		},
	}

	otherInstance := []internalqb.CrossInstanceTorrentView{
		{
			TorrentView: &internalqb.TorrentView{Torrent: &qbt.Torrent{
				Hash:        "otherinst-name",
				Name:        source.Name,
				SavePath:    "/downloads/other",
				ContentPath: "/downloads/other/other-copy.mkv",
			}},
			InstanceID: other.ID,
		},
	}

	syncManager.cachedByInstance = map[int][]internalqb.CrossInstanceTorrentView{
		active.ID: sameInstance,
		other.ID:  otherInstance,
	}

	svc := &Service{
		instanceStore:    newOrderedInstanceStore(active, other),
		syncManager:      syncManager,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	matches, err := svc.FindDeleteAffectedMatches(context.Background(), active.ID, source.Hash)
	require.NoError(t, err)
	require.Len(t, matches, 1)

	gotByHash := make(map[string]string, len(matches))
	for _, match := range matches {
		gotByHash[match.Hash] = match.MatchType
	}

	require.Equal(t, matchTypeContentPath, gotByHash["sameinst-content"])
	require.NotContains(t, gotByHash, "sameinst-name")
	require.NotContains(t, gotByHash, "sameinst-release")
	require.NotContains(t, gotByHash, "otherinst-name")
}

func TestFindDeleteAffectedMatches_IgnoresOtherInstanceVerificationErrors(t *testing.T) {
	t.Parallel()

	active := &models.Instance{ID: 1, Name: "active", IsActive: true}
	other := &models.Instance{ID: 2, Name: "other", IsActive: true}
	source := qbt.Torrent{
		Hash:        "source-hash",
		Name:        "Show.S01",
		SavePath:    "/downloads/show",
		ContentPath: "/downloads/show",
	}

	syncManager := &findLocalMatchesSyncManager{
		sourceTorrent: source,
		localMatchSyncManager: localMatchSyncManager{
			files: map[string]qbt.TorrentFiles{
				normalizeHash(source.Hash): {
					{Name: "Show/S01E01.mkv", Size: 100},
					{Name: "Show/S01E02.mkv", Size: 100},
				},
				normalizeHash("sameinst-content"): {
					{Name: "Show/S01E01.mkv", Size: 100},
					{Name: "Show/S01E02.mkv", Size: 100},
				},
			},
		},
		cachedByInstance: map[int][]internalqb.CrossInstanceTorrentView{
			active.ID: {
				{
					TorrentView: &internalqb.TorrentView{Torrent: &qbt.Torrent{
						Hash:        "sameinst-content",
						Name:        "Show.S01.Pack",
						SavePath:    "/downloads/show",
						ContentPath: "/downloads/show",
					}},
					InstanceID: active.ID,
				},
			},
			other.ID: {
				{
					TorrentView: &internalqb.TorrentView{Torrent: &qbt.Torrent{
						Hash:        "otherinst-ambiguous",
						Name:        "Show.S01.Other",
						SavePath:    "/downloads/show",
						ContentPath: "/downloads/show",
					}},
					InstanceID: other.ID,
				},
			},
		},
	}

	svc := &Service{
		instanceStore:    newOrderedInstanceStore(active, other),
		syncManager:      syncManager,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	matches, err := svc.FindDeleteAffectedMatches(context.Background(), active.ID, source.Hash)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, "sameinst-content", matches[0].Hash)
	require.Equal(t, []int{active.ID}, syncManager.cachedInstanceIDs)
}
