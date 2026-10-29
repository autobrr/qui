// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

func TestBuildTrackedFileUpsert_SearchedIndexerIDs(t *testing.T) {
	t.Parallel()

	l := zerolog.New(io.Discard)
	modTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	existing := &models.DirScanFile{
		FilePath:           "/data/movies/movie.mkv",
		FileSize:           100,
		FileModTime:        modTime,
		Status:             models.DirScanFileStatusNoMatch,
		SearchedIndexerIDs: []int{1, 2},
	}
	idx := &trackedFilesIndex{
		byPath:   map[string]*models.DirScanFile{existing.FilePath: existing},
		byFileID: map[fsops.FileKey]*models.DirScanFile{},
	}

	t.Run("unchanged file carries the search set forward", func(t *testing.T) {
		scanned := &ScannedFile{Path: existing.FilePath, Size: 100, ModTime: modTime}
		fileModel, err := buildTrackedFileUpsert(1, scanned, idx, &seedingIndex{}, &l)
		require.NoError(t, err)
		require.NotNil(t, fileModel)
		require.Equal(t, models.DirScanFileStatusNoMatch, fileModel.Status)
		require.Equal(t, []int{1, 2}, fileModel.SearchedIndexerIDs)
	})

	t.Run("changed file resets to pending and clears the search set", func(t *testing.T) {
		scanned := &ScannedFile{Path: existing.FilePath, Size: 200, ModTime: modTime}
		fileModel, err := buildTrackedFileUpsert(1, scanned, idx, &seedingIndex{}, &l)
		require.NoError(t, err)
		require.NotNil(t, fileModel)
		require.Equal(t, models.DirScanFileStatusPending, fileModel.Status)
		require.Nil(t, fileModel.SearchedIndexerIDs)
	})
}

// A tracked row keeps the tagged bytes; a rescan finds it by identity after a rename.
func TestLookupTrackedFile_MatchesStoredTaggedFileID(t *testing.T) {
	t.Parallel()

	id := hardlink.UnixFileID(7, 42)
	tracked := &models.DirScanFile{FilePath: "/data/old.mkv", FileID: id.Bytes()}
	idx := &trackedFilesIndex{
		byPath:   map[string]*models.DirScanFile{tracked.FilePath: tracked},
		byFileID: map[fsops.FileKey]*models.DirScanFile{},
	}
	key, ok := idx.keyOfStored(tracked.FileID)
	require.True(t, ok)
	idx.byFileID[key] = tracked

	existing, matchedBy := lookupTrackedFile(&ScannedFile{Path: "/data/new.mkv", FileID: id}, idx)
	require.Same(t, tracked, existing)
	require.Equal(t, "file_id", matchedBy)

	_, ok = idx.keyOfStored(make([]byte, 16))
	require.False(t, ok, "a pre-tag width decodes to no identity and is never indexed")
}

// stubTorrentFiles is a qBittorrent that answers the seeding index build
// with err, or with no torrents when err is nil.
type stubTorrentFiles struct{ err error }

func (f stubTorrentFiles) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, f.err
}

func (f stubTorrentFiles) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return nil, f.err
}

// A Full scan clears already_seeding only when the seeding index built in full.
//
// regression: #3605
func TestFullScanResetsAlreadySeedingOnlyWhenTheSeedingIndexBuilt(t *testing.T) {
	for _, tt := range []struct {
		name     string
		indexErr error
		want     models.DirScanFileStatus
	}{
		{name: "qBittorrent does not answer", indexErr: errors.New("qBittorrent did not answer"), want: models.DirScanFileStatusAlreadySeeding},
		{name: "torrent removed", want: models.DirScanFileStatusPending},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			movie := filepath.Join(dir.Path, "Synthetic.Film.2024.1080p.WEB.x264-GRP", "film.mkv")
			require.NoError(t, os.MkdirAll(filepath.Dir(movie), 0o750))
			require.NoError(t, os.WriteFile(movie, []byte("synthetic media"), 0o600))
			info, err := local.NewBackend().Stat(ctx, movie)
			require.NoError(t, err)
			require.NoError(t, store.UpsertFile(ctx, &models.DirScanFile{
				DirectoryID: dir.ID,
				FilePath:    movie,
				FileSize:    info.Size,
				FileModTime: info.ModTime,
				FileID:      info.FileID.Bytes(),
				Status:      models.DirScanFileStatusAlreadySeeding,
			}))
			runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
			require.NoError(t, err)
			svc.backendPool = testBackendPool(&models.Instance{ID: dir.TargetInstanceID, HasLocalFilesystemAccess: true})
			svc.torrentFiles = stubTorrentFiles{err: tt.indexErr}
			l := zerolog.Nop()

			scanResult, index, ok := svc.runScanPhase(ctx, dir, dir.Path, runID, &l)
			require.True(t, ok)
			_, err = svc.refreshTrackedFilesFromScan(ctx, dir.ID, scanResult, index, &l)
			require.NoError(t, err)

			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, tt.want, files[0].Status)
		})
	}
}

// A file whose identity the run did not read keeps already_seeding, even
// when the seeding index built in full.
//
// regression: #3605
func TestRefreshTrackedFilesKeepsAlreadySeedingWithoutIdentity(t *testing.T) {
	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)
	movie := filepath.Join(dir.Path, "film.mkv")
	modTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.UpsertFile(ctx, &models.DirScanFile{
		DirectoryID: dir.ID,
		FilePath:    movie,
		FileSize:    100,
		FileModTime: modTime,
		Status:      models.DirScanFileStatusAlreadySeeding,
	}))
	scanResult := &ScanResult{Searchees: []*Searchee{{
		Name:  "film",
		Path:  movie,
		Files: []*ScannedFile{{Path: movie, RelPath: "film.mkv", Size: 100, ModTime: modTime}},
	}}}
	l := zerolog.Nop()

	_, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, scanResult, &seedingIndex{complete: true}, &l)
	require.NoError(t, err)

	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, models.DirScanFileStatusAlreadySeeding, files[0].Status)
}
