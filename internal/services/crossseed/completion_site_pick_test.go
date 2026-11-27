// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

type sitePickAsks struct {
	redAPI, opsAPI, musicTorznab, redTorznab atomic.Int32
}

func TestExecuteCompletionSearch_AsksTheSitesOfTheSitePick(t *testing.T) {
	const (
		red = "redacted.sh"
		ops = "orpheus.network"
	)
	tests := []struct {
		name                                     string
		pick                                     models.SitePick
		redAPI, opsAPI, musicTorznab, redTorznab bool
	}{
		{name: "an empty pick asks every site", pick: models.SitePick{}, redAPI: true, opsAPI: true, musicTorznab: true},
		{name: "a RED-only pick asks RED through the API and no Torznab indexer", pick: models.SitePick{IndexerIDs: []int{}, GazelleSites: []string{red}}, redAPI: true},
		{name: "a Torznab-only pick asks no RED/OPS site", pick: models.SitePick{IndexerIDs: []int{7}, GazelleSites: []string{}}, musicTorznab: true},
		{name: "a pick of the RED Torznab indexer that the RED key replaces asks RED through the API", pick: models.SitePick{IndexerIDs: []int{8}, GazelleSites: []string{ops}}, redAPI: true, opsAPI: true},
		{name: "a migrated pick asks every RED/OPS site beside its Torznab indexers", pick: models.SitePick{IndexerIDs: []int{7}, GazelleSites: []string{ops, red}}, redAPI: true, opsAPI: true, musicTorznab: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			asks := &sitePickAsks{}
			service, source, settings := newCompletionSitePickRig(t, asks)

			err := service.executeCompletionSearch(t.Context(), 1, source, settings, &models.InstanceCrossSeedCompletionSettings{
				InstanceID: 1,
				Enabled:    true,
				SitePick:   tc.pick,
			})

			require.NoError(t, err)
			require.Equal(t, tc.redAPI, asks.redAPI.Load() > 0, "RED API")
			require.Equal(t, tc.opsAPI, asks.opsAPI.Load() > 0, "OPS API")
			require.Equal(t, tc.musicTorznab, asks.musicTorznab.Load() > 0, "music Torznab indexer")
			require.Equal(t, tc.redTorznab, asks.redTorznab.Load() > 0, "RED Torznab indexer")
		})
	}
}

func newCompletionSitePickRig(t *testing.T, asks *sitePickAsks) (*Service, *qbt.Torrent, *models.CrossSeedAutomationSettings) {
	t.Helper()
	const (
		instanceID = 1
		sourceHash = "89abcdef0123456789abcdef0123456789abcdef"
		sourceName = "Velvet Orchard - Paper Lanterns (2023) [FLAC]"
	)

	gazelleAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "red-key":
			asks.redAPI.Add(1)
		case "ops-key":
			asks.opsAPI.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","response":{"results":[]}}`)
	}))
	t.Cleanup(gazelleAPI.Close)
	prevNewClient := newGazelleClient
	newGazelleClient = func(host, _, apiKey string) (*gazellemusic.Client, error) {
		return gazellemusic.NewClient(host, gazelleAPI.URL, apiKey)
	}
	t.Cleanup(func() { newGazelleClient = prevNewClient })

	torznabFake := func(searches *atomic.Int32) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("t") == "caps" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = fmt.Fprint(w, `<caps><limits default="100" max="100"/><searching>
					<search available="yes" supportedParams="q"/>
					<music-search available="yes" supportedParams="q"/>
				</searching><categories><category id="3000" name="Audio"/></categories></caps>`)
				return
			}
			searches.Add(1)
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprint(w, `<rss version="2.0"><channel><title>Torznab</title></channel></rss>`)
		}))
		t.Cleanup(server.Close)
		return server
	}
	musicIndexer := torznabFake(&asks.musicTorznab)
	redTorznab := torznabFake(&asks.redTorznab)

	db := testdb.NewMigratedSQLite(t, "completion-site-pick")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	settings := &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "red-key", OrpheusAPIKey: "ops-key"}
	_, err = store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: 123, Progress: 1, Tracker: "https://tracker.example.invalid/announce"}
	filterCache := ttlcache.New[string, *AsyncIndexerFilteringState]()
	filterCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{7, 8},
		FilteredIndexers:      []int{7, 8},
		contentType:           "music",
	}, ttlcache.DefaultTTL)

	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: {ID: instanceID, Name: "music"}}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 7, Name: "Music Indexer", BaseURL: musicIndexer.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true},
			{ID: 8, Name: "Redacted", BaseURL: redTorznab.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true},
		}),
		syncManager: &hashFilteringSyncManager{gazelleSkipHashSyncManager{
			torrents:    []qbt.Torrent{source},
			filesByHash: map[string]qbt.TorrentFiles{sourceHash: {{Name: sourceName + "/01 - Velvet Orchard - Paper Lanterns.flac", Size: 123}}},
		}},
		automationStore:     store,
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		searchResultCache:   ttlcache.New[string, cachedTorrentSearchResults](),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
	}
	return service, &source, settings
}
