// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

type staticDirscanIndexerStore struct {
	indexers []*models.TorznabIndexer
}

func (s *staticDirscanIndexerStore) Get(_ context.Context, id int) (*models.TorznabIndexer, error) {
	for _, idx := range s.indexers {
		if idx != nil && idx.ID == id {
			return idx, nil
		}
	}
	return nil, nil
}

func (s *staticDirscanIndexerStore) List(context.Context) ([]*models.TorznabIndexer, error) {
	out := make([]*models.TorznabIndexer, 0, len(s.indexers))
	out = append(out, s.indexers...)
	return out, nil
}

func (s *staticDirscanIndexerStore) ListEnabled(context.Context) ([]*models.TorznabIndexer, error) {
	out := make([]*models.TorznabIndexer, 0, len(s.indexers))
	for _, idx := range s.indexers {
		if idx != nil && idx.Enabled {
			out = append(out, idx)
		}
	}
	return out, nil
}

func (*staticDirscanIndexerStore) GetDecryptedAPIKey(*models.TorznabIndexer) (string, error) {
	return "", nil
}

func (*staticDirscanIndexerStore) GetDecryptedBasicPassword(*models.TorznabIndexer) (string, error) {
	return "", nil
}

func (*staticDirscanIndexerStore) GetCapabilities(context.Context, int) ([]string, error) {
	return []string{}, nil
}

func (*staticDirscanIndexerStore) SetCapabilities(context.Context, int, []string) error {
	return nil
}

func (*staticDirscanIndexerStore) SetCategories(context.Context, int, []models.TorznabIndexerCategory) error {
	return nil
}

func (*staticDirscanIndexerStore) SetLimits(context.Context, int, int, int) error {
	return nil
}

func (*staticDirscanIndexerStore) RecordLatency(context.Context, int, string, int, bool) error {
	return nil
}

func (*staticDirscanIndexerStore) RecordError(context.Context, int, string, string) error {
	return nil
}

func (*staticDirscanIndexerStore) ListRateLimitCooldowns(context.Context) ([]models.TorznabIndexerCooldown, error) {
	return []models.TorznabIndexerCooldown{}, nil
}

func (*staticDirscanIndexerStore) UpsertRateLimitCooldown(context.Context, int, time.Time, time.Duration, string) error {
	return nil
}

func (*staticDirscanIndexerStore) DeleteRateLimitCooldown(context.Context, int) error {
	return nil
}

func newDirscanTestJackettService(indexers []*models.TorznabIndexer) *jackett.Service {
	return jackett.NewService(&staticDirscanIndexerStore{indexers: indexers})
}

func TestFilterOutGazelleTorznabIndexers_RequiresGazelleConfiguration(t *testing.T) {
	svc := &Service{
		jackettService: newDirscanTestJackettService([]*models.TorznabIndexer{
			{ID: 1, Name: "RED", BaseURL: "https://redacted.sh", Enabled: true},
			{ID: 2, Name: "General", BaseURL: "https://tracker.example", Enabled: true},
		}),
	}

	filtered := svc.filterOutGazelleTorznabIndexers(context.Background(), []int{1, 2}, nil)
	require.Equal(t, []int{1, 2}, filtered)
}

func TestFilterOutGazelleTorznabIndexers_ExcludesWhenGazelleConfigured(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dirscan-gazelle-settings.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	crossSeedStore, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	_, err = crossSeedStore.UpsertSettings(context.Background(), &models.CrossSeedAutomationSettings{
		GazelleEnabled: true,
		RedactedAPIKey: "red-key",
	})
	require.NoError(t, err)

	svc := &Service{
		jackettService: newDirscanTestJackettService([]*models.TorznabIndexer{{ID: 1, Name: "RED", BaseURL: "https://redacted.sh", Enabled: true}, {ID: 2, Name: "General", BaseURL: "https://tracker.example", Enabled: true}}),
		crossSeedStore: crossSeedStore,
	}

	filtered := svc.filterOutGazelleTorznabIndexers(context.Background(), []int{1, 2}, nil)
	require.Equal(t, []int{2}, filtered)
}
