// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/pathutil"
)

func TestApplySeasonPack_TakenFolderNameGivesTheHashedFolder(t *testing.T) {
	fix := newSeasonPackFixture(t)
	baseDir := t.TempDir()
	sourceDir := t.TempDir()
	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true, HasLocalFilesystemAccess: true, UseHardlinks: true,
		HardlinkBaseDir: baseDir, HardlinkDirPreset: "flat",
	}

	hashes := []string{"e01", "e02", "e03", "e04"}
	torrents := make([]qbt.Torrent, 0, len(fix.packFiles))
	for i, fileName := range fix.packFiles {
		torrents = append(torrents, qbt.Torrent{
			Hash: hashes[i], Name: strings.TrimSuffix(fileName, filepath.Ext(fileName)),
			ContentPath: filepath.Join(sourceDir, fileName), Progress: 1.0,
		})
	}
	writeEpisodeSources(t, torrents)

	takenFolder := filepath.Join(baseDir, fix.packName)
	otherFile := filepath.Join(takenFolder, fix.packFiles[0])
	require.NoError(t, os.MkdirAll(takenFolder, 0o755))
	require.NoError(t, os.WriteFile(otherFile, []byte("another torrent"), 0o600))

	torrentBytes, err := base64.StdEncoding.DecodeString(fix.torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	hashedFolder := filepath.Join(baseDir, pathutil.HashedIsolationFolder(meta.HashV1.String(), fix.packName))

	apply := func() *adderFake {
		t.Helper()
		baseSM := newMultiFakeSyncManager(map[int][]qbt.Torrent{inst.ID: torrents}, map[int]*models.Instance{inst.ID: inst})
		baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, hashes...)
		svc := &Service{
			instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
			syncManager:              &seasonPackSyncManager{fakeSyncManager: baseSM},
			releaseCache:             NewReleaseCache(),
			automationSettingsLoader: defaultSettings(true, 1.0),
			seasonPackRunStore:       &stubSeasonPackRunStore{},
		}
		svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
		adds := withLinkAdder(svc)
		result, err := svc.applySeasonPack(t.Context(), seasonPackRequest{
			name: fix.packName, torrentData: fix.torrentData, instanceIDs: []int{inst.ID},
		})
		require.NoError(t, err)
		require.True(t, result.applied, "%+v", result)
		require.Len(t, adds.added, 1)
		return adds
	}
	requireTree := func() {
		t.Helper()
		require.ElementsMatch(t, fix.packFiles, linkedPackFiles(t, filepath.Join(hashedFolder, fix.packName), torrentContentPaths(torrents)...))
		for _, name := range fix.packFiles {
			info, err := local.NewBackend().Lstat(t.Context(), filepath.Join(hashedFolder, fix.packName, name))
			require.NoError(t, err)
			require.Equal(t, uint64(2), info.Nlinks, "%s has one link besides its source", name)
		}
		entries, err := os.ReadDir(takenFolder)
		require.NoError(t, err)
		require.Len(t, entries, 1, "the existing folder does not change")
		data, err := os.ReadFile(otherFile)
		require.NoError(t, err)
		require.Equal(t, "another torrent", string(data))
	}

	first := apply()
	require.Equal(t, hashedFolder, first.added[0]["savepath"])
	requireTree()

	second := apply()
	require.Equal(t, hashedFolder, second.added[0]["savepath"], "the same pack again reuses its hashed folder")
	requireTree()
}
