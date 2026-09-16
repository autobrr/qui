// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestOrphanScanSettings_CategoryPathsRequireDefaultSavePath(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphan-scan-settings")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	handler := NewOrphanScanHandler(store, instances, nil)
	router := chi.NewRouter()
	router.Put("/api/instances/{instanceID}/orphan-scan/settings", handler.UpdateSettings)
	url := fmt.Sprintf("/api/instances/%d/orphan-scan/settings", instance.ID)

	for _, step := range []struct {
		name string
		body string
		want bool
	}{
		{"enable both", `{"scanDefaultSavePath":true,"scanCategoryPaths":true}`, true},
		{"disable default only", `{"scanDefaultSavePath":false}`, false},
		{"enable categories only", `{"scanCategoryPaths":true}`, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), http.MethodPut, url, strings.NewReader(step.body)))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			var updated models.OrphanScanSettings
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &updated))
			require.Equal(t, step.want, updated.ScanCategoryPaths)
			stored, err := store.GetSettings(t.Context(), instance.ID)
			require.NoError(t, err)
			require.Equal(t, step.want, stored.ScanCategoryPaths)
		})
	}
}
