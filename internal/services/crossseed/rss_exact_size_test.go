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
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestProcessAutomationCandidateBindsExactSize catches a fallback that
// downloads the result but loses the source-specific authority required by the
// later CrossSeed replay.
func TestProcessAutomationCandidateBindsExactSize(t *testing.T) {
	const (
		instanceID = 7
		sourceHash = "0123456789abcdef0123456789abcdef01234567"
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		size       = int64(1_000_000)
	)

	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, TotalSize: size, Progress: 1, Category: "tv", Tags: "seed"}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{sourceHash: {{Name: sourceName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{
			InstanceID: instanceID, InstanceName: instance.Name, Success: true, Status: "added",
		}}}, nil
	}

	category := "cross-seed"
	status, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:            []int{instanceID},
		Category:                     &category,
		RSSAutomationTags:            []string{"rss"},
		InheritSourceTags:            true,
		StartPaused:                  true,
		SkipAutoResumeRSS:            true,
		SkipPieceBoundarySafetyCheck: true,
		RSSSourceCategories:          []string{"tv"},
		RSSSourceTags:                []string{"seed"},
		RSSSourceExcludeCategories:   []string{"excluded"},
		RSSSourceExcludeTags:         []string{"blocked"},
		FindIndividualEpisodes:       true,
	}, nil, jackett.SearchResult{
		Indexer: "synthetic", IndexerID: 1, Title: resultName, Size: size,
		DownloadURL: "https://example.invalid/download.torrent", GUID: "rss-item",
	}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.Equal(t, 1, downloadCalls)
	require.NotNil(t, captured)
	require.Equal(t, []int{instanceID}, captured.TargetInstanceIDs)
	require.Equal(t, searchCandidateClassExactSizeFallback, captured.SearchDecision.Class)
	require.Equal(t, instanceID, captured.SearchDecision.SourceInstanceID)
	require.Equal(t, normalizeHash(sourceHash), captured.SearchDecision.SourceHash)
	require.Equal(t, "codec mismatch", captured.SearchDecision.StrictMismatchReason)
	require.Equal(t, []string{"codec"}, captured.SearchDecision.RelaxedDifferences)
	require.Equal(t, category, captured.Category)
	require.Equal(t, []string{"rss"}, captured.Tags)
	require.True(t, captured.InheritSourceTags)
	require.True(t, *captured.StartPaused)
	require.True(t, captured.SkipAutoResume)
	require.True(t, captured.SkipPieceBoundarySafetyCheck)
	require.Equal(t, []string{"tv"}, captured.SourceFilterCategories)
	require.Equal(t, []string{"seed"}, captured.SourceFilterTags)
	require.Equal(t, []string{"excluded"}, captured.SourceFilterExcludeCategories)
	require.Equal(t, []string{"blocked"}, captured.SourceFilterExcludeTags)
}

func TestProcessAutomationCandidateExactSizeFallbackRequiresPositiveEqualSizes(t *testing.T) {
	const (
		instanceID = 8
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		size       = int64(1_000_000)
	)

	for _, tt := range []struct {
		name       string
		reportedSz int64
	}{
		{name: "zero", reportedSz: 0},
		{name: "nonexact", reportedSz: size - 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := qbt.Torrent{Hash: "source-" + tt.name, Name: sourceName, TotalSize: size, Progress: 1}
			instance := &models.Instance{ID: instanceID, Name: "main"}
			sync := &announcementLookupCountingSyncManager{fakeSyncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
				source.Hash: {{Name: sourceName + ".mkv", Size: size}},
			})}
			service := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}
			downloadCalls := 0
			crossSeedCalls := 0
			service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
				downloadCalls++
				return []byte("torrent"), nil
			}
			service.crossSeedInvoker = func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
				crossSeedCalls++
				return nil, nil
			}

			_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
				TargetInstanceIDs: []int{instanceID},
			}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: tt.reportedSz}, AutomationRunOptions{}, nil)

			require.NoError(t, err)
			require.Zero(t, downloadCalls)
			require.Zero(t, crossSeedCalls)
			require.Zero(t, sync.lookups)
		})
	}
}

func TestProcessAutomationCandidateStrict(t *testing.T) {
	const (
		instanceID  = 9
		sourceHash  = "strict-source"
		torrentName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		size        = int64(1_000_000)
	)

	source := qbt.Torrent{Hash: sourceHash, Name: torrentName, TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{sourceHash: {{Name: torrentName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil }
	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Success: true, Status: "added"}}}, nil
	}

	_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{instanceID},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: torrentName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.NotNil(t, captured)
	require.False(t, captured.SearchDecision.admitted())
}

func TestProcessAutomationCandidateRecheck(t *testing.T) {
	const (
		instanceID = 10
		size       = int64(1_000_000)
	)

	for _, tt := range []struct {
		name         string
		sourceName   string
		announcement string
		rescueTitles bool
	}{
		{
			name:         "title",
			sourceName:   "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI",
			announcement: "Different.Voyage.S01E05.1080p.WEB-DL.H.264-KIRI",
			rescueTitles: true,
		},
		{
			name:         "season",
			sourceName:   "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI",
			announcement: "Azure.Compass.S02.1080p.WEB-DL.H.264-KIRI",
		},
		{
			name:         "episode",
			sourceName:   "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI",
			announcement: "Azure.Compass.S01E06.1080p.WEB-DL.H.264-KIRI",
		},
		{
			name:         "split group",
			sourceName:   "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch]",
			announcement: "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := qbt.Torrent{Hash: "recheck-" + tt.name, Name: tt.sourceName, TotalSize: size, Progress: 1}
			instance := &models.Instance{ID: instanceID, Name: "main"}
			service := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{source.Hash: {{Name: source.Name + ".mkv", Size: size}}}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}
			downloadCalls := 0
			crossSeedCalls := 0
			service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
				downloadCalls++
				return []byte("torrent"), nil
			}
			service.crossSeedInvoker = func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
				crossSeedCalls++
				return nil, nil
			}
			matches, err := service.findRSSAnnouncementMatches(context.Background(), jackett.SearchResult{Title: tt.announcement, Size: size}, &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:     []int{instanceID},
				RescueTitleMismatches: tt.rescueTitles,
			}, nil)
			require.NoError(t, err)
			require.Len(t, matches, 1)
			require.True(t, searchDecisionRequiresVerification(matches[0].decision))

			_, _, err = service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:     []int{instanceID},
				RescueTitleMismatches: tt.rescueTitles,
				SkipRecheck:           true,
			}, nil, jackett.SearchResult{Indexer: "synthetic", Title: tt.announcement, Size: size}, AutomationRunOptions{}, nil)

			require.NoError(t, err)
			require.Zero(t, downloadCalls)
			require.Zero(t, crossSeedCalls)
		})
	}
}

func TestFindRSSAnnouncementMatches(t *testing.T) {
	const (
		instanceID = 11
		size       = int64(1_000_000)
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	strict := qbt.Torrent{Hash: "strict", Name: resultName, Size: size - 1, TotalSize: size, Progress: 1}
	relaxed := qbt.Torrent{Hash: "relaxed", Name: "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{relaxed, strict}, map[string]qbt.TorrentFiles{
			relaxed.Hash: {{Name: relaxed.Name + ".mkv", Size: size}},
			strict.Hash:  {{Name: strict.Name + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	matches, err := service.findRSSAnnouncementMatches(context.Background(), jackett.SearchResult{Title: resultName, Size: size}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{instanceID},
	}, nil)

	require.NoError(t, err)
	require.Len(t, matches, 2)
	require.Equal(t, strict.Hash, matches[0].torrent.Hash, "the strict match must outrank the exact-size fallback")
	require.Equal(t, searchCandidateClassStrict, matches[0].decision.Class)
	require.Equal(t, normalizeHash(strict.Hash), matches[0].decision.SourceHash)
	require.Equal(t, relaxed.Hash, matches[1].torrent.Hash)
}

func TestFindRSSAnnouncementMatchesPrefersCodecFallbackOverTitleRescue(t *testing.T) {
	const (
		instanceID = 19
		size       = int64(1_000_000)
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	codec := qbt.Torrent{Hash: "codec", Name: "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", TotalSize: size, Progress: 1}
	title := qbt.Torrent{Hash: "title", Name: "Different.Voyage.S01E05.1080p.WEB-DL.H.265-KIRI", TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{codec, title}, map[string]qbt.TorrentFiles{
			codec.Hash: {{Name: codec.Name + ".mkv", Size: size}},
			title.Hash: {{Name: title.Name + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	matches, err := service.findRSSAnnouncementMatches(context.Background(), jackett.SearchResult{Title: resultName, Size: size}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:     []int{instanceID},
		RescueTitleMismatches: true,
	}, nil)

	require.NoError(t, err)
	require.Len(t, matches, 2)
	require.Equal(t, codec.Hash, matches[0].torrent.Hash)
	require.Equal(t, searchCandidateClassExactSizeFallback, matches[0].decision.Class)
	require.False(t, searchDecisionRequiresVerification(matches[0].decision))
	require.Equal(t, title.Hash, matches[1].torrent.Hash)

	downloadCalls := 0
	var captured *CrossSeedRequest
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Success: true, Status: "added"}}}, nil
	}

	_, _, err = service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:     []int{instanceID},
		RescueTitleMismatches: true,
		SkipRecheck:           true,
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, 1, downloadCalls)
	require.NotNil(t, captured)
	require.Equal(t, codec.Hash, captured.SearchDecision.SourceHash)
	require.False(t, searchDecisionRequiresVerification(captured.SearchDecision))
}

func TestProcessAutomationCandidateExactSizeSourceFilters(t *testing.T) {
	const (
		instanceID = 12
		size       = int64(1_000_000)
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	source := qbt.Torrent{Hash: "filtered", Name: sourceName, TotalSize: size, Progress: 1, Category: "excluded"}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{source.Hash: {{Name: sourceName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}

	_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:          []int{instanceID},
		RSSSourceExcludeCategories: []string{"excluded"},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Zero(t, downloadCalls)
}

func TestFindRSSAnnouncementMatchesIncludesOnlyConfiguredCategoryAndTag(t *testing.T) {
	const (
		instanceID = 25
		size       = int64(1_000_000)
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	wrongCategory := qbt.Torrent{Hash: "wrong-category", Name: sourceName, TotalSize: size, Progress: 1, Category: "movies", Tags: "keep"}
	wrongTag := qbt.Torrent{Hash: "wrong-tag", Name: sourceName, TotalSize: size, Progress: 1, Category: "tv", Tags: "ignore"}
	included := qbt.Torrent{Hash: "included", Name: sourceName, TotalSize: size, Progress: 1, Category: "tv", Tags: "keep"}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{wrongCategory, wrongTag, included}, map[string]qbt.TorrentFiles{
			wrongCategory.Hash: {{Name: sourceName + ".mkv", Size: size}},
			wrongTag.Hash:      {{Name: sourceName + ".mkv", Size: size}},
			included.Hash:      {{Name: sourceName + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	matches, err := service.findRSSAnnouncementMatches(context.Background(), jackett.SearchResult{Title: resultName, Size: size}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:   []int{instanceID},
		RSSSourceCategories: []string{"tv"},
		RSSSourceTags:       []string{"keep"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, included.Hash, matches[0].torrent.Hash)
}

func TestProcessAutomationCandidateDuplicate(t *testing.T) {
	const (
		instanceID = 13
		size       = int64(1_000_000)
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		hash       = "0123456789abcdef0123456789abcdef01234567"
	)

	source := qbt.Torrent{Hash: hash, Name: sourceName, TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{hash: {{Name: sourceName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}

	status, returnedHash, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{instanceID},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size, InfoHashV1: hash}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.NotNil(t, returnedHash)
	require.Equal(t, hash, *returnedHash)
	require.Zero(t, downloadCalls)
}

func TestProcessAutomationCandidateDuplicateComment(t *testing.T) {
	const (
		instanceID = 18
		size       = int64(1_000_000)
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		commentURL = "https://example.invalid/torrents/42"
	)

	source := qbt.Torrent{Hash: "comment-source", Name: sourceName, TotalSize: size, Progress: 1, Comment: "uploaded from " + commentURL}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{source.Hash: {{Name: sourceName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}

	status, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{instanceID},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size, GUID: commentURL}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.Zero(t, downloadCalls)
}

func TestProcessAutomationCandidateExactSizeDoesNotReuseDifferentReportedSize(t *testing.T) {
	const (
		instanceID = 14
		size       = int64(1_000_000)
		sourceName = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	source := qbt.Torrent{Hash: "cached-source", Name: sourceName, TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{source.Hash: {{Name: sourceName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	service.crossSeedInvoker = func(_ context.Context, _ *CrossSeedRequest) (*CrossSeedResponse, error) {
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Success: true, Status: "added"}}}, nil
	}
	autoCtx := &automationContext{candidateCache: make(map[string]*FindCandidatesResponse)}
	settings := &models.CrossSeedAutomationSettings{TargetInstanceIDs: []int{instanceID}}

	_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, settings, autoCtx, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)
	require.NoError(t, err)
	_, _, err = service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, settings, autoCtx, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size + 1}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, 1, downloadCalls)
}

func TestProcessAutomationCandidateBindsRecheckDecision(t *testing.T) {
	const (
		instanceID = 15
		size       = int64(1_000_000)
		sourceName = "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch]"
		resultName = "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI"
	)

	source := qbt.Torrent{Hash: "split-group-source", Name: sourceName, TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{source.Hash: {{Name: sourceName + ".mkv", Size: size}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil }
	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Success: true, Status: "added"}}}, nil
	}

	_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{instanceID},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Equal(t, searchCandidateClassExactSizeFallback, captured.SearchDecision.Class)
	require.True(t, searchDecisionRequiresVerification(captured.SearchDecision))
	require.Equal(t, instanceID, captured.SearchDecision.SourceInstanceID)
	require.Equal(t, normalizeHash(source.Hash), captured.SearchDecision.SourceHash)
}

func TestProcessAutomationCandidateExactSizeAcrossInstances(t *testing.T) {
	const (
		firstInstanceID  = 16
		secondInstanceID = 17
		size             = int64(1_000_000)
		sourceName       = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName       = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	first := qbt.Torrent{Hash: "first-source", Name: sourceName, TotalSize: size, Progress: 1}
	second := qbt.Torrent{Hash: "second-source", Name: sourceName, TotalSize: size, Progress: 1}
	firstInstance := &models.Instance{ID: firstInstanceID, Name: "first"}
	secondInstance := &models.Instance{ID: secondInstanceID, Name: "second"}
	sync := newFakeSyncManager(firstInstance, []qbt.Torrent{first}, map[string]qbt.TorrentFiles{first.Hash: {{Name: sourceName + ".mkv", Size: size}}})
	sync.all[secondInstanceID] = []qbt.Torrent{second}
	sync.cached[secondInstanceID] = buildCrossInstanceViews(secondInstance, []qbt.Torrent{second})
	sync.files[normalizeHash(second.Hash)] = qbt.TorrentFiles{{Name: sourceName + ".mkv", Size: size}}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{firstInstanceID: firstInstance, secondInstanceID: secondInstance}},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	var requests []*CrossSeedRequest
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		requests = append(requests, req)
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: req.TargetInstanceIDs[0], Success: true, Status: "added"}}}, nil
	}

	_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{firstInstanceID, secondInstanceID},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, 1, downloadCalls)
	require.Len(t, requests, 2)
	require.Equal(t, []int{firstInstanceID}, requests[0].TargetInstanceIDs)
	require.Equal(t, normalizeHash(first.Hash), requests[0].SearchDecision.SourceHash)
	require.Equal(t, []int{secondInstanceID}, requests[1].TargetInstanceIDs)
	require.Equal(t, normalizeHash(second.Hash), requests[1].SearchDecision.SourceHash)
}

func TestProcessAutomationCandidateExactSizeTriesLowerRankedSourceAfterNoMatch(t *testing.T) {
	const (
		instanceID = 20
		size       = int64(1_000_000)
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	high := qbt.Torrent{Hash: "high", Name: "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", TotalSize: size, Progress: 1}
	lower := qbt.Torrent{Hash: "lower", Name: "Different.Voyage.S01E05.1080p.WEB-DL.H.265-KIRI", TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{high, lower}, map[string]qbt.TorrentFiles{
			high.Hash:  {{Name: high.Name + ".mkv", Size: size}},
			lower.Hash: {{Name: lower.Name + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	var sourceHashes []string
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		sourceHashes = append(sourceHashes, req.SearchDecision.SourceHash)
		if req.SearchDecision.SourceHash == high.Hash {
			return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Status: "no_match"}}}, nil
		}
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Success: true, Status: "added"}}}, nil
	}
	run := &models.CrossSeedRun{}

	status, _, err := service.processAutomationCandidate(context.Background(), run, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:     []int{instanceID},
		RescueTitleMismatches: true,
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.Equal(t, 1, downloadCalls)
	require.Equal(t, []string{high.Hash, lower.Hash}, sourceHashes)
	require.Equal(t, 1, run.TorrentsAdded)
	require.Len(t, run.Results, 1)
	require.True(t, run.Results[0].Success)
}

func TestInvokeBoundAnnouncementMatchesStopsOnTopLevelSuccess(t *testing.T) {
	matches := []boundAnnouncementMatch{
		{instanceID: 1, torrent: qbt.Torrent{Hash: "first"}},
		{instanceID: 1, torrent: qbt.Torrent{Hash: "second"}},
	}
	calls := 0
	service := &Service{
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			calls++
			if calls > 1 {
				t.Fatal("a top-level successful response must stop source retries")
			}
			return &CrossSeedResponse{Success: true}, nil
		},
	}

	response, err := service.invokeBoundAnnouncementMatches(context.Background(), matches, func(boundAnnouncementMatch) *CrossSeedRequest {
		return &CrossSeedRequest{}
	})

	require.NoError(t, err)
	require.True(t, response.Success)
	require.Equal(t, 1, calls)
}

func TestProcessAutomationCandidateExactSizeRetainsSuccessWhenAnotherInstanceErrors(t *testing.T) {
	const (
		firstInstanceID  = 21
		secondInstanceID = 22
		size             = int64(1_000_000)
		sourceName       = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
		resultName       = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	first := qbt.Torrent{Hash: "first", Name: sourceName, TotalSize: size, Progress: 1}
	second := qbt.Torrent{Hash: "second", Name: sourceName, TotalSize: size, Progress: 1}
	firstInstance := &models.Instance{ID: firstInstanceID, Name: "first"}
	secondInstance := &models.Instance{ID: secondInstanceID, Name: "second"}
	sync := newFakeSyncManager(firstInstance, []qbt.Torrent{first}, map[string]qbt.TorrentFiles{first.Hash: {{Name: sourceName + ".mkv", Size: size}}})
	sync.all[secondInstanceID] = []qbt.Torrent{second}
	sync.cached[secondInstanceID] = buildCrossInstanceViews(secondInstance, []qbt.Torrent{second})
	sync.files[normalizeHash(second.Hash)] = qbt.TorrentFiles{{Name: sourceName + ".mkv", Size: size}}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{firstInstanceID: firstInstance, secondInstanceID: secondInstance}},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil }
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		if req.TargetInstanceIDs[0] == firstInstanceID {
			return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: firstInstanceID, InstanceName: firstInstance.Name, Success: true, Status: "added"}}}, nil
		}
		return nil, errors.New("second instance unavailable")
	}
	run := &models.CrossSeedRun{}

	status, _, err := service.processAutomationCandidate(context.Background(), run, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{firstInstanceID, secondInstanceID},
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.Error(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.Equal(t, 1, run.TorrentsAdded)
	require.Equal(t, 1, run.TorrentsFailed)
	require.Len(t, run.Results, 2)
	require.True(t, run.Results[0].Success)
	require.Equal(t, "error", run.Results[1].Status)
}

func TestProcessAutomationCandidateExactSizeSkipRecheckKeepsSafeInstances(t *testing.T) {
	const (
		safeInstanceID = 23
		hardInstanceID = 24
		size           = int64(1_000_000)
		resultName     = "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI"
	)

	safe := qbt.Torrent{Hash: "safe", Name: "Azure.Compass.S01.1080p.WEB-DL.H.265-KIRI", TotalSize: size, Progress: 1}
	hard := qbt.Torrent{Hash: "hard", Name: "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch]", TotalSize: size, Progress: 1}
	safeInstance := &models.Instance{ID: safeInstanceID, Name: "safe"}
	hardInstance := &models.Instance{ID: hardInstanceID, Name: "hard"}
	sync := newFakeSyncManager(safeInstance, []qbt.Torrent{safe}, map[string]qbt.TorrentFiles{safe.Hash: {{Name: safe.Name + ".mkv", Size: size}}})
	sync.all[hardInstanceID] = []qbt.Torrent{hard}
	sync.cached[hardInstanceID] = buildCrossInstanceViews(hardInstance, []qbt.Torrent{hard})
	sync.files[normalizeHash(hard.Hash)] = qbt.TorrentFiles{{Name: hard.Name + ".mkv", Size: size}}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{safeInstanceID: safeInstance, hardInstanceID: hardInstance}},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	var requests []*CrossSeedRequest
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		requests = append(requests, req)
		return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: safeInstanceID, Success: true, Status: "added"}}}, nil
	}

	_, _, err := service.processAutomationCandidate(context.Background(), &models.CrossSeedRun{}, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs: []int{safeInstanceID, hardInstanceID},
		SkipRecheck:       true,
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, 1, downloadCalls)
	require.Len(t, requests, 1)
	require.Equal(t, []int{safeInstanceID}, requests[0].TargetInstanceIDs)
	require.False(t, searchDecisionRequiresVerification(requests[0].SearchDecision))
}

func TestProcessAutomationCandidateExactSizeInfohashPrecheckGroupsSourcesByInstance(t *testing.T) {
	const (
		instanceID = 26
		size       = int64(1_000_000)
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		hash       = "0123456789abcdef0123456789abcdef01234567"
	)

	first := qbt.Torrent{Hash: hash, Name: "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", TotalSize: size, Progress: 1}
	second := qbt.Torrent{Hash: "second", Name: "Different.Voyage.S01E05.1080p.WEB-DL.H.265-KIRI", TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{first, second}, map[string]qbt.TorrentFiles{
			first.Hash:  {{Name: first.Name + ".mkv", Size: size}},
			second.Hash: {{Name: second.Name + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	run := &models.CrossSeedRun{}

	status, _, err := service.processAutomationCandidate(context.Background(), run, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:     []int{instanceID},
		RescueTitleMismatches: true,
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size, InfoHashV1: hash}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.Zero(t, downloadCalls)
	require.Len(t, run.Results, 1)
	require.Equal(t, "exists", run.Results[0].Status)
}

func TestProcessAutomationCandidateExactSizeCommentPrecheckGroupsSourcesByInstance(t *testing.T) {
	const (
		instanceID = 27
		size       = int64(1_000_000)
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
		commentURL = "https://example.invalid/torrents/99"
	)

	first := qbt.Torrent{Hash: "first", Name: "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", TotalSize: size, Progress: 1}
	second := qbt.Torrent{Hash: "second", Name: "Different.Voyage.S01E05.1080p.WEB-DL.H.265-KIRI", TotalSize: size, Progress: 1, Comment: "source " + commentURL}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{first, second}, map[string]qbt.TorrentFiles{
			first.Hash:  {{Name: first.Name + ".mkv", Size: size}},
			second.Hash: {{Name: second.Name + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	downloadCalls := 0
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
		downloadCalls++
		return []byte("torrent"), nil
	}
	run := &models.CrossSeedRun{}

	status, _, err := service.processAutomationCandidate(context.Background(), run, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:     []int{instanceID},
		RescueTitleMismatches: true,
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size, GUID: commentURL}, AutomationRunOptions{}, nil)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	require.Zero(t, downloadCalls)
	require.Len(t, run.Results, 1)
	require.Equal(t, "exists", run.Results[0].Status)
}

func TestProcessAutomationCandidateExactSizeNoMatchThenErrorFailsInstance(t *testing.T) {
	const (
		instanceID = 28
		size       = int64(1_000_000)
		resultName = "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI"
	)

	first := qbt.Torrent{Hash: "first", Name: "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI", TotalSize: size, Progress: 1}
	second := qbt.Torrent{Hash: "second", Name: "Different.Voyage.S01E05.1080p.WEB-DL.H.265-KIRI", TotalSize: size, Progress: 1}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{first, second}, map[string]qbt.TorrentFiles{
			first.Hash:  {{Name: first.Name + ".mkv", Size: size}},
			second.Hash: {{Name: second.Name + ".mkv", Size: size}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	service.torrentDownloadFunc = func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) { return []byte("torrent"), nil }
	service.crossSeedInvoker = func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		if req.SearchDecision.SourceHash == first.Hash {
			return &CrossSeedResponse{Results: []InstanceCrossSeedResult{{InstanceID: instanceID, Status: "no_match"}}}, nil
		}
		return nil, errors.New("second source unavailable")
	}
	run := &models.CrossSeedRun{}

	status, _, err := service.processAutomationCandidate(context.Background(), run, &models.CrossSeedAutomationSettings{
		TargetInstanceIDs:     []int{instanceID},
		RescueTitleMismatches: true,
	}, nil, jackett.SearchResult{Indexer: "synthetic", Title: resultName, Size: size}, AutomationRunOptions{}, nil)

	require.Error(t, err)
	require.Equal(t, models.CrossSeedFeedItemStatusFailed, status)
	require.Equal(t, 1, run.TorrentsFailed)
	require.Len(t, run.Results, 1)
	require.Equal(t, "error", run.Results[0].Status)
}

func TestMergeCrossSeedResponsesPreservesAggregateSuccess(t *testing.T) {
	destination := &CrossSeedResponse{}
	source := &CrossSeedResponse{Success: true, titleRescueUsed: true}

	mergeCrossSeedResponses(destination, source)

	require.True(t, destination.Success)
	require.True(t, destination.titleRescueUsed)
}
