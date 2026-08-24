// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestCheckWebhookReportedSizeFallback catches a check that applies relaxed
// metadata matching without exact announced byte evidence, or labels a
// tolerance-only strict match as exact.
func TestCheckWebhookReportedSizeFallback(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 11, Name: "primary", IsActive: true}
	tests := []struct {
		name          string
		request       *WebhookCheckRequest
		source        qbt.Torrent
		wantMatch     bool
		wantMatchType string
	}{
		{
			name: "strict metadata within tolerance reports size",
			request: &WebhookCheckRequest{
				InstanceIDs: []int{instance.ID},
				TorrentName: "Starbound.Route.2025.1080p.WEB-DL.H.264-LUMA",
				Size:        1_000_500,
			},
			source: qbt.Torrent{
				Hash: "strict-tolerance", Name: "Starbound.Route.2025.1080p.WEB-DL.H.264-LUMA",
				Size: 1_000_000, TotalSize: 1_000_000, Progress: 1,
			},
			wantMatch: true, wantMatchType: "size",
		},
		{
			name: "equal positive bytes permit codec difference as exact",
			request: &WebhookCheckRequest{
				InstanceIDs: []int{instance.ID},
				TorrentName: "Starbound.Route.2025.1080p.WEB-DL.H.265-LUMA",
				Size:        2_000_000,
			},
			source: qbt.Torrent{
				Hash: "codec-exact", Name: "Starbound.Route.2025.1080p.WEB-DL.H.264-LUMA",
				Size: 1_000_000, TotalSize: 2_000_000, Progress: 1,
			},
			wantMatch: true, wantMatchType: "exact",
		},
		{
			name: "nonexact positive bytes reject codec difference",
			request: &WebhookCheckRequest{
				InstanceIDs: []int{instance.ID},
				TorrentName: "Starbound.Route.2025.1080p.WEB-DL.H.265-LUMA",
				Size:        2_000_500,
			},
			source: qbt.Torrent{
				Hash: "codec-nonexact", Name: "Starbound.Route.2025.1080p.WEB-DL.H.264-LUMA",
				Size: 1_000_000, TotalSize: 2_000_000, Progress: 1,
			},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
				syncManager:      newFakeSyncManager(instance, []qbt.Torrent{tt.source}, nil),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{}, nil
				},
			}

			response, err := service.CheckWebhook(context.Background(), tt.request)
			require.NoError(t, err)
			require.Equal(t, tt.wantMatch, response.CanCrossSeed)
			if !tt.wantMatch {
				require.Empty(t, response.Matches)
				return
			}
			require.Len(t, response.Matches, 1)
			require.Equal(t, tt.wantMatchType, response.Matches[0].MatchType)
		})
	}
}

// TestCheckWebhookUnknownSizePreflight catches an unknown-size check that
// either rejects allowed descriptive drift or permits hard identity drift.
func TestCheckWebhookUnknownSizePreflight(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 12, Name: "primary", IsActive: true}
	tests := []struct {
		name         string
		announcement string
		sourceName   string
		skipRecheck  bool
		wantMatch    bool
	}{
		{
			name: "strict metadata", announcement: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: true,
		},
		{
			name: "codec difference", announcement: "Silver.Harbor.S01E05.1080p.WEB-DL.H.265-LUMA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: true,
		},
		{
			name: "source difference", announcement: "Silver.Harbor.S01E05.1080p.BluRay.H.264-LUMA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: true,
		},
		{
			name: "title difference", announcement: "Northern.Orbit.S01E05.1080p.WEB-DL.H.264-LUMA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: false,
		},
		{
			name: "season difference", announcement: "Silver.Harbor.S02E05.1080p.WEB-DL.H.264-LUMA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: false,
		},
		{
			name: "episode difference", announcement: "Silver.Harbor.S01E06.1080p.WEB-DL.H.264-LUMA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: false,
		},
		{
			name: "explicit group conflict", announcement: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-NOVA",
			sourceName: "Silver.Harbor.S01E05.1080p.WEB-DL.H.264-LUMA", wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
					Hash: "unknown-" + tt.name, Name: tt.sourceName, Size: 2_000_000, TotalSize: 2_000_000, Progress: 1,
				}}, nil),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{SkipRecheck: tt.skipRecheck}, nil
				},
				crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
					t.Fatal("webhook check must not invoke CrossSeed")
					return nil, nil
				},
			}

			response, err := service.CheckWebhook(context.Background(), &WebhookCheckRequest{
				InstanceIDs: []int{instance.ID}, TorrentName: tt.announcement,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantMatch, response.CanCrossSeed)
			if tt.wantMatch {
				require.Len(t, response.Matches, 1)
				require.Equal(t, "metadata", response.Matches[0].MatchType)
			} else {
				require.Empty(t, response.Matches)
			}
		})
	}
}

// TestCheckWebhookAnnouncementCheapGate catches positive nonexact checks that
// fetch selected files even though only raw strict/tolerance matching can
// survive. Exact byte evidence remains the sole path that earns file-aware
// relaxed matching.
func TestCheckWebhookAnnouncementCheapGate(t *testing.T) {
	t.Parallel()

	const sourceSize = int64(2_000_000)
	instance := &models.Instance{ID: 13, Name: "primary", IsActive: true}
	tests := []struct {
		name          string
		announcement  string
		size          uint64
		wantMatch     bool
		wantMatchType string
		wantLookups   int
	}{
		{
			name:         "unknown strict avoids file lookup",
			announcement: "Quiet.Signal.2025.1080p.WEB-DL.H.264-LUMA",
			wantMatch:    true, wantMatchType: "metadata", wantLookups: 0,
		},
		{
			name:         "nonexact codec avoids file lookup",
			announcement: "Quiet.Signal.2025.1080p.WEB-DL.H.265-LUMA",
			size:         uint64(sourceSize + 500),
			wantLookups:  0,
		},
		{
			name:         "nonexact strict tolerance avoids file lookup",
			announcement: "Quiet.Signal.2025.1080p.WEB-DL.H.264-LUMA",
			size:         uint64(sourceSize + 500),
			wantMatch:    true, wantMatchType: "size", wantLookups: 0,
		},
		{
			name:         "exact codec uses file lookup",
			announcement: "Quiet.Signal.2025.1080p.WEB-DL.H.265-LUMA",
			size:         uint64(sourceSize),
			wantMatch:    true, wantMatchType: "exact", wantLookups: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := qbt.Torrent{
				Hash: "cheap-gate", Name: "Quiet.Signal.2025.1080p.WEB-DL.H.264-LUMA",
				Size: sourceSize - 10_000, TotalSize: sourceSize, Progress: 1,
			}
			sync := &announcementLookupCountingSyncManager{fakeSyncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
				source.Hash: {{Name: source.Name + ".mkv", Size: sourceSize}},
			})}
			service := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{}, nil
				},
			}

			response, err := service.CheckWebhook(context.Background(), &WebhookCheckRequest{
				InstanceIDs: []int{instance.ID}, TorrentName: tt.announcement, Size: tt.size,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantMatch, response.CanCrossSeed)
			require.Equal(t, tt.wantLookups, sync.lookups)
			if tt.wantMatch {
				require.Len(t, response.Matches, 1)
				require.Equal(t, tt.wantMatchType, response.Matches[0].MatchType)
			} else {
				require.Empty(t, response.Matches)
			}
		})
	}
}
