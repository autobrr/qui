// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	quiqbt "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// newDiskRouter wires the real handler and the local backend to a synthetic
// qBittorrent whose default save path is savePath, and counts the reads of it.
func newDiskRouter(t *testing.T, name, savePath string, localAccess bool) (*chi.Mux, int, <-chan struct{}) {
	t.Helper()

	preferences, err := json.Marshal(map[string]string{"save_path": savePath})
	require.NoError(t, err)
	reads := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.4"))
		case "/api/v2/app/preferences":
			reads <- struct{}{}
			_, _ = w.Write(preferences)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, name)
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), name, server.URL, "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	clientPool, err := quiqbt.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	handler := NewInstancesHandler(instanceStore, nil, nil, clientPool, quiqbt.NewSyncManager(clientPool, nil), nil, nil, nil)
	handler.SetFilesystem(fsops.NewPool(instanceStore, local.NewBackend()))
	t.Cleanup(handler.connectionTests.Wait)
	router := chi.NewRouter()
	router.Get("/api/instances/{instanceID}/disk", handler.GetDisk)

	return router, instance.ID, reads
}

// regression: #4305
func TestGetDiskHandler(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tests := []struct {
		name        string
		savePath    string
		localAccess bool
		wantDisk    bool
		wantReads   int
	}{
		{name: "local access reads the disk of the default save path", savePath: dir, localAccess: true, wantDisk: true, wantReads: 1},
		{name: "no file access reads nothing", savePath: dir, localAccess: false, wantReads: 0},
		{name: "a save path that qui does not see has no disk", savePath: filepath.Join(dir, "missing"), localAccess: true, wantReads: 1},
		{name: "an empty save path does not read a folder of qui", savePath: "", localAccess: true, wantReads: 1},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router, instanceID, reads := newDiskRouter(t, fmt.Sprintf("disk-%d", i), tt.savePath, tt.localAccess)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/instances/%d/disk", instanceID), nil))

			require.Equal(t, http.StatusOK, rec.Code)
			var got InstanceDiskResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Len(t, reads, tt.wantReads)
			if !tt.wantDisk {
				require.Nil(t, got.FreeBytes)
				require.Nil(t, got.TotalBytes)
				return
			}
			require.NotNil(t, got.FreeBytes)
			require.NotNil(t, got.TotalBytes)
			require.Positive(t, *got.TotalBytes)
			require.GreaterOrEqual(t, *got.FreeBytes, int64(0))
			require.LessOrEqual(t, *got.FreeBytes, *got.TotalBytes)
		})
	}
}
