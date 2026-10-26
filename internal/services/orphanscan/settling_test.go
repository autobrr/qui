// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type mockHealthChecker struct {
	healthy  bool
	lastSync time.Time
}

func (m *mockHealthChecker) IsHealthy() bool              { return m.healthy }
func (m *mockHealthChecker) GetLastSyncUpdate() time.Time { return m.lastSync }

type rootIdentityBackend struct {
	fsops.Backend
	infos map[string]*fsops.LstatInfo
}

func (b *rootIdentityBackend) Lstat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, ok := b.infos[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	infoCopy := *info
	return &infoCopy, nil
}

type walkDirCounter struct {
	fsops.Backend
	walks *atomic.Int32
}

func (b *walkDirCounter) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	b.walks.Add(1)
	return b.Backend.WalkDir(ctx, root, opts)
}

func TestReadinessChecks(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name        string
		client      *mockHealthChecker
		wantErr     bool
		wantErrPart string
	}{
		{
			name: "client unhealthy",
			client: &mockHealthChecker{
				healthy:  false,
				lastSync: now,
			},
			wantErr:     true,
			wantErrPart: "unhealthy",
		},
		{
			name: "never synced",
			client: &mockHealthChecker{
				healthy:  true,
				lastSync: time.Time{},
			},
			wantErr:     true,
			wantErrPart: "waiting for first sync",
		},
		{
			name: "sync data stale",
			client: &mockHealthChecker{
				healthy:  true,
				lastSync: now.Add(-5 * time.Minute),
			},
			wantErr:     true,
			wantErrPart: "sync data stale",
		},
		{
			name: "all checks pass",
			client: &mockHealthChecker{
				healthy:  true,
				lastSync: now.Add(-30 * time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkReadinessGates(tt.client)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrPart,
					"error %q should contain %q", err.Error(), tt.wantErrPart)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestIsTransientTorrentStateForOrphanScan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state qbt.TorrentState
		want  bool
	}{
		{state: qbt.TorrentStateMetaDl, want: true},
		{state: qbt.TorrentStateCheckingResumeData, want: true},
		{state: qbt.TorrentStateCheckingDl, want: true},
		{state: qbt.TorrentStateCheckingUp, want: true},
		{state: qbt.TorrentStateAllocating, want: true},
		{state: qbt.TorrentStateMoving, want: true},
		{state: qbt.TorrentStatePausedUp, want: false},
		{state: qbt.TorrentStateUploading, want: false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, isTransientTorrentStateForOrphanScan(tt.state), "state=%q", tt.state)
	}
}

func TestBuildFileMapFromTorrents_FailsWhenStableTorrentMissingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := buildFileMapFromTorrents(fsops.HostPaths, []qbt.Torrent{
		{Hash: "stable", SavePath: root, State: qbt.TorrentStatePausedUp},
	}, map[string]qbt.TorrentFiles{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stable torrents returned no files")
}

func TestBuildFileMapFromTorrents_IgnoresTorrentWithoutMetadataAtSharedRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hasMetadata := false

	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{Hash: "stable", SavePath: root, State: qbt.TorrentStatePausedUp},
			{Hash: "metadata-less", SavePath: root, State: qbt.TorrentStateStoppedDl, HasMetadata: &hasMetadata},
		},
		map[string]qbt.TorrentFiles{
			"stable": {{Name: "movie.mkv", Size: 1}},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Clean(root)}, result.scanRoots)
	assert.Empty(t, result.skippedRoots)
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(root, "movie.mkv")))
}

func TestBuildFileMapFromTorrents_SkipsTransientMissingRoots(t *testing.T) {
	t.Parallel()

	stableRoot := filepath.Join(t.TempDir(), "stable")
	transientRoot := filepath.Join(t.TempDir(), "transient")

	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{Hash: "stable", SavePath: stableRoot, State: qbt.TorrentStatePausedUp},
			{Hash: "checking", SavePath: transientRoot, State: qbt.TorrentStateCheckingResumeData},
		},
		map[string]qbt.TorrentFiles{
			"stable": {{Name: "movie.mkv", Size: 1}},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Clean(stableRoot)}, result.scanRoots)
	assert.Equal(t, []string{filepath.Clean(transientRoot)}, result.skippedRoots)
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(stableRoot, "movie.mkv")))
}

func TestBuildFileMapFromTorrents_SkipsSharedRootWhenTransientTorrentHasNoFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{Hash: "stable", SavePath: root, State: qbt.TorrentStatePausedUp},
			{Hash: "allocating", SavePath: root, State: qbt.TorrentStateAllocating},
		},
		map[string]qbt.TorrentFiles{
			"stable": {{Name: "movie.mkv", Size: 1}},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, result.scanRoots)
	assert.Equal(t, []string{filepath.Clean(root)}, result.skippedRoots)
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(root, "movie.mkv")))
}

func TestFilterCoveredScanRoots(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "library")
	child := filepath.Join(root, "transient")
	descendant := filepath.Join(child, "nested")
	sibling := filepath.Join(root, "stable")
	staging := filepath.Join(base, "staging")
	staging2 := filepath.Join(base, "staging2")
	stagingUpper := filepath.Join(base, "Staging")

	tests := []struct {
		name      string
		scanRoots []string
		covers    []string
		want      []string
	}{
		{
			name:      "keeps parent root when cover is child",
			scanRoots: []string{root, sibling},
			covers:    []string{child},
			want:      []string{filepath.Clean(root), filepath.Clean(sibling)},
		},
		{
			name:      "drops descendant root covered by ancestor",
			scanRoots: []string{descendant, sibling},
			covers:    []string{child},
			want:      []string{filepath.Clean(sibling)},
		},
		{
			name:      "drops root equal to cover",
			scanRoots: []string{staging, sibling},
			covers:    []string{staging},
			want:      []string{filepath.Clean(sibling)},
		},
		{
			name:      "keeps root that only shares a string prefix",
			scanRoots: []string{staging2},
			covers:    []string{staging},
			want:      []string{filepath.Clean(staging2)},
		},
		{
			name:      "drops root spelled with different case",
			scanRoots: []string{stagingUpper, sibling},
			covers:    []string{staging},
			want:      []string{filepath.Clean(sibling)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := filterCoveredScanRoots(fsops.HostPaths, tt.scanRoots, tt.covers)
			assert.Equal(t, tt.want, got)
		})
	}
}

// A filesystem-root cover matches every descendant. Pruning with it would
// drop every scan root; the walk must keep them and ignore the files instead.
func TestFilterCoveredScanRoots_FilesystemRoot(t *testing.T) {
	t.Parallel()

	scanRoots := []string{"/data/library", "/mnt/staging"}
	got := filterCoveredScanRoots(fsops.SlashPaths, scanRoots, []string{"/"})
	assert.Equal(t, []string{"/data/library", "/mnt/staging"}, got)

	win := winRootTestDialect{fsops.HostPaths}
	driveRoots := []string{`C:\data\library`, `C:\data\staging`}
	got = filterCoveredScanRoots(win, driveRoots, []string{win.Clean(`C:\`)})
	assert.Equal(t, []string{win.Clean(`C:\data\library`), win.Clean(`C:\data\staging`)}, got)

	uncRoots := []string{`\\server\share\library`, `\\server\share\staging`}
	wantUNC := []string{win.Clean(`\\server\share\library`), win.Clean(`\\server\share\staging`)}
	got = filterCoveredScanRoots(win, uncRoots, []string{win.Clean(`\\server\share`)})
	assert.Equal(t, wantUNC, got)
	got = filterCoveredScanRoots(win, uncRoots, []string{win.Clean(`\\server\share\`)})
	assert.Equal(t, wantUNC, got)
}

func TestMetadataIgnoreRoots(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	scanRoot := filepath.Join(base, "downloads")
	nestedRoot := filepath.Join(scanRoot, "incoming")

	got := metadataIgnoreRoots(
		context.Background(),
		[]string{scanRoot},
		[]string{nestedRoot, scanRoot, base, filepath.Join(base, "elsewhere")},
		local.NewBackend(),
		nil,
	)
	assert.Equal(t, []string{filepath.Clean(nestedRoot)}, got)
	assert.Empty(t, metadataIgnoreRoots(context.Background(), []string{scanRoot, nestedRoot}, []string{nestedRoot}, local.NewBackend(), nil))

	stagedFile := filepath.Join(nestedRoot, "pending.bin")
	orphanFile := filepath.Join(scanRoot, "orphan.bin")
	writeOldFile(t, stagedFile)
	writeOldFile(t, orphanFile)

	orphans, truncated, err := walkScanRoot(context.Background(), scanRoot, newTestProtected(t, fsops.HostPaths, got, NewTorrentFileMap(fsops.HostPaths)), 0, 100, local.NewBackend(), nil)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Equal(t, []string{fsops.NormalizePath(fsops.HostPaths, orphanFile)}, orphanPaths(orphans))

	t.Run("distinct case variants", func(t *testing.T) {
		caseRoot := filepath.Join(base, "case-sensitive")
		upperRoot := filepath.Join(caseRoot, "Incoming")
		lowerRoot := filepath.Join(caseRoot, "incoming")
		require.NoError(t, os.MkdirAll(upperRoot, 0o755))
		require.NoError(t, os.MkdirAll(lowerRoot, 0o755))

		upperInfo, err := os.Lstat(upperRoot)
		require.NoError(t, err)
		lowerInfo, err := os.Lstat(lowerRoot)
		require.NoError(t, err)
		if os.SameFile(upperInfo, lowerInfo) {
			t.Skip("filesystem is case-insensitive")
		}

		assert.Equal(t, []string{filepath.Clean(upperRoot)}, metadataIgnoreRoots(context.Background(),
			[]string{caseRoot, lowerRoot},
			[]string{upperRoot},
			local.NewBackend(),
			nil,
		))
	})
}

func TestBuildFileMapFromTorrents_ContentPathDivergesFromSavePath(t *testing.T) {
	t.Parallel()

	categoryRoot := filepath.Join(t.TempDir(), "cross-seed")
	trackerDir := filepath.Join(categoryRoot, "tracker-name")
	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{
				Hash:        "abc123",
				SavePath:    categoryRoot,
				ContentPath: filepath.Join(trackerDir, "My.Torrent", "file1.mkv"),
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"abc123": {
				{Name: "My.Torrent/file1.mkv", Size: 1000},
				{Name: "My.Torrent/file2.srt", Size: 100},
			},
		},
	)
	require.NoError(t, err)

	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(categoryRoot, "My.Torrent", "file1.mkv")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(categoryRoot, "My.Torrent", "file2.srt")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(trackerDir, "My.Torrent", "file1.mkv")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(trackerDir, "My.Torrent", "file2.srt")))
	assert.Contains(t, result.scanRoots, filepath.Clean(categoryRoot))
	assert.Contains(t, result.scanRoots, filepath.Clean(trackerDir))
}

func TestBuildFileMapFromTorrents_FlatMultiFileContentPathStaysWithinSavePath(t *testing.T) {
	t.Parallel()

	saveRoot := filepath.Join(t.TempDir(), "downloads")
	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{
				Hash:        "flat123",
				SavePath:    saveRoot,
				ContentPath: saveRoot,
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"flat123": {
				{Name: "movie.mkv", Size: 1000},
				{Name: "subs/file.srt", Size: 100},
			},
		},
	)
	require.NoError(t, err)

	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(saveRoot, "movie.mkv")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(saveRoot, "subs", "file.srt")))
	assert.Equal(t, []string{filepath.Clean(saveRoot)}, result.scanRoots)
	assert.NotContains(t, result.scanRoots, filepath.Dir(saveRoot))
}

func TestBuildFileMapFromTorrents_FlatMultiFileDivergentContentPathUsesContentRoot(t *testing.T) {
	t.Parallel()

	categoryRoot := filepath.Join(t.TempDir(), "cross-seed")
	contentRoot := filepath.Join(categoryRoot, "tracker-name")
	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{
				Hash:        "flat-divergent",
				SavePath:    categoryRoot,
				ContentPath: contentRoot,
				State:       qbt.TorrentStatePausedUp,
			},
		},
		map[string]qbt.TorrentFiles{
			"flat-divergent": {
				{Name: "extras/poster.jpg", Size: 100},
				{Name: "movie.mkv", Size: 1000},
			},
		},
	)
	require.NoError(t, err)

	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(categoryRoot, "extras", "poster.jpg")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(categoryRoot, "movie.mkv")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(contentRoot, "extras", "poster.jpg")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(contentRoot, "movie.mkv")))
	assert.Contains(t, result.scanRoots, filepath.Clean(categoryRoot))
	assert.Contains(t, result.scanRoots, filepath.Clean(contentRoot))
	assert.NotContains(t, result.scanRoots, filepath.Dir(categoryRoot))
}

// Two torrents whose save paths differ only by case name the same physical
// directory on a case-insensitive filesystem, so both spellings must resolve to
// the same owned files. See issue #2314.
func TestBuildFileMapFromTorrents_SavePathsDifferingOnlyByCase(t *testing.T) {
	t.Parallel()

	categoryRoot := filepath.Join(t.TempDir(), "cross-seed")
	upper := filepath.Join(categoryRoot, "TrackerName")
	lower := filepath.Join(categoryRoot, "trackername")

	result, err := buildFileMapFromTorrents(fsops.HostPaths,
		[]qbt.Torrent{
			{Hash: "upper", SavePath: upper, ContentPath: filepath.Join(upper, "Show.S01"), State: qbt.TorrentStatePausedUp},
			{Hash: "lower", SavePath: lower, ContentPath: filepath.Join(lower, "Movie.2024"), State: qbt.TorrentStatePausedUp},
		},
		map[string]qbt.TorrentFiles{
			"upper": {{Name: "Show.S01/Show.S01E01.mkv", Size: 1000}},
			"lower": {{Name: "Movie.2024/Movie.2024.mkv", Size: 2000}},
		},
	)
	require.NoError(t, err)

	// Each file must be found under either spelling of its directory.
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(lower, "Show.S01", "Show.S01E01.mkv")))
	assert.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(filepath.Join(upper, "Movie.2024", "Movie.2024.mkv")))
}

func TestDedupeCaseVariantRoots(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	upper := filepath.Join(base, "TrackerName")
	require.NoError(t, os.MkdirAll(upper, 0o755))
	lower := filepath.Join(base, "trackername")

	if _, err := os.Lstat(lower); err == nil {
		// Case-insensitive filesystem: one directory, two spellings, walk once.
		assert.Equal(t, []string{upper}, dedupeCaseVariantRoots(context.Background(), []string{upper, lower}, local.NewBackend(), nil))
		return
	}

	// Case-sensitive filesystem: nothing may be dropped, because every spelling
	// is a directory of its own.
	require.NoError(t, os.MkdirAll(lower, 0o755))
	missing := filepath.Join(base, "Gone")
	roots := []string{upper, lower, missing, strings.ToLower(missing)}
	assert.Equal(t, roots, dedupeCaseVariantRoots(context.Background(), roots, local.NewBackend(), nil))

	// A symlink whose name is a case variant of its target must not evict the
	// target: filepath.WalkDir does not follow a symlinked scan root, so the
	// tree would stop being scanned.
	linked := filepath.Join(base, "Linked")
	require.NoError(t, os.MkdirAll(linked, 0o755))
	link := filepath.Join(base, "linked")
	if err := os.Symlink(linked, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	assert.Equal(t, []string{link, linked}, dedupeCaseVariantRoots(context.Background(), []string{link, linked}, local.NewBackend(), nil))
}

func TestRootIdentityChecksUseBackend(t *testing.T) {
	localInfo, err := local.NewBackend().Lstat(t.Context(), t.TempDir())
	require.NoError(t, err)

	base := filepath.Join(string(filepath.Separator), "remote")
	upper := filepath.Join(base, "TrackerName")
	lower := filepath.Join(base, "trackername")
	backend := &rootIdentityBackend{
		Backend: newTestBackend(),
		infos: map[string]*fsops.LstatInfo{
			upper: localInfo,
			lower: localInfo,
		},
	}

	assert.Equal(t, []string{upper}, dedupeCaseVariantRoots(t.Context(), []string{upper, lower}, backend, nil))
	assert.Empty(t, metadataIgnoreRoots(t.Context(), []string{base, lower}, []string{upper}, backend, nil))
}

// A remote backend reports no file identity, so only a byte-identical root is
// dropped; a case variant is kept because nothing confirms it is the same dir.
func TestDedupeCaseVariantRootsDropsExactDuplicatesWithoutIdentity(t *testing.T) {
	noID := &fsops.LstatInfo{IsDir: true, FileIDErr: errors.New("no identity")}
	root := "/remote/TrackerName"
	lower := "/remote/trackername"
	backend := &rootIdentityBackend{
		Backend: newSlashBackend(nil, root, lower),
		infos:   map[string]*fsops.LstatInfo{root: noID, lower: noID},
	}

	// root, lower, root is the no-identity case: Clean does not collapse it.
	assert.Equal(t, []string{root, lower}, dedupeCaseVariantRoots(t.Context(), []string{root, lower, root}, backend, nil))
}

// Declaring the default save path alongside a torrent at the same root must not
// walk it twice when the backend cannot confirm file identity.
func TestExecuteScan_DropsExactDuplicateRootsWithoutIdentity(t *testing.T) {
	t.Parallel()

	const root = "/srv/data"
	noID := &fsops.LstatInfo{IsDir: true, FileIDErr: errors.New("no identity")}
	slash := newSlashBackend(map[string]int64{
		root + "/owned.mkv":  1,
		root + "/orphan.mkv": 2,
	}, root)
	identity := &rootIdentityBackend{
		Backend: slash,
		infos: map[string]*fsops.LstatInfo{
			root:       noID,
			root + "/": noID,
		},
	}
	walks := &atomic.Int32{}
	backend := &walkDirCounter{Backend: identity, walks: walks}

	f := newModeFixture(t, "orphanscan-dedupe-exact-roots", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
		return backend
	})
	stubSync(f.svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
	}
	stubSync(f.svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}}, nil
	}
	stubSync(f.svc).folders(qbittorrent.InstanceFolders{DefaultSavePath: root + "/"})

	settings, err := f.store.GetSettings(t.Context(), 1)
	require.NoError(t, err)
	settings.ScanDefaultSavePath = true
	_, err = f.store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	assert.Equal(t, []string{root}, run.ScanPaths)
	assert.Equal(t, int32(1), walks.Load())
}
