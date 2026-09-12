// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	quiqbt "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// newFreeSpaceRouter wires the real handler to a synthetic qBittorrent that answers
// app/getFreeSpaceAtPath with body, and records the path it received.
func newFreeSpaceRouter(t *testing.T, name, apiVersion, body string, status int) (*chi.Mux, int, <-chan string) {
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

	db := testdb.NewMigratedSQLite(t, name)
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), name, server.URL, "", "", nil, nil, false, nil)
	require.NoError(t, err)

	clientPool, err := quiqbt.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	handler := NewInstancesHandler(instanceStore, nil, nil, clientPool, quiqbt.NewSyncManager(clientPool, nil), nil)
	router := chi.NewRouter()
	router.Get("/api/instances/{instanceID}/free-space", handler.GetFreeSpaceAtPath)

	return router, instance.ID, paths
}

func TestGetFreeSpaceAtPathHandler(t *testing.T) {
	const path = "/downloads/complete"

	tests := []struct {
		name       string
		apiVersion string
		body       string
		status     int
		query      string
		wantStatus int
		wantBody   string
	}{
		{name: "positive", apiVersion: "2.15.2", body: "1099511627776", status: http.StatusOK, query: path, wantStatus: http.StatusOK, wantBody: `"bytes":1099511627776`},
		{name: "zero stays zero", apiVersion: "2.15.2", body: "0", status: http.StatusOK, query: path, wantStatus: http.StatusOK, wantBody: `"bytes":0`},
		{name: "negative is unavailable", apiVersion: "2.15.2", body: "-1", status: http.StatusOK, query: path, wantStatus: http.StatusOK, wantBody: `"bytes":null`},
		{name: "failed read is unavailable", apiVersion: "2.15.2", body: "boom", status: http.StatusInternalServerError, query: path, wantStatus: http.StatusOK, wantBody: `"bytes":null`},
		{name: "older instance", apiVersion: "2.15.1", body: "1099511627776", status: http.StatusOK, query: path, wantStatus: http.StatusNotImplemented},
		{name: "missing path", apiVersion: "2.15.2", body: "1099511627776", status: http.StatusOK, query: "  ", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, instanceID, paths := newFreeSpaceRouter(t, tt.name, tt.apiVersion, tt.body, tt.status)

			target := fmt.Sprintf("/api/instances/%d/free-space?path=%s", instanceID, url.QueryEscape(tt.query))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantBody != "" {
				require.Contains(t, rec.Body.String(), tt.wantBody)
				require.Contains(t, rec.Body.String(), fmt.Sprintf(`"path":%q`, path))
			}

			if tt.wantStatus == http.StatusOK {
				select {
				case forwarded := <-paths:
					require.Equal(t, path, forwarded)
				default:
					t.Fatal("qBittorrent never received the path")
				}
			}
		})
	}
}
