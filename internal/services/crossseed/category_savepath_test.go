// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildCategorySavePath(t *testing.T) {
	tests := []struct {
		name                  string
		preset                string
		baseDir               string
		incomingTrackerDomain string
		indexerName           string
		instanceName          string
		customizations        []*models.TrackerCustomization
		expected              string
	}{
		{
			name:                  "by-tracker with indexer name",
			preset:                "by-tracker",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "FearNoPeer",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed/FearNoPeer",
		},
		{
			name:                  "by-tracker with tracker customization",
			preset:                "by-tracker",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.lst.example.com",
			indexerName:           "LST-Fallback",
			instanceName:          "Truenas",
			customizations: []*models.TrackerCustomization{
				{DisplayName: "LST", Domains: []string{"tracker.lst.example.com"}},
			},
			expected: "/data/cross-seed/LST",
		},
		{
			name:                  "by-tracker with special characters in name",
			preset:                "by-tracker",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.oe.example.com",
			indexerName:           "OnlyEncodes+ (API)",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed/OnlyEncodes+ (API)",
		},
		{
			name:                  "by-tracker with no tracker info falls back to Unknown",
			preset:                "by-tracker",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "",
			indexerName:           "",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed/Unknown",
		},
		{
			name:                  "by-instance uses instance name",
			preset:                "by-instance",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "FearNoPeer",
			instanceName:          "Seedhost-40gb",
			expected:              "/data/cross-seed/Seedhost-40gb",
		},
		{
			name:                  "flat returns base dir only",
			preset:                "flat",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "FearNoPeer",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed",
		},
		{
			name:                  "unknown preset treated as flat",
			preset:                "something-else",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "FearNoPeer",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed",
		},
		{
			name:                  "empty preset treated as flat",
			preset:                "",
			baseDir:               "/data/cross-seed",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "FearNoPeer",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed",
		},
		{
			name:                  "by-tracker with trailing slash on baseDir",
			preset:                "by-tracker",
			baseDir:               "/data/cross-seed/",
			incomingTrackerDomain: "tracker.example.com",
			indexerName:           "LST",
			instanceName:          "Truenas",
			expected:              "/data/cross-seed/LST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &mockTrackerCustomizationStore{
				customizations: tt.customizations,
			}
			if tt.customizations == nil {
				mockStore.customizations = []*models.TrackerCustomization{}
			}

			svc := &Service{
				trackerCustomizationStore: mockStore,
			}

			instance := &models.Instance{
				HardlinkDirPreset: tt.preset,
			}

			candidate := CrossSeedCandidate{
				InstanceName: tt.instanceName,
			}

			req := &CrossSeedRequest{
				IndexerName: tt.indexerName,
			}

			result := svc.buildCategorySavePath(context.Background(), instance, tt.baseDir, tt.incomingTrackerDomain, candidate, req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildCategorySavePathDoesNotIncludeIsolationFolder(t *testing.T) {
	// Verify that buildCategorySavePath never includes torrent-specific isolation folders.
	// This is the core bug fix — category save paths should be stable directory-level paths
	// like /data/cross-seed/TrackerName, not torrent-specific paths like
	// /data/cross-seed/TrackerName/Movie.Name--abc123
	mockStore := &mockTrackerCustomizationStore{
		customizations: []*models.TrackerCustomization{},
	}

	svc := &Service{
		trackerCustomizationStore: mockStore,
	}

	instance := &models.Instance{
		HardlinkDirPreset: "by-tracker",
	}

	candidate := CrossSeedCandidate{
		InstanceName: "Truenas",
	}

	req := &CrossSeedRequest{
		IndexerName: "TorrentLeech",
	}

	result := svc.buildCategorySavePath(context.Background(), instance, "/data/cross-seed", "tracker.tl.example.com", candidate, req)

	// Should NOT contain any hash-like suffix or torrent name
	assert.NotContains(t, result, "--")
	assert.NotContains(t, result, ".mkv")
	assert.NotContains(t, result, ".mp4")
	// Should be exactly the tracker-level directory
	assert.Equal(t, "/data/cross-seed/TorrentLeech", result)
}
