// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/torrentname"
)

func TestCacheTorrentBlobAtomicWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	relBlob := filepath.Join("aa", "bb", "cc", "deadbeef.torrent")
	absBlob := filepath.Join(dir, relBlob)

	require.NoError(t, cacheTorrentBlob(dir, relBlob, []byte("payload")))
	got, err := os.ReadFile(absBlob)
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), got)

	// No temp files may remain next to the blob.
	entries, err := os.ReadDir(filepath.Dir(absBlob))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// An existing destination is trusted and never rewritten.
	require.NoError(t, os.WriteFile(absBlob, []byte("sentinel"), 0o600))
	require.NoError(t, cacheTorrentBlob(dir, relBlob, []byte("payload")))
	got, err = os.ReadFile(absBlob)
	require.NoError(t, err)
	require.Equal(t, []byte("sentinel"), got)
}

func TestSweepStaleBlobTemps(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blobDir := filepath.Join(dir, "aa", "bb", "cc")
	require.NoError(t, os.MkdirAll(blobDir, 0o755))
	blob := filepath.Join(blobDir, "deadbeef.torrent")
	require.NoError(t, os.WriteFile(blob, []byte("payload"), 0o600))
	stale := filepath.Join(blobDir, "deadbeef.torrent.tmp-123-1")
	require.NoError(t, os.WriteFile(stale, []byte("partial"), 0o600))
	staleRoot := filepath.Join(dir, "cafe.torrent.tmp-123-2")
	require.NoError(t, os.WriteFile(staleRoot, []byte("partial"), 0o600))
	fresh := filepath.Join(blobDir, "cafebabe.torrent.tmp-123-3")
	require.NoError(t, os.WriteFile(fresh, []byte("inflight"), 0o600))

	// Age everything but the fresh temp past the guard.
	old := time.Now().Add(-2 * blobTmpMinAge)
	for _, p := range []string{blob, stale, staleRoot} {
		require.NoError(t, os.Chtimes(p, old, old))
	}

	sweepStaleBlobTemps(dir)

	_, err := os.Stat(blob)
	require.NoError(t, err, "real blobs must survive the sweep")
	_, err = os.Stat(fresh)
	require.NoError(t, err, "temp files inside the age guard must survive")
	_, err = os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(staleRoot)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCopyTorrentFromTempAtomicWrite(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	dataDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "upload.torrent")
	payload := []byte("d" + strings.Repeat("x", 63))
	require.NoError(t, os.WriteFile(src, payload, 0o600))

	rel := filepath.Join("backups", "torrents", "aa", "bb", "cc", "deadbeef.torrent")
	require.NoError(t, svc.copyTorrentFromTemp(src, dataDir, rel))

	got, err := os.ReadFile(filepath.Join(dataDir, rel))
	require.NoError(t, err)
	require.Equal(t, payload, got)

	// No temp files may remain next to the blob.
	entries, err := os.ReadDir(filepath.Join(dataDir, filepath.Dir(rel)))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Invalid payloads are rejected before anything is written.
	badSrc := filepath.Join(t.TempDir(), "bad.torrent")
	require.NoError(t, os.WriteFile(badSrc, []byte("not bencoded, but long enough to pass the size check"), 0o600))
	require.ErrorContains(t, svc.copyTorrentFromTemp(badSrc, dataDir, filepath.Join("backups", "torrents", "bad.torrent")), "not a bencoded dict")
	_, err = os.Stat(filepath.Join(dataDir, "backups", "torrents", "bad.torrent"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestIsExportMetadataUnavailable(t *testing.T) {
	if !isExportMetadataUnavailable(qbt.ErrTorrentMetadataNotDownloadedYet) {
		t.Fatal("expected metadata-not-downloaded error to be treated as skippable")
	}

	err := errors.New("could not get export; torrent hash: deadbeef | status code: 409: unexpected status code")
	if !isExportMetadataUnavailable(err) {
		t.Fatal("expected 409 status to be treated as skippable")
	}

	err = errors.New("could not get export; torrent hash: deadbeef | status code: 500: unexpected status code")
	if isExportMetadataUnavailable(err) {
		t.Fatal("expected non-409 status to be non-skippable")
	}
}

func TestAdaptiveExportDelayCapsSlowExports(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		start := time.Now()
		// A 10s stall must not be mirrored back as a 10s sleep.
		require.NoError(t, adaptiveExportDelay(ctx, 20*time.Millisecond, 10*time.Second))
		require.Equal(t, maxAdaptiveExportDelay, time.Since(start))
	})
}

func TestAdaptiveExportDelayHonorsConfiguredMinAboveCap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		minDelay := maxAdaptiveExportDelay + 200*time.Millisecond
		start := time.Now()
		require.NoError(t, adaptiveExportDelay(ctx, minDelay, 10*time.Second))
		require.Equal(t, minDelay, time.Since(start))
	})
}

// concurrentExportStub is a thread-safe backupReader for exercising the
// concurrent export path with per-hash gates, payload, and error behavior.
type concurrentExportStub struct {
	torrents []qbt.Torrent
	// gates holds an export until its gate closes or its ctx ends. A hash
	// without a gate exports at once.
	gates map[string]chan struct{}
	errs  map[string]error
	// exported, if set, runs when an export passes its gate.
	exported func(hash string)

	mu          sync.Mutex
	calls       int
	inflight    int
	maxInflight int
}

func (s *concurrentExportStub) GetAllTorrents(context.Context, int) ([]qbt.Torrent, error) {
	return append([]qbt.Torrent(nil), s.torrents...), nil
}

func (s *concurrentExportStub) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return nil, nil
}

func (s *concurrentExportStub) GetTags(context.Context, int) ([]string, error) {
	return nil, nil
}

func (s *concurrentExportStub) GetInstanceWebAPIVersion(context.Context, int) (string, error) {
	return "", nil
}

func (s *concurrentExportStub) ExportTorrent(ctx context.Context, _ int, hash string) ([]byte, string, string, error) {
	s.mu.Lock()
	s.calls++
	s.inflight++
	if s.inflight > s.maxInflight {
		s.maxInflight = s.inflight
	}
	gate := s.gates[hash]
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()

	if gate != nil {
		select {
		case <-ctx.Done():
			return nil, "", "", ctx.Err()
		case <-gate:
		}
	}
	if s.exported != nil {
		s.exported(hash)
	}

	if err := s.errs[hash]; err != nil {
		return nil, "", "", err
	}
	return []byte("payload-" + hash), "name-" + hash, "tracker.example", nil
}

func newExportTestService(t *testing.T, stub backupReader) (*Service, int) {
	t.Helper()

	db := setupTestBackupDB(t)
	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "concurrent-export")
	store := models.NewBackupStore(db)

	require.NoError(t, store.UpsertSettings(ctx, &models.BackupSettings{
		InstanceID:        instanceID,
		Enabled:           true,
		HourlyEnabled:     true,
		KeepHourly:        1,
		IncludeCategories: true,
		IncludeTags:       true,
	}))

	svc := NewService(store, stub, Config{
		WorkerCount:    1,
		DataDir:        t.TempDir(),
		ExportThrottle: time.Millisecond,
	}, nil)
	svc.now = func() time.Time { return time.Unix(0, 0).UTC() }
	return svc, instanceID
}

func TestExecuteBackupConcurrentMatchesSerialOrdering(t *testing.T) {
	t.Parallel()

	const torrentCount = 24

	torrents := make([]qbt.Torrent, 0, torrentCount)
	errs := map[string]error{}
	for i := range torrentCount {
		hash := fmt.Sprintf("hash-%02d", i)
		var category string
		switch i % 3 {
		case 0:
			category = "cat-a"
		case 1:
			category = "cat-b"
		}
		torrents = append(torrents, qbt.Torrent{
			Hash: hash,
			// Colliding names force ensureUniquePath to disambiguate in input order.
			Name:      "Duplicate Name",
			Category:  category,
			Tags:      "tag1, tag2",
			TotalSize: int64(1000 + i),
		})
	}
	// Metadata-unavailable exports must be skipped without failing the run.
	errs["hash-05"] = errors.New("could not get export; status code: 409: unexpected status code")
	errs["hash-16"] = qbt.ErrTorrentMetadataNotDownloadedYet

	// Serial reference: what the pre-concurrency loop would have produced.
	var expectedHashes, expectedPaths []string
	expectedCounts := map[string]int{}
	var expectedBytes int64
	used := map[string]int{}
	for _, torrent := range torrents {
		if errs[torrent.Hash] != nil {
			continue
		}
		filename := torrentname.SanitizeExportFilename("name-"+torrent.Hash, torrent.Hash, "tracker.example", torrent.Hash)
		archivePath := filename
		if torrent.Category != "" {
			expectedCounts[torrent.Category]++
			archivePath = filepath.ToSlash(filepath.Join(safeSegment(torrent.Category), filename))
		} else {
			expectedCounts["(uncategorized)"]++
		}
		expectedHashes = append(expectedHashes, torrent.Hash)
		expectedPaths = append(expectedPaths, ensureUniquePath(archivePath, used))
		expectedBytes += int64(len("payload-" + torrent.Hash))
	}

	// The exports finish in an order other than the input order: within each
	// window of exportWorkers inputs, which the workers hold at once, the last
	// one finishes first. The end of each export opens the gate of the export
	// released after it.
	var release []string
	for start := 0; start < torrentCount; start += exportWorkers {
		for i := min(start+exportWorkers, torrentCount) - 1; i >= start; i-- {
			release = append(release, torrents[i].Hash)
		}
	}
	next := make(map[string]string, torrentCount)
	for i := 1; i < len(release); i++ {
		next[release[i-1]] = release[i]
	}
	stub := &concurrentExportStub{torrents: torrents, errs: errs}
	stub.exported = func(hash string) {
		if following, ok := next[hash]; ok {
			close(stub.gates[following])
		}
	}
	svc, instanceID := newExportTestService(t, stub)

	for runID := int64(1); runID <= 2; runID++ {
		stub.gates = make(map[string]chan struct{}, torrentCount)
		for _, torrent := range torrents {
			stub.gates[torrent.Hash] = make(chan struct{})
		}
		close(stub.gates[release[0]])

		// A gate chain out of step with the workers ends on this timeout
		// instead of hanging the test.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		result, err := svc.executeBackup(ctx, job{runID: runID, instanceID: instanceID, kind: models.BackupRunKindManual})
		cancel()
		require.NoError(t, err)
		require.NotNil(t, result)

		var gotHashes, gotPaths []string
		for _, item := range result.items {
			gotHashes = append(gotHashes, item.TorrentHash)
			require.NotNil(t, item.ArchiveRelPath)
			gotPaths = append(gotPaths, *item.ArchiveRelPath)
		}
		require.Equal(t, expectedHashes, gotHashes, "archive item order must match input order")
		require.Equal(t, expectedPaths, gotPaths, "unique archive paths must match a serial run")
		require.Equal(t, expectedCounts, result.categoryCounts)
		require.Equal(t, expectedBytes, result.totalBytes)
		require.Equal(t, len(expectedHashes), result.torrentCount)

		svc.progressMu.RLock()
		progress := svc.progress[runID]
		svc.progressMu.RUnlock()
		require.NotNil(t, progress)
		require.Equal(t, torrentCount, progress.Current, "skipped torrents still count toward progress")
		require.Equal(t, torrentCount, progress.Total)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.LessOrEqual(t, stub.maxInflight, exportWorkers)
	require.Equal(t, 2*torrentCount, stub.calls)
}

// rendezvousExportStub blocks every export until at least two are in flight at
// once, so a serial implementation cannot pass.
type rendezvousExportStub struct {
	concurrentExportStub
	ready     chan struct{}
	readyOnce sync.Once
}

func (s *rendezvousExportStub) ExportTorrent(ctx context.Context, _ int, hash string) ([]byte, string, string, error) {
	s.mu.Lock()
	s.inflight++
	if s.inflight >= 2 {
		s.readyOnce.Do(func() { close(s.ready) })
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()

	select {
	case <-s.ready:
		return []byte("payload-" + hash), "name-" + hash, "tracker.example", nil
	case <-ctx.Done():
		return nil, "", "", ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, "", "", errors.New("export never overlapped with another export")
	}
}

func TestExecuteBackupRunsExportsConcurrently(t *testing.T) {
	t.Parallel()

	torrents := make([]qbt.Torrent, 0, exportWorkers)
	for i := range exportWorkers {
		torrents = append(torrents, qbt.Torrent{
			Hash: fmt.Sprintf("hash-%02d", i),
			Name: fmt.Sprintf("Torrent %02d", i),
		})
	}
	stub := &rendezvousExportStub{
		torrents: torrents,
		ready:    make(chan struct{}),
	}
	svc, instanceID := newExportTestService(t, stub)

	result, err := svc.executeBackup(context.Background(), job{runID: 1, instanceID: instanceID, kind: models.BackupRunKindManual})
	require.NoError(t, err)
	require.Equal(t, len(torrents), result.torrentCount)
}

func TestExecuteBackupConcurrentFailsFastOnExportError(t *testing.T) {
	t.Parallel()

	// The first torrent fails at once while every other export waits on its
	// ctx; if the error did not cancel outstanding work, the run would never
	// end.
	errBoom := errors.New("boom")
	torrents := make([]qbt.Torrent, 0, 12)
	gates := make(map[string]chan struct{}, 12)
	for i := range 12 {
		hash := fmt.Sprintf("hash-%02d", i)
		torrents = append(torrents, qbt.Torrent{Hash: hash, Name: fmt.Sprintf("Torrent %02d", i)})
		if i > 0 {
			gates[hash] = make(chan struct{})
		}
	}
	stub := &concurrentExportStub{
		torrents: torrents,
		gates:    gates,
		errs:     map[string]error{"hash-00": errBoom},
	}
	svc, instanceID := newExportTestService(t, stub)

	done := make(chan error, 1)
	go func() {
		// t.Context ends when the test does, so a run that ignores the
		// first error does not keep its workers blocked after a failure.
		_, err := svc.executeBackup(t.Context(), job{runID: 1, instanceID: instanceID, kind: models.BackupRunKindManual})
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first error must cancel outstanding exports")
	}
	require.ErrorIs(t, err, errBoom)
	require.Contains(t, err.Error(), "export torrent hash-00")

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.Less(t, stub.calls, len(torrents), "error must stop the feed before all torrents export")
}

func TestExecuteBackupConcurrentStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	torrents := make([]qbt.Torrent, 0, 40)
	for i := range 40 {
		hash := fmt.Sprintf("hash-%02d", i)
		torrents = append(torrents, qbt.Torrent{Hash: hash, Name: fmt.Sprintf("Torrent %02d", i)})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The first export cancels the run.
	stub := &concurrentExportStub{torrents: torrents, exported: func(string) { cancel() }}
	svc, instanceID := newExportTestService(t, stub)

	_, err := svc.executeBackup(ctx, job{runID: 1, instanceID: instanceID, kind: models.BackupRunKindManual})
	require.ErrorIs(t, err, context.Canceled)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.Less(t, stub.calls, len(torrents), "cancellation must stop the feed before all torrents export")
}
