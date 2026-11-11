// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
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

// regression: #3736
func TestBuildTrackedFileUpsert_ComparesModTimeInWholeSeconds(t *testing.T) {
	t.Parallel()

	second := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	nanos := second.Add(123456789 * time.Nanosecond)

	for _, tt := range []struct {
		name        string
		storedTime  time.Time
		scannedTime time.Time
		scannedSize int64
		wantStatus  models.DirScanFileStatus
		wantIndexer []int
	}{
		{name: "stored whole second, scanned nanos", storedTime: second, scannedTime: nanos, scannedSize: 100, wantStatus: models.DirScanFileStatusNoMatch, wantIndexer: []int{1}},
		{name: "stored nanos, scanned whole second", storedTime: nanos, scannedTime: second, scannedSize: 100, wantStatus: models.DirScanFileStatusNoMatch, wantIndexer: []int{1}},
		{name: "different second", storedTime: nanos, scannedTime: nanos.Add(time.Second), scannedSize: 100, wantStatus: models.DirScanFileStatusPending},
		{name: "different size, same second", storedTime: nanos, scannedTime: nanos, scannedSize: 200, wantStatus: models.DirScanFileStatusPending},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			existing := &models.DirScanFile{
				FilePath:           "/data/movies/movie.mkv",
				FileSize:           100,
				FileModTime:        tt.storedTime,
				Status:             models.DirScanFileStatusNoMatch,
				SearchedIndexerIDs: []int{1},
			}
			idx := &trackedFilesIndex{
				byPath:   map[string]*models.DirScanFile{existing.FilePath: existing},
				byFileID: map[fsops.FileKey]*models.DirScanFile{},
			}
			scanned := &ScannedFile{Path: existing.FilePath, Size: tt.scannedSize, ModTime: tt.scannedTime}

			fileModel, err := buildTrackedFileUpsert(1, scanned, idx, &seedingIndex{}, nil)
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, fileModel.Status)
			require.Equal(t, tt.wantIndexer, fileModel.SearchedIndexerIDs)
		})
	}
}

// regression: #3736
func TestRefreshTrackedFilesKeepsFinalStatusAcrossScansSQLite(t *testing.T) {
	t.Parallel()
	runTrackedFileRescanTest(t, testdb.NewMigratedSQLite(t, "dirscan-rescan"))
}

// regression: #3736
func TestRefreshTrackedFilesKeepsFinalStatusAcrossScansPostgresIntegration(t *testing.T) {
	runTrackedFileRescanTest(t, testdb.NewMigratedPostgres(t, "dirscan-rescan"))
}

// runTrackedFileRescanTest stores an mtime through the real store and scans it
// again. Postgres reads it back in microseconds. The nanos sit at the top of the
// second so a driver that rounded instead of truncating would fail here too.
func runTrackedFileRescanTest(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewDirScanStore(db)
	svc := &Service{store: store}

	for _, tt := range []struct {
		name   string
		zone   *time.Location
		fileID hardlink.FileID
	}{
		{name: "utc", zone: time.UTC, fileID: hardlink.UnixFileID(7, 42)},
		{name: "west-of-utc", zone: time.FixedZone("PST", -8*3600), fileID: hardlink.UnixFileID(7, 42)},
		// Without a file ID only the INSERT writes the row.
		{name: "west-of-utc-no-file-id", zone: time.FixedZone("PST", -8*3600)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
				Path:                "/data/" + tt.name,
				Enabled:             true,
				TargetInstanceID:    instance.ID,
				ScanIntervalMinutes: 60,
			})
			require.NoError(t, err)

			scanned := &ScannedFile{
				Path:    "/data/" + tt.name + "/movie.mkv",
				RelPath: "movie.mkv",
				Size:    100,
				ModTime: time.Date(2026, time.July, 1, 12, 0, 0, 999999999, tt.zone),
				FileID:  tt.fileID,
			}
			root := &Searchee{Name: "movie", Path: scanned.Path, Files: []*ScannedFile{scanned}}
			item := searcheeWorkItem{searchee: root}
			result := &ScanResult{Searchees: []*Searchee{root}}
			enabledIndexers := map[int]struct{}{1: {}}

			tracked, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, nil, result, &seedingIndex{}, nil)
			require.NoError(t, err)
			require.NoError(t, svc.upsertDirScanFileStatus(ctx, dir.ID, scanned, tracked, dirScanFileUpdate{
				status:             models.DirScanFileStatusNoMatch,
				searchedIndexerIDs: []int{1},
			}))

			rescan := func(t *testing.T) (*models.DirScanFile, bool) {
				t.Helper()
				idx, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, nil, result, &seedingIndex{}, nil)
				require.NoError(t, err)
				files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
				require.NoError(t, err)
				require.Len(t, files, 1)
				return files[0], workItemHasPendingFiles(item, idx, enabledIndexers)
			}

			file, pending := rescan(t)
			require.Equal(t, models.DirScanFileStatusNoMatch, file.Status)
			require.Equal(t, []int{1}, file.SearchedIndexerIDs)
			require.False(t, pending)

			scanned.ModTime = scanned.ModTime.Add(time.Second)
			file, pending = rescan(t)
			require.Equal(t, models.DirScanFileStatusPending, file.Status)
			require.Nil(t, file.SearchedIndexerIDs)
			require.True(t, pending)
		})
	}
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

// stubTorrentFiles is a qBittorrent that seeds one other completed torrent,
// or none when empty is set. It answers the torrent list with err, and the
// file lists with files. unsynced is an instance before its first sync.
type stubTorrentFiles struct {
	err      error
	files    map[string]qbt.TorrentFiles
	empty    bool
	unsynced bool
}

func (f stubTorrentFiles) SeedingReady(context.Context, int) error {
	if f.unsynced {
		return errors.New("instance not ready: waiting for first sync")
	}
	return nil
}

func (f stubTorrentFiles) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	if f.empty {
		return nil, f.err
	}
	other := &qbt.Torrent{Hash: "bbbb", Progress: 1, SavePath: "/elsewhere"}
	return []qbittorrent.CrossInstanceTorrentView{{TorrentView: &qbittorrent.TorrentView{Torrent: other}}}, f.err
}

func (f stubTorrentFiles) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return f.files, nil
}

// statBackend answers every Stat with info or err, for the files of the
// seeding index.
type statBackend struct {
	fsops.Backend
	info *fsops.LstatInfo
	err  error
}

func (b statBackend) Stat(context.Context, string) (*fsops.LstatInfo, error) {
	return b.info, b.err
}

// A torrent file the index could not read, or read without an identity,
// leaves the index incomplete. So does a missing file that the torrent wants,
// since a completed torrent has it on disk. A missing skipped file does not,
// since qBittorrent may not have written it.
//
// regression: #3613
func TestBuildFileIDIndexCompleteness(t *testing.T) {
	notExist := &fs.PathError{Op: "stat", Err: fs.ErrNotExist}
	onDisk := &fsops.LstatInfo{Size: 1, FileID: hardlink.UnixFileID(7, 42)}
	for _, tt := range []struct {
		name      string
		noList    bool
		emptyList bool
		priority  int
		info      *fsops.LstatInfo
		err       error
		want      bool
		indexed   int
		reason    string
	}{
		{name: "file on disk", priority: 1, info: onDisk, want: true, indexed: 1},
		{name: "missing skipped file", err: notExist, want: true},
		{name: "parent of a skipped file is a file", err: &fs.PathError{Op: "stat", Err: syscall.ENOTDIR}, want: true},
		{name: "missing wanted file", priority: 1, err: notExist, want: false, reason: "torrent bbbb file other.mkv"},
		{name: "unreadable file", err: &fs.PathError{Op: "stat", Err: fs.ErrPermission}, want: false, reason: "torrent bbbb file other.mkv"},
		{name: "no identity", info: &fsops.LstatInfo{Size: 1}, want: false, reason: "torrent bbbb file other.mkv"},
		{name: "no file list", noList: true, want: false, reason: "no file list for torrent bbbb"},
		// regression: #3613
		{name: "empty file list", emptyList: true, want: false, reason: "empty file list for torrent bbbb"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lists := map[string]qbt.TorrentFiles{"bbbb": {{Name: "other.mkv", Priority: tt.priority}}}
			if tt.noList {
				lists = map[string]qbt.TorrentFiles{}
			}
			if tt.emptyList {
				lists = map[string]qbt.TorrentFiles{"bbbb": {}}
			}
			svc := &Service{torrentFiles: stubTorrentFiles{files: lists}}
			backend := statBackend{Backend: local.NewBackend(), info: tt.info, err: tt.err}

			index, err := svc.buildFileIDIndex(t.Context(), &models.Instance{ID: 1, HasLocalFilesystemAccess: true}, backend, nil)
			require.NoError(t, err)
			require.Equal(t, tt.want, index.complete)
			require.Equal(t, tt.indexed, index.len())
			require.Contains(t, index.incompleteReason, tt.reason)
		})
	}
}

// A Full scan clears already_seeding only when the seeding index built in full.
//
// regression: #3605
func TestFullScanResetsAlreadySeedingOnlyWhenTheSeedingIndexBuilt(t *testing.T) {
	for _, tt := range []struct {
		name string
		qbt  stubTorrentFiles
		want models.DirScanFileStatus
	}{
		{
			name: "qBittorrent does not answer",
			qbt:  stubTorrentFiles{err: errors.New("qBittorrent did not answer")},
			want: models.DirScanFileStatusAlreadySeeding,
		},
		{
			name: "a file list is missing",
			qbt:  stubTorrentFiles{files: map[string]qbt.TorrentFiles{}},
			want: models.DirScanFileStatusAlreadySeeding,
		},
		{
			name: "torrent removed",
			qbt:  stubTorrentFiles{files: map[string]qbt.TorrentFiles{"bbbb": {{Name: "other.mkv"}}}},
			want: models.DirScanFileStatusPending,
		},
		// regression: #3613
		{
			name: "first sync not done",
			qbt:  stubTorrentFiles{empty: true, unsynced: true},
			want: models.DirScanFileStatusAlreadySeeding,
		},
		{
			name: "instance seeds nothing",
			qbt:  stubTorrentFiles{empty: true},
			want: models.DirScanFileStatusPending,
		},
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
			svc.torrentFiles = tt.qbt
			l := zerolog.Nop()

			scanResult, index, ok := svc.runScanPhase(ctx, dir, localScanTarget(), dir.Path, runID, &l)
			require.True(t, ok)
			_, err = svc.refreshTrackedFilesFromScan(ctx, dir.ID, localScanTarget().instance, scanResult, index, &l)
			require.NoError(t, err)

			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, tt.want, files[0].Status)
		})
	}
}

// A file already seeding keeps that status until a complete index, checked
// against the file's identity, shows the torrent is gone. That holds when the
// file changed on disk too.
//
// regression: #3605
func TestRefreshTrackedFilesKeepsAlreadySeedingWithoutProof(t *testing.T) {
	id := hardlink.UnixFileID(7, 42)
	key, ok := fsops.FileKeyOf(id, nil)
	require.True(t, ok)
	seeding := &seedingIndex{byKey: map[fsops.FileKey]string{key: "aaaa"}, complete: true}

	for _, tt := range []struct {
		name    string
		id      hardlink.FileID
		size    int64
		seeding *seedingIndex
		want    models.DirScanFileStatus
	}{
		{name: "identity not read", size: 100, seeding: &seedingIndex{complete: true}, want: models.DirScanFileStatusAlreadySeeding},
		// regression: #3613
		{name: "changed, index incomplete", id: id, size: 200, seeding: &seedingIndex{}, want: models.DirScanFileStatusAlreadySeeding},
		// regression: #3613
		{name: "changed, still seeding", id: id, size: 200, seeding: seeding, want: models.DirScanFileStatusAlreadySeeding},
		{name: "changed, torrent removed", id: id, size: 200, seeding: &seedingIndex{byKey: map[fsops.FileKey]string{}, complete: true}, want: models.DirScanFileStatusPending},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
				Files: []*ScannedFile{{Path: movie, RelPath: "film.mkv", Size: tt.size, ModTime: modTime, FileID: tt.id}},
			}}}
			l := zerolog.Nop()

			_, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, localScanTarget().instance, scanResult, tt.seeding, &l)
			require.NoError(t, err)

			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, tt.want, files[0].Status)
		})
	}
}
