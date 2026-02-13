// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

type stubHealthChecker struct {
	healthy      bool
	recoveryTime time.Time
	lastSync     time.Time
}

func (s stubHealthChecker) IsHealthy() bool                { return s.healthy }
func (s stubHealthChecker) GetLastRecoveryTime() time.Time { return s.recoveryTime }
func (s stubHealthChecker) GetLastSyncUpdate() time.Time   { return s.lastSync }

func TestGetOtherLocalInstances(t *testing.T) {
	t.Parallel()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil)
	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 3, Name: "inactive", IsActive: false, HasLocalFilesystemAccess: true},
			{ID: 4, Name: "no-local", IsActive: true, HasLocalFilesystemAccess: false},
		}, nil
	}

	got, err := svc.getOtherLocalInstances(context.Background(), 1)
	if err != nil {
		t.Fatalf("getOtherLocalInstances: %v", err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("expected only instance 2, got %+v", got)
	}
}

func TestBuildFileMap_CrossInstance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil)

	now := time.Now()
	recoveryTime := now.Add(-10 * time.Minute)
	lastSync := now.Add(-10 * time.Second)

	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:      true,
			recoveryTime: recoveryTime,
			lastSync:     lastSync,
		}, nil
	}

	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	svc.getAllTorrentsProvider = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		default:
			return nil, nil
		}
	}

	svc.getTorrentFilesBatchProvider = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
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

	svc.markSettledForRecovery(1, recoveryTime)
	svc.markSettledForRecovery(2, recoveryTime)

	result, err := svc.buildFileMap(context.Background(), 1)
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
}

func TestBuildFileMap_BailsWhenOtherLocalInstanceUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil)

	now := time.Now()
	recoveryTime := now.Add(-10 * time.Minute)
	lastSync := now.Add(-10 * time.Second)

	offlineErr := errors.New("offline")

	svc.getClientProvider = func(_ context.Context, instanceID int) (healthChecker, error) {
		if instanceID == 2 {
			return nil, offlineErr
		}
		return stubHealthChecker{
			healthy:      true,
			recoveryTime: recoveryTime,
			lastSync:     lastSync,
		}, nil
	}

	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	svc.getAllTorrentsProvider = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
	}

	svc.getTorrentFilesBatchProvider = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"a": {{Name: "one.mkv", Size: 1}},
		}, nil
	}

	svc.markSettledForRecovery(1, recoveryTime)

	_, err := svc.buildFileMap(context.Background(), 1)
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

	svc := NewService(DefaultConfig(), nil, nil, nil, nil)

	now := time.Now()
	recoveryTime := now.Add(-10 * time.Minute)
	lastSync := now.Add(-10 * time.Second)

	offlineErr := errors.New("offline")

	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:      true,
			recoveryTime: recoveryTime,
			lastSync:     lastSync,
		}, nil
	}

	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	svc.getAllTorrentsProvider = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
		default:
			return nil, nil
		}
	}

	svc.getTorrentFilesBatchProvider = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		if instanceID == 2 {
			return nil, offlineErr
		}
		return map[string]qbt.TorrentFiles{
			"a": {{Name: "one.mkv", Size: 1}},
		}, nil
	}

	svc.markSettledForRecovery(1, recoveryTime)
	svc.markSettledForRecovery(2, recoveryTime)

	_, err := svc.buildFileMap(context.Background(), 1)
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

	svc := NewService(DefaultConfig(), nil, nil, nil, nil)

	now := time.Now()
	recoveryTime := now.Add(-10 * time.Minute)
	lastSync := now.Add(-10 * time.Second)

	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{
			healthy:      true,
			recoveryTime: recoveryTime,
			lastSync:     lastSync,
		}, nil
	}

	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	svc.getAllTorrentsProvider = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		switch instanceID {
		case 1:
			return []qbt.Torrent{{Hash: "A", SavePath: rootA, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: rootB, State: qbt.TorrentStatePausedUp}}, nil
		default:
			return nil, nil
		}
	}

	svc.getTorrentFilesBatchProvider = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
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

	svc.markSettledForRecovery(1, recoveryTime)
	svc.markSettledForRecovery(2, recoveryTime)

	result, err := svc.buildFileMap(context.Background(), 1)
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

func TestBuildFileMap_StaleNonOverlappingRootsDoNotBypassSafety(t *testing.T) {
	t.Parallel()

	rootA := t.TempDir()
	rootB := t.TempDir()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil)

	now := time.Now()
	recoveryTime := now.Add(-10 * time.Minute)
	lastSync := now.Add(-10 * time.Second)

	offlineErr := errors.New("offline")

	svc.getClientProvider = func(_ context.Context, instanceID int) (healthChecker, error) {
		if instanceID == 2 {
			return nil, offlineErr
		}
		return stubHealthChecker{
			healthy:      true,
			recoveryTime: recoveryTime,
			lastSync:     lastSync,
		}, nil
	}

	svc.getLastCompletedRunProvider = func(_ context.Context, instanceID int) (*models.OrphanScanRun, error) {
		if instanceID != 2 {
			return nil, nil
		}
		return &models.OrphanScanRun{InstanceID: 2, ScanPaths: []string{rootB}}, nil
	}

	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "two", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}

	svc.getAllTorrentsProvider = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		if instanceID == 1 {
			return []qbt.Torrent{{Hash: "A", SavePath: rootA, State: qbt.TorrentStatePausedUp}}, nil
		}
		return nil, nil
	}

	svc.getTorrentFilesBatchProvider = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"a": {{Name: "one.mkv", Size: 1}},
		}, nil
	}

	svc.markSettledForRecovery(1, recoveryTime)

	_, err := svc.buildFileMap(context.Background(), 1)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, offlineErr) {
		t.Fatalf("expected offline error, got %v", err)
	}
}
