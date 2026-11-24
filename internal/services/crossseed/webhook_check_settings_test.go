// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const settingsCheckName = "Harbor.Signal.2026.1080p.WEB-DL.H.264-LUMA"

func settingsCheckService(t *testing.T) *Service {
	t.Helper()
	torrent := qbt.Torrent{Hash: "harbor", Name: settingsCheckName, Category: "films", Size: 4 << 30, TotalSize: 4 << 30, Progress: 1, State: qbt.TorrentStateUploading}
	return &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{localReadsInstance.ID: localReadsInstance}},
		syncManager:      newFakeSyncManager(localReadsInstance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{torrent.Hash: {{Name: settingsCheckName + ".mkv", Size: torrent.Size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
}

func checkSettingsName(t *testing.T, svc *Service) *WebhookCheckResponse {
	t.Helper()
	resp, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{InstanceIDs: []int{localReadsInstance.ID}, TorrentName: settingsCheckName, Size: 4 << 30})
	require.NoError(t, err)
	return resp
}

func TestCheckWebhookLoadsSettingsOnce(t *testing.T) {
	svc := settingsCheckService(t)
	loads := 0
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		loads++
		return &models.CrossSeedAutomationSettings{}, nil
	}

	require.True(t, checkSettingsName(t, svc).CanCrossSeed)
	afterFirst := loads
	require.True(t, checkSettingsName(t, svc).CanCrossSeed)
	require.Equal(t, afterFirst, loads, "a second check loads no settings")
}

func TestCheckWebhookLoadsSettingsAgainAfterFailedLoad(t *testing.T) {
	svc := settingsCheckService(t)
	svc.automationSettingsLoader = failingSettings
	_, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{InstanceIDs: []int{localReadsInstance.ID}, TorrentName: settingsCheckName})
	require.ErrorIs(t, err, errSettingsUnavailable)

	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{}, nil
	}
	require.True(t, checkSettingsName(t, svc).CanCrossSeed)
}

func TestCheckWebhookUsesSavedSettings(t *testing.T) {
	store, err := models.NewCrossSeedStore(testdb.NewMigratedSQLite(t, "webhook-check-settings"), make([]byte, 32))
	require.NoError(t, err)
	svc := settingsCheckService(t)
	svc.automationStore = store
	require.True(t, checkSettingsName(t, svc).CanCrossSeed)

	settings := models.DefaultCrossSeedAutomationSettings()
	settings.WebhookSourceExcludeCategories = []string{"films"}
	_, err = svc.UpdateAutomationSettings(t.Context(), settings)
	require.NoError(t, err)

	resp := checkSettingsName(t, svc)
	require.False(t, resp.CanCrossSeed)
	require.Empty(t, resp.Matches)
}

func TestCheckWebhookReloadsSettingsAfterFailedSave(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "webhook-check-failed-save")
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	svc := settingsCheckService(t)
	svc.automationStore = store
	_, err = svc.UpdateAutomationSettings(t.Context(), models.DefaultCrossSeedAutomationSettings())
	require.NoError(t, err)
	require.True(t, checkSettingsName(t, svc).CanCrossSeed)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER break_read_back AFTER UPDATE ON cross_seed_settings BEGIN UPDATE cross_seed_settings SET target_indexer_ids = 'not json' WHERE id = NEW.id; END`)
	require.NoError(t, err)
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.WebhookSourceExcludeCategories = []string{"films"}
	_, err = svc.UpdateAutomationSettings(t.Context(), settings)
	require.Error(t, err, "the row is written, and its read-back fails")
	_, err = db.ExecContext(t.Context(), `DROP TRIGGER break_read_back`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE cross_seed_settings SET target_indexer_ids = '[]'`)
	require.NoError(t, err)

	resp := checkSettingsName(t, svc)
	require.False(t, resp.CanCrossSeed)
	require.Empty(t, resp.Matches)
}
