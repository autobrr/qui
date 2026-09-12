// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// newQbitPathLibraryRig serves a synthetic qBittorrent that answers
// app/getFreeSpaceAtPath from a path-to-body map.
func newQbitPathLibraryRig(t *testing.T, name string, bodies map[string]string) (*qbittorrent.SyncManager, *models.Instance) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.15.2"))
		case "/api/v2/app/getFreeSpaceAtPath":
			body, ok := bodies[r.URL.Query().Get("path")]
			if !ok {
				body = "-1"
			}
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return newQbitPathInstance(t, name, server.URL)
}

// newQbitPathRig serves a synthetic qBittorrent that answers app/getFreeSpaceAtPath
// with body, and records the path it received.
func newQbitPathRig(t *testing.T, name, apiVersion, body string, status int) (*qbittorrent.SyncManager, *models.Instance, <-chan string) {
	t.Helper()

	paths := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte(apiVersion))
		case "/api/v2/app/getFreeSpaceAtPath":
			paths <- r.URL.Query().Get("path")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	syncManager, instance := newQbitPathInstance(t, name, server.URL)
	return syncManager, instance, paths
}

// newQbitPathInstance stores a synthetic instance pointing at host and returns a
// SyncManager over a real client pool, so capability gating runs as it does live.
func newQbitPathInstance(t *testing.T, name, host string) (*qbittorrent.SyncManager, *models.Instance) {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, name)
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), name, host, "", "", nil, nil, false, nil)
	require.NoError(t, err)

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	return qbittorrent.NewSyncManager(clientPool, nil), instance
}

func TestGetFreeSpaceBytesForQbitPathSource(t *testing.T) {
	// A Windows-style path proves qui forwards the qBittorrent host's own semantics.
	const path = `D:\downloads\complete`

	tests := []struct {
		name       string
		apiVersion string
		body       string
		status     int
		want       int64
		wantErr    bool
	}{
		{name: "positive", apiVersion: "2.15.2", body: "1099511627776", status: http.StatusOK, want: 1099511627776},
		{name: "zero is a real answer", apiVersion: "2.15.2", body: "0", status: http.StatusOK, want: 0},
		{name: "negative is unavailable", apiVersion: "2.15.2", body: "-1", status: http.StatusOK, wantErr: true},
		{name: "server error is unavailable", apiVersion: "2.15.2", body: "boom", status: http.StatusInternalServerError, wantErr: true},
		{name: "older instance is unsupported", apiVersion: "2.15.1", body: "1099511627776", status: http.StatusOK, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncManager, instance, paths := newQbitPathRig(t, tt.name, tt.apiVersion, tt.body, tt.status)

			got, err := GetFreeSpaceBytesForSource(
				t.Context(),
				syncManager,
				instance,
				&models.FreeSpaceSource{Type: models.FreeSpaceSourceQbitPath, Path: path},
				nil, // no filesystem backend: the read happens on the qBittorrent host
			)

			if tt.wantErr {
				require.Error(t, err)
				require.Zero(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)

			select {
			case forwarded := <-paths:
				require.Equal(t, path, forwarded)
			default:
				t.Fatal("qBittorrent never received the path")
			}
		})
	}
}

func TestGetFreeSpaceSourceKeySeparatesQbitPath(t *testing.T) {
	const path = "/downloads"

	local := GetFreeSpaceSourceKey(&models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: path})
	remote := GetFreeSpaceSourceKey(&models.FreeSpaceSource{Type: models.FreeSpaceSourceQbitPath, Path: path})

	require.NotEqual(t, local, remote, "local and qBittorrent-host reads of the same path must not share projection state")
	require.Equal(t, FreeSpaceSourceKeyQBittorrent, GetFreeSpaceSourceKey(&models.FreeSpaceSource{Type: models.FreeSpaceSourceQbitPath}))
}

// TestGetFreeSpaceBytesForSourceReadsEachPath pins that two rules on one instance read
// their own path. Preview and execution share this function, so both see these values.
func TestGetFreeSpaceBytesForSourceReadsEachPath(t *testing.T) {
	const (
		archive = "/mnt/archive"
		staging = "/mnt/staging"
	)

	syncManager, instance := newQbitPathLibraryRig(t, "two-paths", map[string]string{
		archive: "1099511627776",
		staging: "536870912000",
	})

	for path, want := range map[string]int64{archive: 1099511627776, staging: 536870912000} {
		source := &models.FreeSpaceSource{Type: models.FreeSpaceSourceQbitPath, Path: path}

		got, err := GetFreeSpaceBytesForSource(t.Context(), syncManager, instance, source, nil)
		require.NoError(t, err)
		require.Equal(t, want, got, "free space for %s", path)
	}

	require.NotEqual(t,
		GetFreeSpaceSourceKey(&models.FreeSpaceSource{Type: models.FreeSpaceSourceQbitPath, Path: archive}),
		GetFreeSpaceSourceKey(&models.FreeSpaceSource{Type: models.FreeSpaceSourceQbitPath, Path: staging}),
		"two paths must not share projection state",
	)
}
