// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

func remoteInstance(tier models.SSHTier) *models.Instance {
	return &models.Instance{
		ID: 1, SSHHost: "box.example.invalid", SSHPort: 22,
		SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey", SSHTier: tier,
	}
}

func remoteID(ino byte) hardlink.FileID {
	raw := [16]byte{15: ino}
	return hardlink.RemoteFileID(raw[:])
}

// answerBackend is a remote host with slash paths, recorded batches and one answer per path.
type answerBackend struct {
	*recordingBackend
	answer func(p string) (*fsops.LstatInfo, error)
}

func (b answerBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	b.stated = append(b.stated, p)
	return b.answer(p)
}

func (b answerBackend) StatBatch(_ context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	b.batches = append(b.batches, paths)
	infos := make([]*fsops.LstatInfo, len(paths))
	errs := make([]error, len(paths))
	for i, p := range paths {
		infos[i], errs[i] = b.answer(p)
	}
	return infos, errs
}

func newAnswerBackend(answer func(p string) (*fsops.LstatInfo, error), walk ...fsops.WalkEntry) answerBackend {
	return answerBackend{recordingBackend: newRecordingBackend(walk...), answer: answer}
}

// seededTorrents is qBittorrent for the seeding index, by torrent hash.
type seededTorrents struct {
	savePaths map[string]string
	files     map[string]qbt.TorrentFiles
	reads     atomic.Int32
}

func (s *seededTorrents) SeedingReady(context.Context, int) error { return nil }

func (s *seededTorrents) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	s.reads.Add(1)
	views := make([]qbittorrent.CrossInstanceTorrentView, 0, len(s.savePaths))
	for hash, savePath := range s.savePaths {
		torrent := &qbt.Torrent{Hash: hash, Progress: 1, SavePath: savePath}
		views = append(views, qbittorrent.CrossInstanceTorrentView{TorrentView: &qbittorrent.TorrentView{Torrent: torrent}})
	}
	return views, nil
}

func (s *seededTorrents) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return s.files, nil
}

func onlyTorrent(savePath string, names ...string) *seededTorrents {
	files := make(qbt.TorrentFiles, 0, len(names))
	for _, name := range names {
		files = append(files, qbt.TorrentFile{Name: name, Priority: 1})
	}
	return &seededTorrents{
		savePaths: map[string]string{"aaaa": savePath},
		files:     map[string]qbt.TorrentFiles{"aaaa": files},
	}
}

// answerWithID gives every path the same identity.
func answerWithID(id hardlink.FileID) func(string) (*fsops.LstatInfo, error) {
	return func(p string) (*fsops.LstatInfo, error) {
		return &fsops.LstatInfo{Path: p, Size: 1, FileID: id, Nlinks: 1}, nil
	}
}

// remoteFileModTime is the mtime both trackRemoteFile and scanOfRemoteFile use, so a scan reads the file as unchanged.
var remoteFileModTime = time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

func trackRemoteFile(t *testing.T, store *models.DirScanStore, dirID int, filePath string, status models.DirScanFileStatus) {
	t.Helper()
	require.NoError(t, store.UpsertFile(t.Context(), &models.DirScanFile{
		DirectoryID: dirID, FilePath: filePath, FileSize: 100, FileModTime: remoteFileModTime,
		FileID: remoteID(1).Bytes(), Status: status,
	}))
}

func scanOfRemoteFile(filePath string) *ScanResult {
	base := path.Base(filePath)
	return &ScanResult{Searchees: []*Searchee{{Name: base, Path: filePath, Files: []*ScannedFile{
		{Path: filePath, RelPath: base, Size: 100, ModTime: remoteFileModTime, FileID: remoteID(1)},
	}}}}
}

// An index built without its instance has no key for a remote identity, so every file reads as unknown.
func TestSeedingIndexScopesRemoteIdentity(t *testing.T) {
	t.Parallel()

	svc := &Service{torrentFiles: onlyTorrent("/data/torrents", "film.mkv")}
	backend := newAnswerBackend(answerWithID(remoteID(1)))

	index, err := svc.buildFileIDIndex(t.Context(), remoteInstance(models.SSHTierExec), backend, nil)

	require.NoError(t, err)
	require.True(t, index.complete, index.incompleteReason)
	hash, ok := index.hash(remoteID(1))
	require.True(t, ok)
	require.Equal(t, "aaaa", hash)
	require.True(t, index.knowsNotSeeding(remoteID(2)), "a file outside a complete index does not seed")
}

func TestRemoteIdentityFromAnotherHostDoesNotMatch(t *testing.T) {
	t.Parallel()

	row := remoteInstance(models.SSHTierExec)
	key, ok := fsops.FileKeyOf(remoteID(1), row)
	require.True(t, ok)
	byKey := map[fsops.FileKey]string{key: "aaaa"}

	otherHost := *row
	otherHost.SSHHost = "other.example.invalid"
	for _, tt := range []struct {
		name     string
		instance *models.Instance
		want     bool
	}{
		{name: "same host", instance: row, want: true},
		{name: "other host", instance: &otherHost},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := (&seedingIndex{byKey: byKey, instance: tt.instance}).hash(remoteID(1))
			require.Equal(t, tt.want, got)
		})
	}
}

// A complete index without the file shows the torrent is gone, which needs a key for the remote identity.
func TestTorrentGoneClearsRemoteAlreadySeeding(t *testing.T) {
	row := remoteInstance(models.SSHTierExec)
	key, ok := fsops.FileKeyOf(remoteID(1), row)
	require.True(t, ok)

	for _, tt := range []struct {
		name  string
		byKey map[fsops.FileKey]string
		want  models.DirScanFileStatus
	}{
		{name: "torrent removed", byKey: map[fsops.FileKey]string{}, want: models.DirScanFileStatusPending},
		{name: "still seeding", byKey: map[fsops.FileKey]string{key: "aaaa"}, want: models.DirScanFileStatusAlreadySeeding},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			trackRemoteFile(t, store, dir.ID, "/data/film.mkv", models.DirScanFileStatusAlreadySeeding)
			scanned := scanOfRemoteFile("/data/film.mkv")
			l := zerolog.Nop()

			_, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, row, scanned, &seedingIndex{byKey: tt.byKey, instance: row, complete: true}, &l)

			require.NoError(t, err)
			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, tt.want, files[0].Status)
		})
	}
}

func TestRemoteRenameKeepsVerdictByFileID(t *testing.T) {
	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)
	row := remoteInstance(models.SSHTierExec)
	trackRemoteFile(t, store, dir.ID, "/data/old/film.mkv", models.DirScanFileStatusNoMatch)
	scanned := scanOfRemoteFile("/data/new/film.mkv")
	l := zerolog.Nop()

	tracked, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, row, scanned, &seedingIndex{instance: row}, &l)

	require.NoError(t, err)
	require.Equal(t, models.DirScanFileStatusNoMatch, tracked.byPath["/data/new/film.mkv"].Status)
	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "/data/new/film.mkv", files[0].FilePath)
	require.Equal(t, models.DirScanFileStatusNoMatch, files[0].Status)
}

// Three torrents under two save paths take two finds, and a missing save path costs none of its files.
func TestFileIDIndexBatchesAcrossTorrents(t *testing.T) {
	t.Parallel()

	ids := map[string]hardlink.FileID{
		"/data/a/one.mkv":   remoteID(1),
		"/data/a/two.mkv":   remoteID(2),
		"/data/a/three.mkv": remoteID(3),
		"/data/b/four.mkv":  remoteID(4),
	}
	torrents := &seededTorrents{
		savePaths: map[string]string{"aaaa": "/data/a", "bbbb": "/data/a", "cccc": "/data/b"},
		files: map[string]qbt.TorrentFiles{
			"aaaa": {{Name: "one.mkv", Priority: 1}, {Name: "two.mkv", Priority: 1}},
			"bbbb": {{Name: "three.mkv", Priority: 1}},
			"cccc": {{Name: "four.mkv", Priority: 1}},
		},
	}
	answer := func(p string) (*fsops.LstatInfo, error) {
		return &fsops.LstatInfo{Path: p, Size: 1, FileID: ids[p]}, nil
	}
	allFiles := slices.Collect(maps.Keys(ids))

	t.Run("every save path present", func(t *testing.T) {
		t.Parallel()
		backend := newAnswerBackend(func(p string) (*fsops.LstatInfo, error) {
			if p == "/data/a" || p == "/data/b" {
				return &fsops.LstatInfo{Path: p, IsDir: true}, nil
			}
			return answer(p)
		})
		svc := &Service{torrentFiles: torrents}

		index, err := svc.buildFileIDIndex(t.Context(), remoteInstance(models.SSHTierExec), backend, nil)

		require.NoError(t, err)
		require.True(t, index.complete, index.incompleteReason)
		require.Equal(t, len(ids), index.len())
		require.Empty(t, backend.stated)
		require.Len(t, backend.batches, 2)
		require.ElementsMatch(t, []string{"/data/a", "/data/b"}, backend.batches[0])
		require.ElementsMatch(t, allFiles, backend.batches[1])
	})

	t.Run("a save path is missing", func(t *testing.T) {
		t.Parallel()
		backend := newAnswerBackend(func(p string) (*fsops.LstatInfo, error) {
			if p == "/data/a" {
				return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
			}
			if p == "/data/b" {
				return &fsops.LstatInfo{Path: p, IsDir: true}, nil
			}
			return answer(p)
		})
		svc := &Service{torrentFiles: torrents}

		index, err := svc.buildFileIDIndex(t.Context(), remoteInstance(models.SSHTierExec), backend, nil)

		require.NoError(t, err)
		require.False(t, index.complete)
		require.Equal(t, 1, index.len(), "the torrent under the other save path is still indexed")
		require.Empty(t, backend.stated)
		require.Len(t, backend.batches, 2)
		require.Equal(t, []string{"/data/b/four.mkv"}, backend.batches[1])
	})
}

// Without identity qui reads no torrent list or stat, and the index cannot clear an already seeding file.
func TestFileIDIndexSkippedWithoutIdentity(t *testing.T) {
	for _, tt := range []struct {
		name      string
		row       *models.Instance
		wantBuilt bool
	}{
		{name: "remote exec tier", row: remoteInstance(models.SSHTierExec), wantBuilt: true},
		{name: "remote sftp tier", row: remoteInstance(models.SSHTierSFTP)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			torrents := onlyTorrent("/data/torrents", "film.mkv")
			backend := newAnswerBackend(answerWithID(remoteID(1)),
				slashEntry("/data/torrents", ".", fs.ModeDir),
				slashEntry("/data/torrents/Film", "Film", fs.ModeDir),
				slashEntry("/data/torrents/Film/film.mkv", "Film/film.mkv", 0),
			)
			svc := &Service{torrentFiles: torrents}
			l := zerolog.Nop()

			result, index, ok := svc.runScanPhase(ctx, &models.DirScanDirectory{TargetInstanceID: tt.row.ID}, &scanTarget{backend: backend, instance: tt.row}, "/data/torrents", 1, &l)

			require.True(t, ok)
			require.Len(t, result.Searchees, 1, "the walk runs either way")
			require.Equal(t, tt.wantBuilt, index.complete)
			if tt.wantBuilt {
				require.Equal(t, 1, index.len())
				require.EqualValues(t, 1, torrents.reads.Load())
				return
			}
			require.Zero(t, index.len())
			require.Zero(t, torrents.reads.Load())
			require.Empty(t, backend.batches)
			require.False(t, index.knowsNotSeeding(remoteID(1)))
		})
	}
}

// A name with a .. element climbs out of a missing save path, so its file is still read.
func TestDotDotNameUnderMissingSavePath(t *testing.T) {
	const seeded = "/other/f.bin"
	row := remoteInstance(models.SSHTierExec)
	for _, tt := range []struct {
		name        string
		savePath    string
		fileName    string
		wantBatches int
		wantIndexed int
		wantStatus  models.DirScanFileStatus
	}{
		{name: "name_climbs_out_of_a_missing_save_path", savePath: "/gone", fileName: "../other/f.bin", wantBatches: 2, wantIndexed: 1, wantStatus: models.DirScanFileStatusAlreadySeeding},
		{name: "name_climbs_to_a_sibling_sharing_the_prefix", savePath: "/oth", fileName: "../other/f.bin", wantBatches: 2, wantIndexed: 1, wantStatus: models.DirScanFileStatusAlreadySeeding},
		{name: "save_path_through_a_missing_folder", savePath: "/gone/../other", fileName: "f.bin", wantBatches: 2, wantIndexed: 1, wantStatus: models.DirScanFileStatusAlreadySeeding},
		{name: "dots_inside_an_element_stay_under_the_save_path", savePath: "/gone", fileName: "sub..../x.bin", wantBatches: 1, wantStatus: models.DirScanFileStatusPending},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			svc.torrentFiles = &seededTorrents{
				savePaths: map[string]string{"aaaa": tt.savePath},
				files:     map[string]qbt.TorrentFiles{"aaaa": {{Name: tt.fileName}}},
			}
			backend := newAnswerBackend(func(p string) (*fsops.LstatInfo, error) {
				switch p {
				case seeded:
					return &fsops.LstatInfo{Path: p, Size: 100, FileID: remoteID(1)}, nil
				case "/other":
					return &fsops.LstatInfo{Path: p, IsDir: true}, nil
				}
				return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
			})
			trackRemoteFile(t, store, dir.ID, seeded, models.DirScanFileStatusAlreadySeeding)
			scanned := scanOfRemoteFile(seeded)
			l := zerolog.Nop()

			index, err := svc.buildFileIDIndex(ctx, row, backend, nil)
			require.NoError(t, err)
			_, err = svc.refreshTrackedFilesFromScan(ctx, dir.ID, row, scanned, index, &l)
			require.NoError(t, err)

			require.True(t, index.complete, index.incompleteReason)
			require.Equal(t, tt.wantIndexed, index.len())
			require.Len(t, backend.batches, tt.wantBatches)
			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, tt.wantStatus, files[0].Status)
		})
	}
}

// Each file under a missing save path answers for itself, so a skipped file listed first can't hide a wanted one.
func TestMixedPrioritiesUnderMissingSavePath(t *testing.T) {
	t.Parallel()

	index := &seedingIndex{byKey: map[fsops.FileKey]string{}, instance: remoteInstance(models.SSHTierExec), complete: true}
	files := map[string]qbt.TorrentFiles{"aaaa": {{Name: "skipped.nfo"}, {Name: "wanted.mkv", Priority: 1}}}
	backend := newAnswerBackend(func(p string) (*fsops.LstatInfo, error) {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	})

	statErrors, err := addTorrentsToFileIDIndex(t.Context(), index, files, map[string]string{"aaaa": "/gone"}, backend)

	require.NoError(t, err)
	require.False(t, index.complete)
	require.Contains(t, index.incompleteReason, "wanted.mkv")
	require.Equal(t, 2, statErrors)
}

func TestMissingSavePathEdges(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		savePath   string
		wantPaths  int
		wantErrors int
	}{
		{name: "trailing separator takes the shortcut", savePath: "/gone/", wantErrors: 1},
		{name: "root falls through to a stat", savePath: "/", wantPaths: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := &seedingIndex{byKey: map[fsops.FileKey]string{}, complete: true}
			files := map[string]qbt.TorrentFiles{"aaaa": {{Name: "f.bin"}}}
			missing := map[string]error{tt.savePath: fs.ErrNotExist}

			paths, owners, statErrors := indexMissingAndListRest(index, files, map[string]string{"aaaa": tt.savePath}, missing, fsops.SlashPaths)

			require.Len(t, paths, tt.wantPaths)
			require.Len(t, owners, tt.wantPaths)
			require.Equal(t, tt.wantErrors, statErrors)
		})
	}
}

// One StatAll keeps each file's verdict: a missing skipped file says nothing, a missing wanted one marks incomplete.
func TestSkippedFileStaysCompleteThroughStatAll(t *testing.T) {
	t.Parallel()

	present := func(p string) (*fsops.LstatInfo, error) {
		return &fsops.LstatInfo{Path: p, Size: 1, FileID: remoteID(1)}, nil
	}
	missingAt := func(missing string, cause error) func(string) (*fsops.LstatInfo, error) {
		return func(p string) (*fsops.LstatInfo, error) {
			if p == missing {
				return nil, &fs.PathError{Op: "stat", Path: p, Err: cause}
			}
			return present(p)
		}
	}
	for _, tt := range []struct {
		name         string
		missing      string
		cause        error
		priority     int
		wantComplete bool
		wantIndexed  int
		wantErrors   int
		wantReason   string
	}{
		{name: "skipped file missing", missing: "/data/gone.mkv", cause: fs.ErrNotExist, wantComplete: true, wantIndexed: 1, wantErrors: 1},
		{name: "parent of a skipped file is a file", missing: "/data/gone.mkv", cause: syscall.ENOTDIR, wantComplete: true, wantIndexed: 1, wantErrors: 1},
		{name: "wanted file missing", missing: "/data/gone.mkv", cause: fs.ErrNotExist, priority: 1, wantIndexed: 1, wantErrors: 1, wantReason: "torrent aaaa file gone.mkv"},
		{name: "skipped file unreadable", missing: "/data/gone.mkv", cause: fs.ErrPermission, wantIndexed: 1, wantErrors: 1, wantReason: "torrent aaaa file gone.mkv"},
		{name: "skipped files under a missing save path", missing: "/data", cause: fs.ErrNotExist, wantComplete: true, wantErrors: 2},
		{name: "wanted file under a missing save path", missing: "/data", cause: fs.ErrNotExist, priority: 1, wantErrors: 2, wantReason: "torrent aaaa file gone.mkv"},
		{name: "unreadable save path reads each file", missing: "/data", cause: fs.ErrPermission, priority: 1, wantComplete: true, wantIndexed: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			index := &seedingIndex{byKey: map[fsops.FileKey]string{}, instance: remoteInstance(models.SSHTierExec), complete: true}
			files := map[string]qbt.TorrentFiles{"aaaa": {{Name: "gone.mkv", Priority: tt.priority}, {Name: "here.mkv"}}}
			backend := newAnswerBackend(missingAt(tt.missing, tt.cause))

			statErrors, err := addTorrentsToFileIDIndex(t.Context(), index, files, map[string]string{"aaaa": "/data"}, backend)

			require.NoError(t, err)
			require.Equal(t, tt.wantComplete, index.complete, index.incompleteReason)
			require.Equal(t, tt.wantErrors, statErrors)
			require.Contains(t, index.incompleteReason, tt.wantReason)
			require.Equal(t, tt.wantIndexed, index.len(), "every present file has the one identity")
		})
	}
}

// A connection lost in the index fails the run, from a stat or its identity read, and other errors stay best effort.
//
// regression: #2919
func TestScanFailsWhenIndexBuildLosesConnection(t *testing.T) {
	lost := fmt.Errorf("stat: %w", fsops.ErrConnectionLost)
	saveDir := &fsops.LstatInfo{Path: "/data/torrents", IsDir: true, FileID: remoteID(9)}
	film := "/data/torrents/film.mkv"
	fileInfo := func(p string) *fsops.LstatInfo {
		return &fsops.LstatInfo{Path: p, Size: 1, FileID: remoteID(1)}
	}
	for _, tt := range []struct {
		name   string
		answer func(p string) (*fsops.LstatInfo, error)
		wantOK bool
	}{
		{
			name: "save path stat loses the connection",
			answer: func(p string) (*fsops.LstatInfo, error) {
				if p == "/data/torrents" {
					return nil, lost
				}
				return fileInfo(p), nil
			},
		},
		{
			name: "save path identity loses the connection",
			answer: func(p string) (*fsops.LstatInfo, error) {
				if p == "/data/torrents" {
					return &fsops.LstatInfo{Path: p, IsDir: true, FileIDErr: lost}, nil
				}
				return fileInfo(p), nil
			},
		},
		{
			name: "file stat loses the connection",
			answer: func(p string) (*fsops.LstatInfo, error) {
				if p == film {
					return nil, lost
				}
				return saveDir, nil
			},
		},
		{
			name: "file identity loses the connection",
			answer: func(p string) (*fsops.LstatInfo, error) {
				if p == film {
					return &fsops.LstatInfo{Path: p, Size: 1, FileIDErr: lost}, nil
				}
				return saveDir, nil
			},
		},
		{
			name: "another stat error is best effort",
			answer: func(p string) (*fsops.LstatInfo, error) {
				if p == film {
					return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrPermission}
				}
				return saveDir, nil
			},
			wantOK: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
			require.NoError(t, err)
			svc.torrentFiles = onlyTorrent("/data/torrents", "film.mkv")
			backend := newAnswerBackend(tt.answer,
				slashEntry("/data/scan", ".", fs.ModeDir),
				slashEntry("/data/scan/Film", "Film", fs.ModeDir),
				slashEntry("/data/scan/Film/film.mkv", "Film/film.mkv", 0),
			)
			target := &scanTarget{backend: backend, instance: remoteInstance(models.SSHTierExec)}
			l := zerolog.Nop()

			result, index, ok := svc.runScanPhase(ctx, dir, target, "/data/scan", runID, &l)

			require.Equal(t, tt.wantOK, ok)
			run, err := store.GetRun(ctx, runID)
			require.NoError(t, err)
			if tt.wantOK {
				require.Len(t, result.Searchees, 1)
				require.False(t, index.complete, "a best effort index does not rule out a seeding file")
				require.NotEqual(t, models.DirScanRunStatusFailed, run.Status)
				return
			}
			require.Nil(t, result)
			require.Equal(t, models.DirScanRunStatusFailed, run.Status)
			require.Contains(t, run.ErrorMessage, fsops.ErrConnectionLost.Error())
		})
	}
}
