// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
)

type gatedSyncManager struct {
	*scheduleSyncManager
	gate chan struct{}
}

func (m *gatedSyncManager) GetTorrentRefs(ctx context.Context, instanceID int, opts internalqb.TorrentRefFilter) ([]*qbt.Torrent, error) {
	select {
	case <-m.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return m.scheduleSyncManager.GetTorrentRefs(ctx, instanceID, opts)
}

func newRedOpsPickFixture(t *testing.T, dbName string) (*scheduleFixture, *gatedSyncManager) {
	t.Helper()
	f := newScheduleFixture(t, dbName, "http://127.0.0.1:1")
	_, err := f.store.UpsertSettings(t.Context(), &models.CrossSeedAutomationSettings{
		GazelleEnabled:              true,
		RedactedAPIKey:              "red-key",
		SeasonPackAutomationEnabled: true,
	})
	require.NoError(t, err)
	gated := &gatedSyncManager{scheduleSyncManager: f.qbt, gate: make(chan struct{})}
	f.svc.syncManager = gated
	return f, gated
}

func (f *scheduleFixture) heldRunOpts(t *testing.T, gated *gatedSyncManager) SearchRunOptions {
	t.Helper()
	f.svc.searchMu.RLock()
	opts := f.svc.searchState.opts
	f.svc.searchMu.RUnlock()
	close(gated.gate)
	f.waitRunEnd(t)
	return opts
}

func TestLibraryScan_RedOpsPickKeepsTenSecondsAndSkipsTheSeasonPackSearch(t *testing.T) {
	tests := []struct {
		name         string
		pick         models.SitePick
		wantInterval int
		wantPacks    bool
	}{
		{name: "a RED/OPS-only pick", pick: models.SitePick{GazelleSites: []string{"redacted.sh"}}, wantInterval: 10},
		{name: "an empty pick asks Torznab too", pick: models.SitePick{}, wantInterval: minSearchIntervalSecondsTorznab, wantPacks: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			f, gated := newRedOpsPickFixture(t, "crossseed-red-ops-pick-interval")

			_, err := f.svc.PatchSearchSettings(ctx, SearchSettingsPatch{
				InstanceIDSet:   true,
				InstanceID:      &f.instanceID,
				SitePick:        &tc.pick,
				IntervalSeconds: new(10),
			})
			require.NoError(t, err)
			f.drainWake()
			reloaded, err := f.svc.GetSearchSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.wantInterval, reloaded.IntervalSeconds, "settings reload")

			run, err := f.svc.StartSearchRun(ctx, SearchRunOptions{InstanceID: f.instanceID, SitePick: reloaded.SitePick, IntervalSeconds: reloaded.IntervalSeconds})
			require.NoError(t, err)
			require.Equal(t, tc.wantInterval, run.IntervalSeconds, "Start")
			opts := f.heldRunOpts(t, gated)
			require.Equal(t, tc.wantPacks, opts.EnsembleSeasonSearch, "season pack search of Start")

			gated.gate = make(chan struct{})
			scheduled, err := f.svc.RunScheduledSearchNow(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.wantInterval, scheduled.IntervalSeconds, "scheduled run")
			opts = f.heldRunOpts(t, gated)
			require.Equal(t, tc.wantPacks, opts.EnsembleSeasonSearch, "season pack search of the scheduled run")
		})
	}
}

func TestStartSearchRun_TorznabPickWithNoEnabledIndexerFailsWithAPickReason(t *testing.T) {
	f := newScheduleFixture(t, "crossseed-torznab-pick-no-indexer", "http://127.0.0.1:1")
	f.svc.jackettService = newJackettServiceWithIndexers(nil)

	_, err := f.svc.StartSearchRun(t.Context(), SearchRunOptions{InstanceID: f.instanceID, SitePick: models.SitePick{IndexerIDs: []int{7}}})

	require.ErrorIs(t, err, ErrInvalidRequest)
	require.ErrorIs(t, err, errTorznabPickNoIndexer)
	require.Equal(t, scheduleBlockedTorznabPickNoIndexer, scheduleBlockedReason(err))
}

func TestStartSearchRun_FailedIndexerLookupAsksOnlyTheRedOpsSitesWithAKey(t *testing.T) {
	f, gated := newRedOpsPickFixture(t, "crossseed-pick-indexer-lookup-fails")
	f.svc.jackettService = newFailingJackettService(errors.New("indexer list failed"))

	run, err := f.svc.StartSearchRun(t.Context(), SearchRunOptions{InstanceID: f.instanceID})
	require.NoError(t, err)
	require.Empty(t, run.IndexerIDs)
	opts := f.heldRunOpts(t, gated)
	require.Equal(t, models.SitePick{IndexerIDs: []int{}, GazelleSites: []string{}}, opts.SitePick, "the run asks no Torznab indexer")

	loaded, err := f.store.GetSearchRun(t.Context(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.ErrorMessage)
	require.Contains(t, *loaded.ErrorMessage, "indexer list failed", "the run history shows the failed lookup")
}

func TestStartSearchRun_TorznabPickStartsWhenTheIndexerLookupFails(t *testing.T) {
	f := newScheduleFixture(t, "crossseed-torznab-pick-lookup-fails", "http://127.0.0.1:1")
	f.svc.jackettService = newFailingJackettService(errors.New("indexer list failed"))

	run, err := f.svc.StartSearchRun(t.Context(), SearchRunOptions{InstanceID: f.instanceID, SitePick: models.SitePick{IndexerIDs: []int{7}}})
	require.NoError(t, err)
	require.Equal(t, []int{7}, run.IndexerIDs, "the run is in the history with the picked indexer")
	f.waitRunEnd(t)
}
