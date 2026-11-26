// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestDirScan_UnknownTargetInstanceIsBadRequest(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "dirscan-unknown-target")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	handler := NewDirScanHandler(nil, instances)
	router := chi.NewRouter()
	router.Post("/api/dir-scan/directories", handler.CreateDirectory)
	router.Patch("/api/dir-scan/directories/{directoryID}", handler.UpdateDirectory)

	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/dir-scan/directories", `{"path":"/data","targetInstanceId":404}`},
		{http.MethodPatch, "/api/dir-scan/directories/1", `{"targetInstanceId":404}`},
	} {
		t.Run(route.method, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), route.method, route.path, strings.NewReader(route.body)))
			require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
			require.JSONEq(t, `{"error":"Target instance not found"}`, resp.Body.String())
		})
	}
}
