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

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestExecuteCompletionSearchPropagatesExactSizeDecision(t *testing.T) {
	const (
		instanceID    = 1
		indexerID     = 7
		sourceHash    = "0123456789abcdef0123456789abcdef01234567"
		sourceName    = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		candidateName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		reportedSize  = int64(2_147_483_648)
	)

	var searchRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.Header().Set("Content-Type", "application/xml")
			_, err := fmt.Fprint(w, `<caps><limits default="100" max="100"/><searching>
				<search available="yes" supportedParams="q"/>
				<tv-search available="yes" supportedParams="q,season,ep"/>
			</searching><categories><category id="5000" name="TV"/></categories></caps>`)
			if err != nil {
				t.Errorf("write Torznab caps response: %v", err)
			}
			return
		}
		searchRequests.Add(1)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, err := fmt.Fprintf(w, `<rss version="2.0"><channel><title>Completion Indexer</title><item>
			<title>%s</title><guid>completion-exact-size</guid><size>%d</size>
			<enclosure url="%s/candidate.torrent" length="%d" type="application/x-bittorrent" />
		</item></channel></rss>`, candidateName, reportedSize, server.URL, reportedSize)
		if err != nil {
			t.Errorf("write Torznab response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	source := qbt.Torrent{
		Hash:      sourceHash,
		Name:      sourceName,
		Size:      reportedSize,
		TotalSize: reportedSize,
		Progress:  1,
	}
	settings := models.DefaultCrossSeedAutomationSettings()
	filterCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
	filterCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{indexerID},
		FilteredIndexers:      []int{indexerID},
	}, ttlcache.DefaultTTL)

	var captured *CrossSeedRequest
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{
				ID:             indexerID,
				Name:           "Completion Indexer",
				BaseURL:        server.URL,
				Backend:        models.TorznabBackendNative,
				TimeoutSeconds: 5,
				Enabled:        true,
			},
		}),
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			sourceHash: {{Name: sourceName + ".mkv", Size: reportedSize}},
		}),
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		searchResultCache:   ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
			captured = request
			return &CrossSeedResponse{Success: true}, nil
		},
	}

	err := service.executeCompletionSearch(context.Background(), instanceID, &source, settings, &models.InstanceCrossSeedCompletionSettings{
		InstanceID: instanceID,
		Enabled:    true,
		IndexerIDs: []int{indexerID},
	})

	require.NoError(t, err)
	require.Positive(t, searchRequests.Load(), "completion must query the Torznab endpoint")
	require.NotNil(t, captured)
	require.Equal(t, searchCandidateClassExactSizeFallback, captured.SearchDecision.Class)
	require.Equal(t, instanceID, captured.SearchDecision.SourceInstanceID)
	require.Equal(t, sourceHash, captured.SearchDecision.SourceHash)
	require.Equal(t, "codec mismatch", captured.SearchDecision.StrictMismatchReason)
	require.Equal(t, []string{"codec"}, captured.SearchDecision.RelaxedDifferences)
	require.Equal(t, candidateName, captured.SearchDecision.SearchCandidateName)
}
