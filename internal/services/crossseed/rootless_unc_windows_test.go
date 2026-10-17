// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestProcessCrossSeedCandidate_WindowsUNCSavePath(t *testing.T) {
	for _, tc := range []struct {
		name        string
		savePath    string
		contentPath string
		files       qbt.TorrentFiles
		want        string
	}{
		{name: "POSIX control", savePath: "/downloads/tv", contentPath: "/downloads/tv/Harbor", files: qbt.TorrentFiles{{Name: "video.mkv", Size: 1024}, {Name: "extra.nfo", Size: 128}}, want: "/downloads/tv/Harbor"},
		{name: "drive control", savePath: `C:\Media`, contentPath: `C:\Media\Harbor`, files: qbt.TorrentFiles{{Name: "video.mkv", Size: 1024}, {Name: "extra.nfo", Size: 128}}, want: "C:/Media/Harbor"},
		{name: "UNC directory", savePath: `\\nas\media`, contentPath: `\\nas\media\Harbor`, files: qbt.TorrentFiles{{Name: "video.mkv", Size: 1024}, {Name: "extra.nfo", Size: 128}}, want: "//nas/media/Harbor"},
		{name: "UNC forward slashes", savePath: "//nas/media", contentPath: "//nas/media/Harbor", files: qbt.TorrentFiles{{Name: "video.mkv", Size: 1024}, {Name: "extra.nfo", Size: 128}}, want: "//nas/media/Harbor"},
		{name: "UNC single file", savePath: `\\nas\media`, contentPath: `\\nas\media\Harbor\video.mkv`, files: qbt.TorrentFiles{{Name: "video.mkv", Size: 1024}}, want: "//nas/media/Harbor"},
		{name: "UNC share root file", savePath: `\\nas\media`, contentPath: `\\nas\media\video.mkv`, files: qbt.TorrentFiles{{Name: "video.mkv", Size: 1024}}, want: "//nas/media"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sync := &rootlessSavePathSyncManager{
				files: map[string]qbt.TorrentFiles{"matchedhash": tc.files, "newhash": tc.files},
				props: map[string]*qbt.TorrentProperties{"matchedhash": {SavePath: tc.savePath}},
			}
			service := &Service{
				syncManager:      sync,
				instanceStore:    &rootlessSavePathInstanceStore{instances: map[int]*models.Instance{1: {ID: 1}}},
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return models.DefaultCrossSeedAutomationSettings(), nil
				},
			}
			name := "Harbor.S01E01.1080p.WEB-DL-DETECT"
			candidate := CrossSeedCandidate{
				InstanceID:   1,
				InstanceName: "synthetic",
				Torrents: []qbt.Torrent{{
					Hash: "matchedhash", Name: name, Progress: 1, AutoManaged: true,
					ContentPath: tc.contentPath,
				}},
			}
			result := service.processCrossSeedCandidate(t.Context(), candidate, []byte("torrent"), "newhash", "", name,
				&CrossSeedRequest{StartPaused: new(true)}, service.releaseCache.Parse(name), tc.files, nil)
			require.True(t, result.Success, "candidate must reach AddTorrent: %+v", result)
			require.NotNil(t, sync.addedOptions)
			assert.Equal(t, tc.want, filepath.ToSlash(sync.addedOptions["savepath"]), "operational destination must preserve its root")
		})
	}
}
