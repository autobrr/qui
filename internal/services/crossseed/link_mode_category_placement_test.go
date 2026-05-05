// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestResolveLinkModeCategoryPlacement(t *testing.T) {
	t.Parallel()

	filesWithCommonRoot := []hardlinktree.TorrentFile{
		{Path: "Movie/video.mkv", Size: 1000},
	}
	filesRootless := []hardlinktree.TorrentFile{
		{Path: "video.mkv", Size: 1000},
	}

	tests := []struct {
		name                     string
		candidateFiles           []hardlinktree.TorrentFile
		categorySavePath         string
		crossCategoryExistsInQbt bool
		wantSavePath             string
		wantDestDir              string
		wantAutoTMM              bool
	}{
		{
			name:                     "existing tracker category uses configured save path directly",
			candidateFiles:           filesRootless,
			categorySavePath:         "/qbt/Aither",
			crossCategoryExistsInQbt: true,
			wantSavePath:             "/qbt/Aither",
			wantDestDir:              "/qbt/Aither",
			wantAutoTMM:              true,
		},
		{
			name:                     "new tracker category derives preset save path",
			candidateFiles:           filesWithCommonRoot,
			categorySavePath:         "",
			crossCategoryExistsInQbt: false,
			wantSavePath:             "/reflinks/Aither",
			wantDestDir:              "/reflinks/Aither",
			wantAutoTMM:              true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				trackerCustomizationStore: &mockTrackerCustomizationStore{
					customizations: []*models.TrackerCustomization{
						{DisplayName: "Aither", Domains: []string{"aither.cc"}},
					},
				},
			}

			placement := service.resolveLinkModeCategoryPlacement(
				context.Background(),
				&models.Instance{HardlinkDirPreset: "by-tracker"},
				"/reflinks",
				"abcdef1234567890",
				"Movie.2024",
				CrossSeedCandidate{InstanceName: "qbt1"},
				"aither.cc",
				&CrossSeedRequest{},
				tt.candidateFiles,
				"Aither.cross",
				tt.categorySavePath,
				true,
				tt.crossCategoryExistsInQbt,
			)

			require.Equal(t, tt.wantSavePath, placement.EffectiveCategorySavePath)
			require.Equal(t, tt.wantDestDir, placement.DestDir)
			require.Equal(t, tt.wantAutoTMM, placement.EnableAutoTMM)
		})
	}
}

// TestResolveLinkModeCategoryPlacement_NonTrackerCategoryMode covers the regression where
// categorySavePath (from the matched torrent's existing qBittorrent category) was
// unconditionally used as effectiveCategorySavePath, bypassing buildHardlinkDestDir and
// breaking by-tracker subfolder placement for users not using the tracker category feature.
func TestResolveLinkModeCategoryPlacement_NonTrackerCategoryMode(t *testing.T) {
	t.Parallel()

	filesWithCommonRoot := []hardlinktree.TorrentFile{
		{Path: "Movie/video.mkv", Size: 1000},
	}
	filesRootless := []hardlinktree.TorrentFile{
		{Path: "video.mkv", Size: 1000},
	}

	service := &Service{
		trackerCustomizationStore: &mockTrackerCustomizationStore{
			customizations: []*models.TrackerCustomization{
				{DisplayName: "Aither", Domains: []string{"aither.cc"}},
			},
		},
	}

	tests := []struct {
		name                     string
		candidateFiles           []hardlinktree.TorrentFile
		categorySavePath         string // non-empty: simulates matched torrent's category save path
		crossCategoryExistsInQbt bool
		wantSavePath             string // must be empty — feature is not active
		wantDestDirContains      string // tracker subfolder must appear
		wantDestDirExcludes      string // category save path must NOT appear
		wantAutoTMM              bool
	}{
		{
			name:                     "by-tracker without mapping ignores existing category save path — multi-file",
			candidateFiles:           filesWithCommonRoot,
			categorySavePath:         "/qbt/movies",
			crossCategoryExistsInQbt: true,
			wantSavePath:             "",
			wantDestDirContains:      "Aither",
			wantDestDirExcludes:      "/qbt/movies",
			wantAutoTMM:              false,
		},
		{
			name:                     "by-tracker without mapping ignores existing category save path — single-file",
			candidateFiles:           filesRootless,
			categorySavePath:         "/qbt/movies",
			crossCategoryExistsInQbt: true,
			wantSavePath:             "",
			wantDestDirContains:      "Aither",
			wantDestDirExcludes:      "/qbt/movies",
			wantAutoTMM:              false,
		},
		{
			name:                     "by-tracker without mapping and no category save path — uses tracker subfolder",
			candidateFiles:           filesWithCommonRoot,
			categorySavePath:         "",
			crossCategoryExistsInQbt: false,
			wantSavePath:             "",
			wantDestDirContains:      "Aither",
			wantAutoTMM:              false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			placement := service.resolveLinkModeCategoryPlacement(
				context.Background(),
				&models.Instance{HardlinkDirPreset: "by-tracker"},
				"/hardlinks",
				"abcdef1234567890",
				"Movie.2024",
				CrossSeedCandidate{InstanceName: "qbt1"},
				"aither.cc",
				&CrossSeedRequest{},
				tt.candidateFiles,
				"movies.cross",
				tt.categorySavePath,
				false, // isTrackerCategoryMode = false: no mapping configured
				tt.crossCategoryExistsInQbt,
			)

			require.Equal(t, tt.wantSavePath, placement.EffectiveCategorySavePath)
			require.Contains(t, placement.DestDir, tt.wantDestDirContains)
			if tt.wantDestDirExcludes != "" {
				require.NotContains(t, placement.DestDir, tt.wantDestDirExcludes)
			}
			require.Equal(t, tt.wantAutoTMM, placement.EnableAutoTMM)
		})
	}
}
