// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// walkSpy is the local backend, and it records the roots it is asked to walk
// and the directories it is asked to list.
type walkSpy struct {
	fsops.Backend
	mu        sync.Mutex
	walkRoots []string
	readDirs  []string
}

func newWalkSpy() *walkSpy { return &walkSpy{Backend: local.NewBackend()} }

func (s *walkSpy) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	s.mu.Lock()
	s.walkRoots = append(s.walkRoots, root)
	s.mu.Unlock()
	return s.Backend.WalkDir(ctx, root, opts)
}

func (s *walkSpy) ReadDir(ctx context.Context, p string) ([]fsops.DirEntry, error) {
	s.mu.Lock()
	s.readDirs = append(s.readDirs, p)
	s.mu.Unlock()
	return s.Backend.ReadDir(ctx, p)
}

func writeTestFile(t *testing.T, p string, size int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, make([]byte, size), 0o600))
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	err := os.Symlink(target, link)
	if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
		t.Skipf("symlink creation requires privileges: %v", err)
	}
	require.NoError(t, err)
}

// describeScan renders a scan result relative to root, one line per searchee
// and one per file, so two results compare as text.
func describeScan(root string, result *ScanResult) []string {
	var lines []string
	for _, s := range result.Searchees {
		rel, _ := filepath.Rel(root, s.Path)
		lines = append(lines, fmt.Sprintf("%s name=%q disc=%v", filepath.ToSlash(rel), s.Name, s.IsDisc))
		for _, f := range s.Files {
			fileRel, _ := filepath.Rel(root, f.Path)
			lines = append(lines, fmt.Sprintf("  %s rel=%s size=%d id=%v", filepath.ToSlash(fileRel), filepath.ToSlash(f.RelPath), f.Size, !f.FileID.IsZero()))
		}
	}
	return append(lines, fmt.Sprintf("total files=%d size=%d skipped=%d", result.TotalFiles, result.TotalSize, result.SkippedFiles))
}

// The single walk finds the same searchees and files as one walk per
// searchee did on develop. This test passes on develop too: it pins the
// result, which the change must not move.
func TestScanDirectory_SingleWalkKeepsTheSearchees(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// A folder: media files at any depth, hidden entries and other files left out.
	writeTestFile(t, filepath.Join(root, "Alpha.Movie.2024", "alpha.mkv"), 5)
	writeTestFile(t, filepath.Join(root, "Alpha.Movie.2024", "alpha.nfo"), 2)
	writeTestFile(t, filepath.Join(root, "Alpha.Movie.2024", "poster.jpg"), 3)
	writeTestFile(t, filepath.Join(root, "Alpha.Movie.2024", ".hidden.mkv"), 4)
	writeTestFile(t, filepath.Join(root, "Alpha.Movie.2024", ".git", "object.mkv"), 4)
	writeTestFile(t, filepath.Join(root, "Alpha.Movie.2024", "Sample", "sample.mkv"), 1)
	// A disc layout keeps every file, also the ones listed before its marker.
	writeTestFile(t, filepath.Join(root, "Disc.Film.2023", "AACS", "Unit_Key_RO.inf"), 6)
	writeTestFile(t, filepath.Join(root, "Disc.Film.2023", "BDMV", "index.bdmv"), 7)
	writeTestFile(t, filepath.Join(root, "Disc.Film.2023", "BDMV", "STREAM", "00000.m2ts"), 8)
	writeTestFile(t, filepath.Join(root, "Disc.Film.2023", "CERTIFICATE", "id.bdmv"), 9)
	writeTestFile(t, filepath.Join(root, "Disc.Film.2023", "cover.jpg"), 1)
	writeTestFile(t, filepath.Join(root, "lower.disc", "video_ts", "VTS_01_1.VOB"), 2)
	writeTestFile(t, filepath.Join(root, "lower.disc", "info.txt"), 1)
	// A file named like a marker does not make a disc either.
	writeTestFile(t, filepath.Join(root, "File.Marker", "BDMV"), 1)
	writeTestFile(t, filepath.Join(root, "File.Marker", "movie.mkv"), 2)
	// A marker below the top level does not make a disc.
	writeTestFile(t, filepath.Join(root, "Deep.Folder", "Extras", "BDMV", "x.m2ts"), 3)
	writeTestFile(t, filepath.Join(root, "Deep.Folder", "extra.txt"), 1)
	// No media, no searchee.
	writeTestFile(t, filepath.Join(root, "Empty.Folder", "readme.txt"), 1)
	// Root-level files.
	writeTestFile(t, filepath.Join(root, "Single.Episode.S01E01.mkv"), 11)
	writeTestFile(t, filepath.Join(root, "notes.txt"), 1)
	writeTestFile(t, filepath.Join(root, ".Hidden.Movie.mkv"), 1)
	// Links: a root-level link is read through its target, a link inside a
	// folder and a link to a folder are left out.
	writeTestFile(t, filepath.Join(outside, "target.mkv"), 13)
	symlinkOrSkip(t, filepath.Join(outside, "target.mkv"), filepath.Join(root, "Linked.Film.2022.mkv"))
	symlinkOrSkip(t, filepath.Join(outside, "target.mkv"), filepath.Join(root, "Alpha.Movie.2024", "link.mkv"))
	symlinkOrSkip(t, outside, filepath.Join(root, "Dir Alias.mkv"))
	symlinkOrSkip(t, filepath.Join(outside, "missing.mkv"), filepath.Join(root, "dangling.mkv"))

	result, err := NewScanner(local.NewBackend()).ScanDirectory(t.Context(), root)
	require.NoError(t, err)

	require.Equal(t, []string{
		`Alpha.Movie.2024 name="Alpha.Movie.2024" disc=false`,
		`  Alpha.Movie.2024/Sample/sample.mkv rel=Sample/sample.mkv size=1 id=true`,
		`  Alpha.Movie.2024/alpha.mkv rel=alpha.mkv size=5 id=true`,
		`  Alpha.Movie.2024/alpha.nfo rel=alpha.nfo size=2 id=true`,
		`Deep.Folder name="Deep.Folder" disc=false`,
		`  Deep.Folder/Extras/BDMV/x.m2ts rel=Extras/BDMV/x.m2ts size=3 id=true`,
		`Disc.Film.2023 name="Disc.Film.2023" disc=true`,
		`  Disc.Film.2023/AACS/Unit_Key_RO.inf rel=AACS/Unit_Key_RO.inf size=6 id=true`,
		`  Disc.Film.2023/BDMV/STREAM/00000.m2ts rel=BDMV/STREAM/00000.m2ts size=8 id=true`,
		`  Disc.Film.2023/BDMV/index.bdmv rel=BDMV/index.bdmv size=7 id=true`,
		`  Disc.Film.2023/CERTIFICATE/id.bdmv rel=CERTIFICATE/id.bdmv size=9 id=true`,
		`  Disc.Film.2023/cover.jpg rel=cover.jpg size=1 id=true`,
		`File.Marker name="File.Marker" disc=false`,
		`  File.Marker/movie.mkv rel=movie.mkv size=2 id=true`,
		`Linked.Film.2022.mkv name="Linked.Film.2022" disc=false`,
		`  Linked.Film.2022.mkv rel=Linked.Film.2022.mkv size=13 id=true`,
		`Single.Episode.S01E01.mkv name="Single.Episode.S01E01" disc=false`,
		`  Single.Episode.S01E01.mkv rel=Single.Episode.S01E01.mkv size=11 id=true`,
		`lower.disc name="lower.disc" disc=true`,
		`  lower.disc/info.txt rel=info.txt size=1 id=true`,
		`  lower.disc/video_ts/VTS_01_1.VOB rel=video_ts/VTS_01_1.VOB size=2 id=true`,
		`total files=14 size=71 skipped=0`,
	}, describeScan(root, result))

	// A scan root that is a link to the folder finds the same searchees
	// under the link.
	aliasRoot := filepath.Join(outside, "alias")
	symlinkOrSkip(t, root, aliasRoot)
	viaAlias, err := NewScanner(local.NewBackend()).ScanDirectory(t.Context(), aliasRoot)
	require.NoError(t, err)
	require.Equal(t, describeScan(root, result), describeScan(aliasRoot, viaAlias))
}

// failingDirBackend is the local backend, and it reports a read error for
// one directory after that directory's own entry, as a walk does when it
// cannot list a directory.
type failingDirBackend struct {
	fsops.Backend
	dir string
	err error
}

func (b failingDirBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	in, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil {
		return nil, err
	}
	out := make(chan fsops.WalkEntry)
	go func() {
		defer close(out)
		for e := range in {
			batch := []fsops.WalkEntry{e}
			if e.Path == b.dir {
				batch = append(batch, fsops.WalkEntry{Path: e.Path, RelPath: e.RelPath, Err: b.err})
			}
			for _, e := range batch {
				select {
				case out <- e:
				case <-ctx.Done():
				}
			}
		}
	}()
	return out, nil
}

// A denied subfolder inside a searchee is skipped, and any other read error
// drops the searchee, as one walk per searchee did.
func TestScanDirectory_ReadErrorInsideASearchee(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want []string
	}{
		{name: "permission denied", err: &fs.PathError{Op: "open", Err: fs.ErrPermission}, want: []string{"Film", "Other"}},
		{name: "input/output error", err: &fs.PathError{Op: "readdirent", Err: syscall.EIO}, want: []string{"Other"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "Film", "film.mkv"), 1)
			require.NoError(t, os.Mkdir(filepath.Join(root, "Film", "Extras"), 0o750))
			writeTestFile(t, filepath.Join(root, "Other", "other.mkv"), 1)
			backend := failingDirBackend{Backend: local.NewBackend(), dir: filepath.Join(root, "Film", "Extras"), err: tt.err}

			result, err := NewScanner(backend).ScanDirectory(t.Context(), root)
			require.NoError(t, err)

			names := make([]string, 0, len(result.Searchees))
			for _, s := range result.Searchees {
				names = append(names, s.Name)
			}
			require.Equal(t, tt.want, names)
		})
	}
}

// A Full scan of N searchees walks the backend once, where it walked each
// searchee and listed each folder for the disc check.
func TestScanDirectory_WalksTheRootOnce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const searchees = 4
	for i := range searchees {
		writeTestFile(t, filepath.Join(root, fmt.Sprintf("Film.%d.2024", i), "film.mkv"), 1)
	}
	spy := newWalkSpy()

	result, err := NewScanner(spy).ScanDirectory(t.Context(), root)
	require.NoError(t, err)

	require.Len(t, result.Searchees, searchees)
	require.Equal(t, []string{root}, spy.walkRoots)
	require.Empty(t, spy.readDirs)
}

// A Webhook scan walks only the imported folder, and tracks only its files.
func TestWebhookScanWalksOnlyTheImportedFolder(t *testing.T) {
	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)
	imported := filepath.Join(dir.Path, "Show")
	episode := filepath.Join(imported, "Season 01", "Show.S01E01.1080p.WEB.x264-GRP.mkv")
	writeTestFile(t, episode, 1)
	writeTestFile(t, filepath.Join(dir.Path, "Other.Film.2024", "film.mkv"), 1)
	spy := newWalkSpy()
	svc.backendPool = fsops.NewPool(&fakeInstanceStore{instance: &models.Instance{ID: dir.TargetInstanceID, HasLocalFilesystemAccess: true}}, spy)
	runID, err := store.CreateRun(ctx, dir.ID, triggeredByWebhook, imported)
	require.NoError(t, err)

	svc.executeScan(ctx, dir.ID, runID)

	require.Equal(t, []string{imported}, spy.walkRoots)
	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, episode, files[0].FilePath)
}
