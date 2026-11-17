// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// fakeTransfers is the Transfer store: the helper torrents, and the folders
// of the running Transfers, of every instance.
type fakeTransfers struct {
	helpers map[string]bool
	folders []string
}

func (f fakeTransfers) HelperCheck(int) func(hash string) bool {
	if len(f.helpers) == 0 {
		return nil
	}
	return func(hash string) bool { return f.helpers[hash] }
}

func (f fakeTransfers) RunningFolders(context.Context, int) ([]string, error) { return f.folders, nil }

// seedingFiles serves completed torrents that each seed one file in one folder.
type seedingFiles map[string]string // hash -> file path

func (f seedingFiles) SeedingReady(context.Context, int) error { return nil }

func (f seedingFiles) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	views := make([]qbittorrent.CrossInstanceTorrentView, 0, len(f))
	for hash, file := range f {
		torrent := &qbt.Torrent{Hash: hash, Progress: 1, SavePath: filepath.Dir(file), Tags: "qui-transfer"}
		views = append(views, qbittorrent.CrossInstanceTorrentView{TorrentView: &qbittorrent.TorrentView{Torrent: torrent}})
	}
	return views, nil
}

func (f seedingFiles) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	files := make(map[string]qbt.TorrentFiles, len(f))
	for hash, file := range f {
		files[hash] = qbt.TorrentFiles{{Name: filepath.Base(file), Size: 15, Progress: 1, Priority: 1}}
	}
	return files, nil
}

// A file that only a helper torrent seeds is not seeding, and the walk leaves
// the folder of a running Transfer alone. A torrent of the user with the
// helper tag and no registry row seeds as any torrent does.
func TestScanSkipsHelperTorrentsAndRunningTransfers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, dir, runID := newLocalModeRun(t)
	helperFile := filepath.Join(dir.Path, "Synthetic.Film.2024.1080p.WEB.x264-GRP.mkv")
	ownFile := filepath.Join(dir.Path, "Other.Film.2022.1080p.WEB.x264-GRP.mkv")
	incoming := filepath.Join(dir.Path, "incoming")
	for _, path := range []string{helperFile, ownFile, filepath.Join(incoming, "Third.Film.2023.1080p.WEB.x264-GRP.mkv")} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("synthetic media"), 0o600))
	}
	instances := &fakeInstanceStore{instance: &models.Instance{ID: dir.TargetInstanceID, HasLocalFilesystemAccess: true}}
	svc := &Service{
		store:        store,
		backendPool:  fsops.NewPool(instances, local.NewBackend()),
		torrentFiles: seedingFiles{"ffff": helperFile, "bbbb": ownFile},
	}
	svc.SetTransfers(fakeTransfers{helpers: map[string]bool{"ffff": true}, folders: []string{incoming + string(filepath.Separator)}})
	logger := zerolog.Nop()

	validated, target, ok := svc.validateDirectory(ctx, dir.ID, runID, &logger)
	require.True(t, ok)
	result, index, ok := svc.runScanPhase(ctx, validated, target, dir.Path, runID, &logger)
	require.True(t, ok)

	seeding := map[string]bool{}
	scanner := NewScanner(target.backend)
	scanner.SetFileIDIndex(index)
	for _, searchee := range result.Searchees {
		seeding[searchee.Path], _ = scanner.CheckAlreadySeeding(searchee)
	}
	require.Equal(t, map[string]bool{helperFile: false, ownFile: true}, seeding)
}
