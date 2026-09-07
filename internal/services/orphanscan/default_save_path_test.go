// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// data builds an absolute path that is absolute on Windows too, where a bare
// leading separator has no drive and fails filepath.IsAbs.
func data(parts ...string) string {
	return filepath.Join(append([]string{absTestRoot()}, parts...)...)
}

func absTestRoot() string {
	if vol := filepath.VolumeName(mustCwd()); vol != "" {
		return vol + string(filepath.Separator) + "data"
	}
	return string(filepath.Separator) + "data"
}

func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func TestPruneNestedScanRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		roots []string
		want  []string
	}{
		{
			name:  "descendants of the default save path are dropped",
			roots: []string{data("torrents"), data("torrents", "movies"), data("torrents", "tv", "season")},
			want:  []string{data("torrents")},
		},
		{
			name:  "sibling roots are all kept",
			roots: []string{data("torrents"), data("other")},
			want:  []string{data("torrents"), data("other")},
		},
		{
			name:  "a prefix that is not a path boundary is not an ancestor",
			roots: []string{data("torrents"), data("torrents-old")},
			want:  []string{data("torrents"), data("torrents-old")},
		},
		{
			name:  "duplicate spellings never prune each other away",
			roots: []string{data("Torrents"), data("torrents")},
			want:  []string{data("Torrents"), data("torrents")},
		},
		{
			name:  "empty input",
			roots: nil,
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := pruneNestedScanRoots(tt.roots)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("pruneNestedScanRoots(%v) = %v, want %v", tt.roots, got, tt.want)
			}
		})
	}
}

func TestValidDefaultSavePath(t *testing.T) {
	t.Parallel()

	absRoot := data("torrents")

	tests := []struct {
		name     string
		savePath string
		want     string
		wantErr  bool
	}{
		{name: "absolute path is used as a scan root", savePath: absRoot, want: absRoot},
		{name: "trailing separator is cleaned", savePath: absRoot + string(filepath.Separator), want: absRoot},
		{name: "surrounding whitespace is trimmed", savePath: "  " + absRoot + "  ", want: absRoot},
		{name: "empty save path is rejected", savePath: "", wantErr: true},
		{name: "relative save path is rejected", savePath: filepath.Join("relative", "path"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := validDefaultSavePath(tt.savePath)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got root %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validDefaultSavePath: %v", err)
			}
			if got != tt.want {
				t.Fatalf("validDefaultSavePath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeclaredScanRoots_FailsWhenPreferencesAreUnreachable(t *testing.T) {
	t.Parallel()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
	svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{}, errors.New("boom")
	}

	if _, _, err := svc.declaredScanRoots(context.Background(), 1, scanScope{DefaultSavePath: true}); err == nil {
		t.Fatal("expected an error when qBittorrent preferences cannot be read")
	}
}

// newDefaultSavePathService wires a single local instance whose only torrent
// lives in a subdirectory of the qBittorrent default save path.
func newDefaultSavePathService(defaultSavePath, torrentSavePath string) *Service {
	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)

	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-10 * time.Second)}, nil
	}
	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "one", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	svc.getAllTorrentsProvider = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "A", SavePath: torrentSavePath, State: qbt.TorrentStatePausedUp}}, nil
	}
	svc.getTorrentFilesBatchProvider = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"a": {{Name: "one.mkv", Size: 1}}}, nil
	}
	svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: defaultSavePath}, nil
	}
	return svc
}

func TestBuildFileMap_DefaultSavePathRootIsOptIn(t *testing.T) {
	t.Parallel()

	defaultSavePath := t.TempDir()
	torrentSavePath := filepath.Join(defaultSavePath, "mydata")

	svc := newDefaultSavePathService(defaultSavePath, torrentSavePath)

	off, err := svc.buildFileMap(context.Background(), 1, newTestBackend(), scanScope{})
	if err != nil {
		t.Fatalf("buildFileMap (toggle off): %v", err)
	}
	if slices.Contains(off.scanRoots, defaultSavePath) {
		t.Fatalf("default save path %q must not be scanned while the toggle is off: %v", defaultSavePath, off.scanRoots)
	}

	on, err := svc.buildFileMap(context.Background(), 1, newTestBackend(), scanScope{DefaultSavePath: true})
	if err != nil {
		t.Fatalf("buildFileMap (toggle on): %v", err)
	}
	if !slices.Contains(on.scanRoots, defaultSavePath) {
		t.Fatalf("default save path %q missing from scan roots: %v", defaultSavePath, on.scanRoots)
	}
	if !slices.Contains(on.scanRoots, torrentSavePath) {
		t.Fatalf("torrent-derived root %q must survive for deletion boundaries: %v", torrentSavePath, on.scanRoots)
	}

	// The nested torrent root is already covered, so only one tree is walked.
	walkRoots := pruneNestedScanRoots(on.scanRoots)
	if !slices.Equal(walkRoots, []string{defaultSavePath}) {
		t.Fatalf("walk roots = %v, want only %q", walkRoots, defaultSavePath)
	}
}

func TestBuildFileMap_DefaultSavePathFailureFailsTheScan(t *testing.T) {
	t.Parallel()

	svc := newDefaultSavePathService("", t.TempDir())

	// An unresolvable default save path must not degrade into a narrower scan
	// that reports clean; discussion #2365.
	if _, err := svc.buildFileMap(context.Background(), 1, newTestBackend(), scanScope{DefaultSavePath: true}); err == nil {
		t.Fatal("expected buildFileMap to fail when the default save path cannot be resolved")
	}
}

func TestBuildFileMap_DefaultSavePathProtectsOverlappingInstance(t *testing.T) {
	t.Parallel()

	defaultSavePath := t.TempDir()
	// Instance 1 has no torrents anywhere near the second instance's tree, so
	// without the declared root the two never overlap and instance 2's files
	// would be reported as orphans.
	ownSavePath := t.TempDir()
	otherSavePath := filepath.Join(defaultSavePath, "second-instance")

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-10 * time.Second)}, nil
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
			return []qbt.Torrent{{Hash: "A", SavePath: ownSavePath, State: qbt.TorrentStatePausedUp}}, nil
		case 2:
			return []qbt.Torrent{{Hash: "B", SavePath: otherSavePath, State: qbt.TorrentStatePausedUp}}, nil
		default:
			return nil, nil
		}
	}
	svc.getTorrentFilesBatchProvider = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		switch instanceID {
		case 1:
			return map[string]qbt.TorrentFiles{"a": {{Name: "one.mkv", Size: 1}}}, nil
		case 2:
			return map[string]qbt.TorrentFiles{"b": {{Name: "two.mkv", Size: 1}}}, nil
		default:
			return map[string]qbt.TorrentFiles{}, nil
		}
	}
	svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: defaultSavePath}, nil
	}

	result, err := svc.buildFileMap(context.Background(), 1, newTestBackend(), scanScope{DefaultSavePath: true})
	if err != nil {
		t.Fatalf("buildFileMap: %v", err)
	}

	protected := filepath.Join(otherSavePath, "two.mkv")
	if !result.fileMap.Has(normalizePath(protected)) {
		t.Fatalf("file %q seeded by the other local instance is not protected inside the default save path", protected)
	}
}
