// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
)

func TestSafeDeleteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()

	disp, err := safeDeleteFile(root, target, tfm)
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

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(target))

	disp, err := safeDeleteFile(root, target, tfm)
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
	tfm := NewTorrentFileMap()

	if _, err := safeDeleteFile(root, root, tfm); err == nil {
		t.Fatalf("expected error deleting scan root")
	}
}

func TestSafeDeleteFile_RefusesEscapingPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap()

	outside := filepath.Join(root, "..", "escape.txt")
	if _, err := safeDeleteFile(root, outside, tfm); err == nil {
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

	tfm := NewTorrentFileMap()

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(root, unit, tfm, nil)
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

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(fileInUse))

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(root, unit, tfm, nil)
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

	tfm := NewTorrentFileMap()
	markerUnit := filepath.Join(movieDir, "BDMV")
	disp, err := safeDeleteTarget(root, markerUnit, tfm, nil)
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
	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(linkPath))

	disp, err := safeDeleteTarget(root, dir, tfm, nil)
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

func TestCollectCandidateDirsForCleanup_CascadesToParents(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	scanRoot := filepath.Join(base, "tv")
	showDir := filepath.Join(scanRoot, "ShowName")
	seasonDir := filepath.Join(showDir, "Season1")

	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	target := filepath.Join(seasonDir, "episode.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	disp, err := safeDeleteFile(scanRoot, target, tfm)
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}

	candidates := collectCandidateDirsForCleanup([]string{target}, []string{scanRoot}, nil)
	for _, dir := range candidates {
		_ = safeDeleteEmptyDir(scanRoot, dir)
	}

	if _, err := os.Stat(seasonDir); !os.IsNotExist(err) {
		t.Fatalf("expected season dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(showDir); !os.IsNotExist(err) {
		t.Fatalf("expected show dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(scanRoot); err != nil {
		t.Fatalf("expected scan root to remain, stat err=%v", err)
	}
}

func TestFindScanRoot_PrefersLongestMatch(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	rootA := filepath.Clean(base)
	rootB := filepath.Join(base, "tv")

	path := filepath.Join(rootB, "ShowName", "Season1", "episode.mkv")

	got := findScanRoot(path, []string{rootA, rootB})
	if filepath.Clean(got) != filepath.Clean(rootB) {
		t.Fatalf("expected longest root %q, got %q", rootB, got)
	}
}

func TestCollectCandidateDirsForCleanup_StopsAtNestedScanRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	rootA := filepath.Join(base, "tv")
	rootB := filepath.Join(rootA, "ShowName")
	target := filepath.Join(rootB, "Season1", "episode.mkv")

	candidates := collectCandidateDirsForCleanup([]string{target}, []string{rootA, rootB}, nil)
	for _, dir := range candidates {
		if filepath.Clean(dir) == filepath.Clean(rootB) {
			t.Fatalf("did not expect nested scan root in candidates: %q", dir)
		}
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

	tfm := NewTorrentFileMap()
	ignorePaths := []string{fileB}

	disp, err := safeDeleteTarget(root, dir, tfm, ignorePaths)
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

	tfm := NewTorrentFileMap()
	ignorePaths := []string{file}

	disp, err := safeDeleteTarget(root, file, tfm, ignorePaths)
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

func TestSafeDeleteTarget_AllowsDeleteWhenNoIgnorePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "to-delete")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	file := filepath.Join(dir, "orphan.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	ignorePaths := []string{} // No ignore paths

	disp, err := safeDeleteTarget(root, dir, tfm, ignorePaths)
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	// Verify directory was removed.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected directory removed, stat err=%v", err)
	}
}

// ---- Restart-safety tests (kept in this file per project convention) ----

// testQuerier wraps sql.DB to implement dbinterface.Querier for tests.
type testQuerier struct {
	*sql.DB
}

type testTx struct {
	*sql.Tx
}

func (t *testTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, query, args...)
}
func (t *testTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, query, args...)
}
func (t *testTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, query, args...)
}
func (t *testTx) Commit() error   { return t.Tx.Commit() }
func (t *testTx) Rollback() error { return t.Tx.Rollback() }

func (q *testQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := q.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &testTx{Tx: tx}, nil
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec failed: %v\nquery=%s", err, query)
	}
}

func createOrphanScanSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	mustExec(t, db, `CREATE TABLE instances (id INTEGER PRIMARY KEY)`)

	mustExec(t, db, `
		CREATE TABLE IF NOT EXISTS orphan_scan_runs (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id     INTEGER NOT NULL,
			status          TEXT NOT NULL,
			triggered_by    TEXT NOT NULL,
			scan_paths      TEXT,
			files_found     INTEGER DEFAULT 0,
			files_deleted   INTEGER DEFAULT 0,
			folders_deleted INTEGER DEFAULT 0,
			bytes_reclaimed INTEGER DEFAULT 0,
			truncated       INTEGER NOT NULL DEFAULT 0,
			error_message   TEXT,
			started_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at    DATETIME,
			FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS orphan_scan_files (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id        INTEGER NOT NULL,
			file_path     TEXT NOT NULL,
			file_size     INTEGER NOT NULL,
			modified_at   DATETIME,
			status        TEXT NOT NULL DEFAULT 'pending',
			error_message TEXT,
			FOREIGN KEY (run_id) REFERENCES orphan_scan_runs(id) ON DELETE CASCADE
		);
	`)
}

func TestOrphanScan_MarkDeletingRunsFailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	createOrphanScanSchema(t, sqlDB)
	mustExec(t, sqlDB, `INSERT INTO instances (id) VALUES (1)`)

	res, err := sqlDB.ExecContext(ctx, `
		INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, scan_paths, files_found)
		VALUES (1, 'deleting', 'manual', '[]', 2)
	`)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	runID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	store := models.NewOrphanScanStore(&testQuerier{DB: sqlDB})
	if err := store.MarkDeletingRunsFailed(ctx, "Deletion interrupted by restart"); err != nil {
		t.Fatalf("MarkDeletingRunsFailed: %v", err)
	}

	run, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run == nil {
		t.Fatalf("expected run, got nil")
	}
	if run.Status != "failed" {
		t.Fatalf("expected status failed, got %q", run.Status)
	}
	if run.ErrorMessage != "Deletion interrupted by restart" {
		t.Fatalf("expected error message %q, got %q", "Deletion interrupted by restart", run.ErrorMessage)
	}
	if run.CompletedAt == nil {
		t.Fatalf("expected completed_at set")
	}
}

func TestOrphanScan_RecoverStuckRuns_MarksDeletingFailedImmediately(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	createOrphanScanSchema(t, sqlDB)
	mustExec(t, sqlDB, `INSERT INTO instances (id) VALUES (1)`)

	// A deleting run should be failed immediately on startup.
	res, err := sqlDB.ExecContext(ctx, `
		INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, scan_paths, files_found)
		VALUES (1, 'deleting', 'manual', '[]', 2)
	`)
	if err != nil {
		t.Fatalf("insert deleting run: %v", err)
	}
	deletingID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// A fresh pending run should not be touched (threshold-based).
	res, err = sqlDB.ExecContext(ctx, `
		INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, scan_paths, files_found)
		VALUES (1, 'pending', 'manual', '[]', 0)
	`)
	if err != nil {
		t.Fatalf("insert pending run: %v", err)
	}
	pendingID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	store := models.NewOrphanScanStore(&testQuerier{DB: sqlDB})
	svc := NewService(DefaultConfig(), nil, store, nil, nil)
	if err := svc.recoverStuckRuns(ctx); err != nil {
		t.Fatalf("recoverStuckRuns: %v", err)
	}

	deletingRun, err := store.GetRun(ctx, deletingID)
	if err != nil {
		t.Fatalf("GetRun deleting: %v", err)
	}
	if deletingRun == nil {
		t.Fatalf("expected deleting run, got nil")
	}
	if deletingRun.Status != "failed" {
		t.Fatalf("expected deleting run failed, got %q", deletingRun.Status)
	}
	if deletingRun.ErrorMessage != "Deletion interrupted by restart" {
		t.Fatalf("expected error message %q, got %q", "Deletion interrupted by restart", deletingRun.ErrorMessage)
	}
	if deletingRun.CompletedAt == nil {
		t.Fatalf("expected completed_at set for deleting run")
	}

	pendingRun, err := store.GetRun(ctx, pendingID)
	if err != nil {
		t.Fatalf("GetRun pending: %v", err)
	}
	if pendingRun == nil {
		t.Fatalf("expected pending run, got nil")
	}
	if pendingRun.Status != "pending" {
		t.Fatalf("expected pending run unchanged, got %q", pendingRun.Status)
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
	tfm := NewTorrentFileMap()
	tfm.Add(filepath.Join(root, "TrackerName", "Show.S01E01.mkv"))

	disp, err := safeDeleteTarget(root, target, tfm, nil)
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
	disp, err = safeDeleteTarget(root, trackerDir, tfm, nil)
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

	if err := withinScanRoot(root, target); err != nil {
		t.Fatalf("expected target inside scan root, got %v", err)
	}
	if err := withinScanRoot(root, strings.ToLower(root)); err == nil ||
		!strings.Contains(err.Error(), "refusing to delete scan root") {
		t.Fatalf("expected refusal to delete the scan root itself, got %v", err)
	}
	outside := filepath.Join(base, "cross-seed", "other", "movie.mkv")
	if err := withinScanRoot(root, outside); err == nil ||
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

	tfm := NewTorrentFileMap()
	tfm.Add(filepath.Join(root, "linked.mkv"))

	disp, err := safeDeleteTarget(root, link, tfm, nil)
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
	disp, err = safeDeleteTarget(root, link, NewTorrentFileMap(), nil)
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

// The empty-directory cleanup walks up from a deleted file to its scan root,
// which can be spelled differently on a case-insensitive filesystem.
func TestCollectCandidateDirsForCleanup_CaseDifferentScanRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	scanRoot := filepath.Join(base, "TrackerName")
	deleted := filepath.Join(base, "trackername", "Show.S01", "Show.S01E01.mkv")

	got := collectCandidateDirsForCleanup([]string{deleted}, []string{scanRoot}, nil)

	want := []string{filepath.Join(base, "trackername", "Show.S01")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected candidate dirs %v, got %v", want, got)
	}
}
