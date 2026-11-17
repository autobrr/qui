// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type disappearingDirBackend struct {
	fsops.Backend
}

func (b disappearingDirBackend) ReadDir(ctx context.Context, path string) ([]fsops.DirEntry, error) {
	if err := b.Remove(ctx, path, fsops.RemoveOptions{}); err != nil {
		return nil, err
	}
	return b.Backend.ReadDir(ctx, path)
}

func TestSafeDeleteEmptyDir_SkipsDirectoryRemovedBeforeReadDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "gone")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}

	backend := disappearingDirBackend{Backend: newTestBackend()}
	disp, err := safeDeleteEmptyDir(t.Context(), root, target, newTestProtected(t, fsops.HostPaths, nil, nil), backend)
	if err != nil {
		t.Fatal(err)
	}
	if disp != deleteDispositionSkippedMissing {
		t.Fatalf("disposition = %v, want skipped missing", disp)
	}
}

func TestSafeDeleteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)

	disp, err := safeDeleteFile(context.Background(), root, target, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
}

func TestSafeDeleteFile_SkipsWhenInUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(fsops.NormalizePath(fsops.HostPaths, target))

	disp, err := safeDeleteFile(context.Background(), root, target, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file to remain, stat err=%v", err)
	}
}

func TestSafeDeleteFile_RefusesScanRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap(fsops.HostPaths)

	if _, err := safeDeleteFile(context.Background(), root, root, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend()); err == nil {
		t.Fatalf("expected error deleting scan root")
	}
}

func TestSafeDeleteFile_RefusesEscapingPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap(fsops.HostPaths)

	outside := filepath.Join(root, "..", "escape.txt")
	if _, err := safeDeleteFile(context.Background(), root, outside, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend()); err == nil {
		t.Fatalf("expected error for path escaping scan root")
	}
}

func TestSafeDeleteTarget_DeletesDirectoryRecursively(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	discDir := filepath.Join(root, "Movie.2024", "BDMV", "STREAM")
	if err := os.MkdirAll(discDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileA := filepath.Join(root, "Movie.2024", "BDMV", "index.bdmv")
	fileB := filepath.Join(discDir, "00000.m2ts")
	if err := os.WriteFile(fileA, []byte("a"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(context.Background(), root, unit, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("expected directory removed, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_SkipsDirectoryWhenAnOwnedFileIsNotOnDiskYet(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	streamDir := filepath.Join(root, "Movie.2024", "BDMV", "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	partial := filepath.Join(streamDir, "00001.m2ts.!qB")
	if err := os.WriteFile(partial, []byte("partial"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(filepath.Join(streamDir, "00001.m2ts"))

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(t.Context(), root, unit, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped in use, got %v", disp)
	}
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("expected the unfinished file to stay, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_SkipsDirectoryWhenAnyFileInUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "Movie.2024", "BDMV")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileInUse := filepath.Join(dir, "index.bdmv")
	fileOther := filepath.Join(dir, "STREAM", "00000.m2ts")
	if err := os.MkdirAll(filepath.Dir(fileOther), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fileInUse, []byte("a"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(fileOther, []byte("b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(fsops.NormalizePath(fsops.HostPaths, fileInUse))

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(context.Background(), root, unit, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(fileInUse); err != nil {
		t.Fatalf("expected in-use file to remain, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_DeletingMarkerDirDoesNotDeleteSiblingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV", "STREAM")
	if err := os.MkdirAll(bdmvDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Disc content.
	fileA := filepath.Join(movieDir, "BDMV", "index.bdmv")
	fileB := filepath.Join(bdmvDir, "00000.m2ts")
	if err := os.WriteFile(fileA, []byte("a"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Sibling content in the parent folder.
	sibling := filepath.Join(movieDir, "readme.txt")
	if err := os.WriteFile(sibling, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	markerUnit := filepath.Join(movieDir, "BDMV")
	disp, err := safeDeleteTarget(context.Background(), root, markerUnit, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(markerUnit); !os.IsNotExist(err) {
		t.Fatalf("expected marker directory removed, stat err=%v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("expected sibling file to remain, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_DirectorySkipsWhenContainsInUseSymlinkFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "to-delete")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a symlink file inside the directory.
	targetFile := filepath.Join(root, "real.txt")
	if err := os.WriteFile(targetFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	linkPath := filepath.Join(dir, "linked.txt")
	if err := os.Symlink(targetFile, linkPath); err != nil {
		// Windows can require admin or Developer Mode for symlinks.
		if errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "required privilege") {
			t.Skipf("symlink not permitted on this system: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	// Mark the symlink path as in-use by a torrent.
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(fsops.NormalizePath(fsops.HostPaths, linkPath))

	disp, err := safeDeleteTarget(context.Background(), root, dir, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected directory to remain, stat err=%v", err)
	}
}

func TestFindScanRoot_PrefersLongestMatch(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	rootA := filepath.Clean(base)
	rootB := filepath.Join(base, "tv")

	path := filepath.Join(rootB, "ShowName", "Season1", "episode.mkv")

	got := findScanRoot(fsops.HostPaths, path, []string{rootA, rootB})
	if filepath.Clean(got) != filepath.Clean(rootB) {
		t.Fatalf("expected longest root %q, got %q", rootB, got)
	}
}

func TestSafeDeleteTarget_SkipsWhenContainsIgnoredFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "to-delete")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileA := filepath.Join(dir, "orphan.txt")
	fileB := filepath.Join(dir, "important.txt")
	for _, p := range []string{fileA, fileB} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	ignorePaths := []string{fileB}

	disp, err := safeDeleteTarget(context.Background(), root, dir, newTestProtected(t, fsops.HostPaths, ignorePaths, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionSkippedIgnored {
		t.Fatalf("expected skipped-ignored disposition, got %v", disp)
	}
	// Verify directory still exists.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected directory to remain, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_SkipsWhenTargetIsIgnored(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "important.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	ignorePaths := []string{file}

	disp, err := safeDeleteTarget(context.Background(), root, file, newTestProtected(t, fsops.HostPaths, ignorePaths, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionSkippedIgnored {
		t.Fatalf("expected skipped-ignored disposition, got %v", disp)
	}
	// Verify file still exists.
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("expected file to remain, stat err=%v", err)
	}
}

func TestDeletionIgnorePathsProtectFreshUnavailableRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	metadataRoot := filepath.Join(root, "metadata-pending")
	skippedRoot := filepath.Join(root, "checking")
	metadataFile := filepath.Join(metadataRoot, "pending.bin")
	skippedFile := filepath.Join(skippedRoot, "existing.bin")
	writeOldFile(t, metadataFile)
	writeOldFile(t, skippedFile)

	ignorePaths, err := NormalizeIgnorePaths(fsops.HostPaths, scanIgnorePaths(context.Background(), nil, []string{root}, &buildFileMapResult{
		metadataRoots: []string{metadataRoot},
		skippedRoots:  []string{skippedRoot},
	}, local.NewBackend(), nil))
	if err != nil {
		t.Fatalf("NormalizeIgnorePaths: %v", err)
	}

	for _, target := range []string{metadataFile, skippedFile} {
		disp, err := safeDeleteTarget(context.Background(), root, target, newTestProtected(t, fsops.HostPaths, ignorePaths, NewTorrentFileMap(fsops.HostPaths)), local.NewBackend())
		if err != nil {
			t.Fatalf("safeDeleteTarget(%q): %v", target, err)
		}
		if disp != deleteDispositionSkippedIgnored {
			t.Fatalf("safeDeleteTarget(%q) disposition: got %v want %v", target, disp, deleteDispositionSkippedIgnored)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("stat protected target %q: %v", target, err)
		}
	}
}

func TestSafeDeleteTarget_FailsClosedWhenChildStatFails(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission-based stat failure is not reproducible via chmod on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	root := t.TempDir()
	dir := filepath.Join(root, "to-delete")
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ownedFile := filepath.Join(locked, "owned.bin")
	if err := os.WriteFile(ownedFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Readable but not searchable: the walk enumerates the child's name, but
	// its metadata lookup fails.
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	t.Run("torrent-owned child is treated as in use", func(t *testing.T) {
		tfm := NewTorrentFileMap(fsops.HostPaths)
		tfm.Add(fsops.NormalizePath(fsops.HostPaths, ownedFile))

		disp, err := safeDeleteTarget(context.Background(), root, dir, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
		if err != nil {
			t.Fatalf("safeDeleteTarget error: %v", err)
		}
		if disp != deleteDispositionSkippedInUse {
			t.Fatalf("expected skipped-in-use disposition, got %v", disp)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("expected directory to remain, stat err=%v", err)
		}
	})

	t.Run("unverifiable child aborts the deletion", func(t *testing.T) {
		_, err := safeDeleteTarget(context.Background(), root, dir, newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths)), local.NewBackend())
		if err == nil {
			t.Fatal("expected error when child metadata cannot be read")
		}
		if _, statErr := os.Stat(dir); statErr != nil {
			t.Fatalf("expected directory to remain, stat err=%v", statErr)
		}
	})
}

// regression: #4146
func TestOrphanScan_RecoverStuckRuns_FailsEveryInterruptedRun(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "orphanscan-recover-stuck-runs")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("instance store: %v", err)
	}
	interrupted, err := instances.Create(ctx, "interrupted", "http://127.0.0.1:8080", "u", "p", nil, nil, false, nil)
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	previewed, err := instances.Create(ctx, "previewed", "http://127.0.0.1:8081", "u", "p", nil, nil, false, nil)
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	insertRun := func(instanceID int, status string, startedAt time.Time) int64 {
		t.Helper()
		res, err := db.ExecContext(ctx, `
			INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, scan_paths, files_found, started_at)
			VALUES (?, ?, 'manual', '[]', 2, ?)
		`, instanceID, status, startedAt.UTC().Format(time.DateTime))
		if err != nil {
			t.Fatalf("insert %s run: %v", status, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id: %v", err)
		}
		return id
	}

	now := time.Now()
	deletingID := insertRun(interrupted.ID, "deleting", now.Add(-5*time.Minute))
	scanningID := insertRun(interrupted.ID, "scanning", now.Add(-5*time.Minute))
	pendingID := insertRun(interrupted.ID, "pending", now)
	previewID := insertRun(previewed.ID, "preview_ready", now.Add(-5*time.Minute))

	store := models.NewOrphanScanStore(db)
	svc := NewService(DefaultConfig(), nil, store, nil, nil, nil)
	if err := svc.recoverStuckRuns(ctx); err != nil {
		t.Fatalf("recoverStuckRuns: %v", err)
	}

	for id, wantMessage := range map[int64]string{
		deletingID: "Deletion interrupted by restart",
		scanningID: "Scan interrupted by restart",
		pendingID:  "Scan interrupted by restart",
	} {
		run, err := store.GetRun(ctx, id)
		if err != nil {
			t.Fatalf("GetRun %d: %v", id, err)
		}
		if run.Status != "failed" {
			t.Fatalf("run %d: expected failed, got %q", id, run.Status)
		}
		if run.ErrorMessage != wantMessage {
			t.Fatalf("run %d: expected error message %q, got %q", id, wantMessage, run.ErrorMessage)
		}
		if run.CompletedAt == nil {
			t.Fatalf("run %d: expected completed_at set", id)
		}
	}

	previewRun, err := store.GetRun(ctx, previewID)
	if err != nil {
		t.Fatalf("GetRun preview: %v", err)
	}
	if previewRun.Status != "preview_ready" {
		t.Fatalf("expected preview_ready run unchanged, got %q", previewRun.Status)
	}

	if _, err := store.CreateRunIfNoActive(ctx, interrupted.ID, "scheduled"); err != nil {
		t.Fatalf("expected a new run after recovery: %v", err)
	}
}

// A case-insensitive filesystem can hand the deletion flow a target spelled
// differently from the file map entry and from the scan root it was found under.
// Neither may lead to deleting an owned file. See issue #2314.
func TestSafeDeleteTarget_CaseDifferentOwnedFileIsSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	trackerDir := filepath.Join(root, "trackername")
	if err := os.MkdirAll(trackerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(trackerDir, "Show.S01E01.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Owned under the other casing of the same directory.
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(filepath.Join(root, "TrackerName", "Show.S01E01.mkv"))

	disp, err := safeDeleteTarget(context.Background(), root, target, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget file: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected owned file to remain, stat err=%v", err)
	}

	// Same guard when the target is the directory (recursive delete path).
	disp, err = safeDeleteTarget(context.Background(), root, trackerDir, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget dir: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition for directory, got %v", disp)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected owned file to survive directory delete, stat err=%v", err)
	}
}

func TestWithinScanRoot_CaseDifferentScanRootDoesNotEscape(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "cross-seed", "TrackerName")
	target := filepath.Join(base, "cross-seed", "trackername", "Show.S01E01.mkv")

	if err := withinScanRoot(fsops.HostPaths, root, target); err != nil {
		t.Fatalf("expected target inside scan root, got %v", err)
	}
	if err := withinScanRoot(fsops.HostPaths, root, strings.ToLower(root)); err == nil ||
		!strings.Contains(err.Error(), "refusing to delete scan root") {
		t.Fatalf("expected refusal to delete the scan root itself, got %v", err)
	}
	outside := filepath.Join(base, "cross-seed", "other", "movie.mkv")
	if err := withinScanRoot(fsops.HostPaths, root, outside); err == nil ||
		!strings.Contains(err.Error(), "escapes scan root") {
		t.Fatalf("expected escape error for %q, got %v", outside, err)
	}
}

// safeDeleteTarget routes symlinks to safeDeleteSymlink, whose in-use re-check
// folds case like every other one.
func TestSafeDeleteSymlink_CaseDifferentOwnedLinkIsSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	payload := filepath.Join(root, "payload.mkv")
	if err := os.WriteFile(payload, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	link := filepath.Join(root, "Linked.mkv")
	if err := os.Symlink(payload, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(filepath.Join(root, "linked.mkv"))

	disp, err := safeDeleteTarget(context.Background(), root, link, newTestProtected(t, fsops.HostPaths, nil, tfm), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget symlink: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("expected symlink to remain, lstat err=%v", err)
	}

	// Not owned: the link goes, under its real (unfolded) name.
	disp, err = safeDeleteTarget(context.Background(), root, link, newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths)), local.NewBackend())
	if err != nil {
		t.Fatalf("safeDeleteTarget symlink: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("expected symlink removed, lstat err=%v", err)
	}
}
