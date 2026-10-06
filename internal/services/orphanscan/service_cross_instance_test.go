// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

type stubHealthChecker struct {
	healthy  bool
	lastSync time.Time
}

func (s stubHealthChecker) IsHealthy() bool              { return s.healthy }
func (s stubHealthChecker) GetLastSyncUpdate() time.Time { return s.lastSync }

func remoteInstance(id int, host string, port int) *models.Instance {
	return &models.Instance{
		ID: id, Name: fmt.Sprintf("remote-%d", id), IsActive: true,
		SSHHost: host, SSHPort: port, SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-pin",
	}
}

func localInstance(id int) *models.Instance {
	return &models.Instance{ID: id, Name: fmt.Sprintf("local-%d", id), IsActive: true, HasLocalFilesystemAccess: true}
}

func TestGetOverlapCandidateInstances(t *testing.T) {
	t.Parallel()

	clearedKey := remoteInstance(3, "box.example", 22)
	clearedKey.SSHKeyEncrypted = ""
	noPin := remoteInstance(4, "box.example", 22)
	noPin.SSHHostKeyEncrypted = ""
	mounted := remoteInstance(5, "box.example", 22)
	mounted.HasLocalFilesystemAccess = true
	inactive := remoteInstance(10, "box.example", 22)
	inactive.IsActive = false

	listed := []*models.Instance{
		remoteInstance(1, "box.example", 22),
		remoteInstance(2, "box.example", 22),
		clearedKey,
		noPin,
		mounted,
		remoteInstance(6, "other.example", 22),
		remoteInstance(7, "box.example", 2222),
		{ID: 8, Name: "local", IsActive: true, HasLocalFilesystemAccess: true},
		{ID: 9, Name: "none", IsActive: true},
		inactive,
		{ID: 11, Name: "local-inactive", IsActive: false, HasLocalFilesystemAccess: true},
	}
	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return listed, nil
	}

	for _, tc := range []struct {
		name    string
		scanned *models.Instance
		want    []int
	}{
		// Same host and port protects whatever the instance's key, pin or local flag.
		{"remote scan pairs by stored endpoint", listed[0], []int{2, 3, 4, 5}},
		{"local scan ignores remote instances", listed[7], []int{5}},
		// A local scan through a mount of the seedbox pairs with the instances reaching it over SSH.
		{"local scan with an SSH host pairs by stored endpoint", mounted, []int{1, 2, 3, 4, 8}},
		// The endpoint is the scanned row's, not the one the list holds for it now.
		{"remote scan pairs by the scanned row, not the list", remoteInstance(1, "other.example", 22), []int{6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.getOverlapCandidateInstances(context.Background(), tc.scanned)
			if err != nil {
				t.Fatalf("getOverlapCandidateInstances: %v", err)
			}
			ids := make([]int, 0, len(got))
			for _, inst := range got {
				ids = append(ids, inst.ID)
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("candidates = %v, want %v", ids, tc.want)
			}
		})
	}
}

// An instance with credentials but no SSH host never pairs with a remote scan.
func TestGetOverlapCandidateInstances_EmptyHostIsIgnored(t *testing.T) {
	t.Parallel()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			remoteInstance(1, "box.example", 22),
			{ID: 2, Name: "no-host", IsActive: true, SSHPort: 22, SSHKeyEncrypted: "enc-key"},
		}, nil
	}

	got, err := svc.getOverlapCandidateInstances(context.Background(), remoteInstance(1, "box.example", 22))
	if err != nil {
		t.Fatalf("getOverlapCandidateInstances: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no candidates, got %+v", got)
	}
}

// The error names the reason the unavailable peer was a candidate, not the scanned instance's mode.
func TestBuildFileMap_FailsWhenOverlapPeerUnavailable(t *testing.T) {
	t.Parallel()

	mounted := remoteInstance(1, "box.example", 22)
	mounted.HasLocalFilesystemAccess = true

	for _, tc := range []struct {
		name    string
		scanned *models.Instance
		peer    *models.Instance
		want    string
	}{
		{"remote scan, remote peer", remoteInstance(1, "box.example", 22), remoteInstance(2, "box.example", 22), "other remote instance on the same host"},
		{"local scan, SSH-only peer", mounted, remoteInstance(2, "box.example", 22), "other remote instance on the same host"},
		{"local scan, local peer", localInstance(1), localInstance(2), "other local-access instance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
			stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
				return []*models.Instance{tc.scanned, tc.peer}, nil
			}
			stubSync(svc).getClient = func(_ context.Context, instanceID int) (healthChecker, error) {
				if instanceID == 2 {
					return nil, errors.New("qBittorrent unreachable")
				}
				return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-10 * time.Second)}, nil
			}
			stubSync(svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
				return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
			}
			stubSync(svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
				return map[string]qbt.TorrentFiles{"a": {{Name: "one.mkv", Size: 1}}}, nil
			}
			stubSync(svc).getLastCompletedRun = func(_ context.Context, _ int) (*models.OrphanScanRun, error) { return nil, nil }

			_, err := svc.buildFileMap(context.Background(), tc.scanned, newTestBackend(), scanScope{})
			if err == nil {
				t.Fatal("expected the scan to fail closed")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not name the %q: %v", tc.want, err)
			}
		})
	}
}

func TestBuildFileMap_CrossInstance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	metadataRoot := filepath.Join(root, "incoming")
	hasMetadata := false

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{
				{Hash: "B", SavePath: root, State: qbt.TorrentStatePausedUp},
				{Hash: "C", SavePath: metadataRoot, State: qbt.TorrentStateStoppedDl, HasMetadata: &hasMetadata},
			}, nil
		default:
			return nil, nil
		}
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		switch instanceID {
		case 1:
			return map[string]qbt.TorrentFiles{
				"a": {{Name: "one.mkv", Size: 1}},
			}, nil
		case 2:
			return map[string]qbt.TorrentFiles{
				"b": {{Name: "two.mkv", Size: 1}},
			}, nil
		default:
			return map[string]qbt.TorrentFiles{}, nil
		}
	}

	result, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err != nil {
		t.Fatalf("buildFileMap: %v", err)
	}

	if !result.fileMap.Has(normalizePath(filepath.Join(root, "one.mkv"))) {
		t.Fatalf("expected instance 1 file to be protected")
	}
	if !result.fileMap.Has(normalizePath(filepath.Join(root, "two.mkv"))) {
		t.Fatalf("expected instance 2 file to be protected")
	}

	gotRoots := slices.Clone(result.scanRoots)
	slices.Sort(gotRoots)
	wantRoots := []string{filepath.Clean(root)}
	if !slices.Equal(gotRoots, wantRoots) {
		t.Fatalf("scanRoots mismatch: got=%v want=%v", gotRoots, wantRoots)
	}
	if got := metadataIgnoreRoots(context.Background(), result.scanRoots, result.metadataRoots, newTestBackend()); !slices.Equal(got, []string{filepath.Clean(metadataRoot)}) {
		t.Fatalf("metadataIgnoreRoots mismatch: got=%v want=%v", got, []string{filepath.Clean(metadataRoot)})
	}
}

func TestBuildFileMap_MergesOtherInstanceWhenOnlyContentPathsOverlap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	instanceOneSaveRoot := filepath.Join(root, "qb1", "cross-seed")
	instanceTwoSaveRoot := filepath.Join(root, "qb2", "cross-seed")
	sharedContentRoot := filepath.Join(root, "shared", "tracker-name")

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{
				Hash:        "A",
				SavePath:    instanceOneSaveRoot,
				ContentPath: filepath.Join(sharedContentRoot, "Movie.One", "file1.mkv"),
				State:       qbt.TorrentStatePausedUp,
			}}, nil
		case 2:
			return []qbt.Torrent{{
				Hash:        "B",
				SavePath:    instanceTwoSaveRoot,
				ContentPath: filepath.Join(sharedContentRoot, "Movie.Two", "file1.mkv"),
				State:       qbt.TorrentStatePausedUp,
			}}, nil
		default:
			return nil, nil
		}
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		switch instanceID {
		case 1:
			return map[string]qbt.TorrentFiles{
				"a": {{Name: "Movie.One/file1.mkv", Size: 1}},
			}, nil
		case 2:
			return map[string]qbt.TorrentFiles{
				"b": {{Name: "Movie.Two/file1.mkv", Size: 1}},
			}, nil
		default:
			return map[string]qbt.TorrentFiles{}, nil
		}
	}

	result, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err != nil {
		t.Fatalf("buildFileMap: %v", err)
	}

	if !result.fileMap.Has(normalizePath(filepath.Join(sharedContentRoot, "Movie.One", "file1.mkv"))) {
		t.Fatalf("expected instance 1 actual content path to be protected")
	}
	if !result.fileMap.Has(normalizePath(filepath.Join(sharedContentRoot, "Movie.Two", "file1.mkv"))) {
		t.Fatalf("expected instance 2 actual content path to be merged when content paths overlap")
	}
}

func TestBuildFileMap_BailsWhenOtherLocalInstanceUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	offlineErr := errors.New("offline")

	stubSync(svc).getClient = func(_ context.Context, instanceID int) (healthChecker, error) {
		if instanceID == 2 {
			return nil, offlineErr
		}
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"a": {{Name: "one.mkv", Size: 1}},
		}, nil
	}

	_, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, offlineErr) {
		t.Fatalf("expected offline error, got %v", err)
	}
}

func TestBuildFileMap_BailsWhenOverlappingInstanceFileMapUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	offlineErr := errors.New("offline")

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		default:
			return nil, nil
		}
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		if instanceID == 2 {
			return nil, offlineErr
		}
		return map[string]qbt.TorrentFiles{
			"a": {{Name: "one.mkv", Size: 1}},
		}, nil
	}

	_, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, offlineErr) {
		t.Fatalf("expected offline error, got %v", err)
	}
}

func TestBuildFileMap_DoesNotMergeWhenNoOverlap(t *testing.T) {
	t.Parallel()

	rootA := t.TempDir()
	rootB := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: rootA, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: rootB, State: qbt.TorrentStatePausedUp}}, nil
		default:
			return nil, nil
		}
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		switch instanceID {
		case 1:
			return map[string]qbt.TorrentFiles{
				"a": {{Name: "one.mkv", Size: 1}},
			}, nil
		case 2:
			return map[string]qbt.TorrentFiles{
				"b": {{Name: "two.mkv", Size: 1}},
			}, nil
		default:
			return map[string]qbt.TorrentFiles{}, nil
		}
	}

	result, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err != nil {
		t.Fatalf("buildFileMap: %v", err)
	}

	if !result.fileMap.Has(normalizePath(filepath.Join(rootA, "one.mkv"))) {
		t.Fatalf("expected instance 1 file to be protected")
	}
	if result.fileMap.Has(normalizePath(filepath.Join(rootB, "two.mkv"))) {
		t.Fatalf("did not expect instance 2 file to be merged without overlap")
	}
}

func TestInstanceScanRootsForOverlap_EmptyHealthyInstanceDoesNotUseStaleFallback(t *testing.T) {
	t.Parallel()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{}, nil
	}

	stubSync(svc).getLastCompletedRun = func(_ context.Context, _ int) (*models.OrphanScanRun, error) {
		return &models.OrphanScanRun{ScanPaths: []string{"/stale/root"}}, nil
	}

	roots, source, err := svc.instanceScanRootsForOverlap(context.Background(), 2)
	if err != nil {
		t.Fatalf("instanceScanRootsForOverlap: %v", err)
	}
	if source != "live" {
		t.Fatalf("source mismatch: got=%q want=%q", source, "live")
	}
	if len(roots) != 0 {
		t.Fatalf("expected no roots for empty instance, got=%v", roots)
	}
}

func TestBuildFileMap_MergesSkippedRootsFromOverlappingInstance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stableRoot := filepath.Join(root, "stable")
	skippedRoot := filepath.Join(stableRoot, "partial")

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: stableRoot, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: skippedRoot, State: qbt.TorrentStateCheckingResumeData}}, nil
		default:
			return nil, nil
		}
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		switch instanceID {
		case 1:
			return map[string]qbt.TorrentFiles{
				"a": {{Name: "one.mkv", Size: 1}},
			}, nil
		case 2:
			return map[string]qbt.TorrentFiles{}, nil
		default:
			return map[string]qbt.TorrentFiles{}, nil
		}
	}

	result, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err != nil {
		t.Fatalf("buildFileMap: %v", err)
	}

	if !result.fileMap.Has(normalizePath(filepath.Join(stableRoot, "one.mkv"))) {
		t.Fatalf("expected instance 1 file to be protected")
	}
	if !slices.Equal(result.scanRoots, []string{filepath.Clean(stableRoot)}) {
		t.Fatalf("scanRoots mismatch: got=%v want=%v", result.scanRoots, []string{filepath.Clean(stableRoot)})
	}
	if !slices.Equal(result.skippedRoots, []string{filepath.Clean(skippedRoot)}) {
		t.Fatalf("skippedRoots mismatch: got=%v want=%v", result.skippedRoots, []string{filepath.Clean(skippedRoot)})
	}
}

func TestBuildFileMap_DropsScanRootsCoveredByOverlappingSkippedRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skippedRoot := filepath.Join(root, "partial")
	stableRoot := filepath.Join(skippedRoot, "complete")

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: stableRoot, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: skippedRoot, State: qbt.TorrentStateCheckingResumeData}}, nil
		default:
			return nil, nil
		}
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		switch instanceID {
		case 1:
			return map[string]qbt.TorrentFiles{
				"a": {{Name: "one.mkv", Size: 1}},
			}, nil
		case 2:
			return map[string]qbt.TorrentFiles{}, nil
		default:
			return map[string]qbt.TorrentFiles{}, nil
		}
	}

	result, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err != nil {
		t.Fatalf("buildFileMap: %v", err)
	}

	if !result.fileMap.Has(normalizePath(filepath.Join(stableRoot, "one.mkv"))) {
		t.Fatalf("expected instance 1 file to be protected")
	}
	if len(result.scanRoots) != 0 {
		t.Fatalf("expected scanRoots to be empty, got=%v", result.scanRoots)
	}
	if !slices.Equal(result.skippedRoots, []string{filepath.Clean(skippedRoot)}) {
		t.Fatalf("skippedRoots mismatch: got=%v want=%v", result.skippedRoots, []string{filepath.Clean(skippedRoot)})
	}
}

func TestBuildFileMap_StaleNonOverlappingRootsDoNotBypassSafety(t *testing.T) {
	t.Parallel()

	rootA := t.TempDir()
	rootB := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	now := time.Now()
	lastSync := now.Add(-10 * time.Second)

	offlineErr := errors.New("offline")

	stubSync(svc).getClient = func(_ context.Context, instanceID int) (healthChecker, error) {
		if instanceID == 2 {
			return nil, offlineErr
		}
		return stubHealthChecker{
			healthy:  true,
			lastSync: lastSync,
		}, nil
	}

	stubSync(svc).getLastCompletedRun = func(_ context.Context, instanceID int) (*models.OrphanScanRun, error) {
		if instanceID != 2 {
			return nil, nil
		}
		return &models.OrphanScanRun{InstanceID: 2, ScanPaths: []string{rootB}}, nil
	}

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		if instanceID == 1 {
			return []qbt.Torrent{{Hash: "A", SavePath: rootA, State: qbt.TorrentStatePausedUp}}, nil
		}
		return nil, nil
	}

	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"a": {{Name: "one.mkv", Size: 1}},
		}, nil
	}

	_, err := svc.buildFileMap(context.Background(), localInstance(1), newTestBackend(), scanScope{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, offlineErr) {
		t.Fatalf("expected offline error, got %v", err)
	}
}
