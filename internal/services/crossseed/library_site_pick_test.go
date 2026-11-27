// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestSearchRunLoop_AsksTheRedOpsSitesOfTheSitePick(t *testing.T) {
	tests := []struct {
		name string
		pick models.SitePick
		want []string
	}{
		{name: "an empty pick asks every RED/OPS site", pick: models.SitePick{}, want: []string{"orpheus.network", "redacted.sh"}},
		{name: "a RED pick asks only RED", pick: models.SitePick{GazelleSites: []string{"redacted.sh"}}, want: []string{"redacted.sh"}},
		{name: "a Torznab pick with Torznab off asks every RED/OPS site", pick: models.SitePick{IndexerIDs: []int{4}}, want: []string{"orpheus.network", "redacted.sh"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := "Lantern Choir - Glass Rivers (2022) [WEB FLAC]"
			torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 11), Name: name, Progress: 1.0, Size: 1_000, Tracker: "https://tracker.example.invalid/announce"}
			svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-site-pick", &hashFilteringSyncManager{
				torrents:    []qbt.Torrent{torrent},
				filesByHash: map[string]qbt.TorrentFiles{torrent.Hash: {{Name: name + "/01 - Track.flac", Size: 1_000}}},
			})
			redClient, err := gazellemusic.NewClient("redacted.sh", gazelleTestServer.URL, "red-key")
			require.NoError(t, err)
			state.gazelleClients.byHost["redacted.sh"] = redClient
			state.opts.SitePick = tc.pick
			var (
				mu          sync.Mutex
				lookupHosts []string
			)
			findGazelleMatch = func(_ context.Context, client *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
				mu.Lock()
				defer mu.Unlock()
				lookupHosts = append(lookupHosts, client.Host())
				return nil, nil
			}

			runSearchLoopToCompletion(t, svc, state)

			slices.Sort(lookupHosts)
			require.Equal(t, tc.want, lookupHosts)
		})
	}
}

func TestGazelleCooldownCountsOnlyThePickedSites(t *testing.T) {
	notPicked := GazelleLookup{Site: "orpheus.network"}.notAsked(GazelleReasonNotSelected)
	require.True(t, gazelleCooldownDue([]GazelleLookup{{Site: "redacted.sh", Status: GazelleNoMatch}, notPicked}, false), "a picked site that answered starts the cooldown")
	require.True(t, gazelleCooldownDue([]GazelleLookup{GazelleLookup{Site: "redacted.sh"}.notAsked(GazelleReasonNotMusic), notPicked}, false), "a failed music check on a picked site starts the cooldown")
	require.False(t, gazelleCooldownDue([]GazelleLookup{GazelleLookup{Site: "redacted.sh"}.notAsked(GazelleReasonNotSelected), notPicked}, false), "a pick with no RED/OPS site starts no RED/OPS cooldown")
}

func TestStartSearchRun_TorznabPickWithoutTorznabAsksNoRedOpsSite(t *testing.T) {
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "crossseed-start-torznab-pick-no-torznab")
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	_, err = store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "red-key"})
	require.NoError(t, err)
	svc := &Service{
		instanceStore:    instanceStore,
		automationStore:  store,
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{}, map[string]qbt.TorrentFiles{}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	_, err = svc.StartSearchRun(ctx, SearchRunOptions{InstanceID: instance.ID, SitePick: models.SitePick{IndexerIDs: []int{7}}})
	require.ErrorIs(t, err, ErrNoIndexersConfigured)
}
