// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
)

func newTestCrossSeedStore(t *testing.T) *models.CrossSeedStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "crossseed.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	return store
}

// setServiceField injects an unexported crossseed.Service field via reflect/unsafe.
// Tests use this to isolate handler behavior without constructing every service dependency.
func setServiceField[T any](t *testing.T, svc *crossseed.Service, name string, value T) {
	t.Helper()

	field := reflect.ValueOf(svc).Elem().FieldByName(name)
	require.True(t, field.IsValid(), "missing field %q", name)

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func TestAutomationSettingsSeasonPackRequests(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		assert     func(*testing.T, *CrossSeedHandler, *models.CrossSeedStore, *httptest.ResponseRecorder)
	}{
		{
			name:   "put persists season pack fields",
			method: http.MethodPut,
			body: `{
				"seasonPackEnabled": true,
				"seasonPackCoverageThreshold": 0.9,
				"seasonPackTags": ["season-pack", "cross-seed"],
				"seasonPackCategory": " tv-hd ",
				"seasonPackSkipRepackCompare": false,
				"seasonPackSimplifyHdrCompare": true,
				"seasonPackSimplifyWebCompare": true,
				"seasonPackSkipYearCompare": true
			}`,
			wantStatus: http.StatusOK,
			assert: func(t *testing.T, _ *CrossSeedHandler, store *models.CrossSeedStore, resp *httptest.ResponseRecorder) {
				t.Helper()

				var updated models.CrossSeedAutomationSettings
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
				require.True(t, updated.SeasonPackEnabled)
				require.InDelta(t, 0.9, updated.SeasonPackCoverageThreshold, 0.001)
				require.Equal(t, []string{"season-pack", "cross-seed"}, updated.SeasonPackTags)
				require.Equal(t, "tv-hd", updated.SeasonPackCategory)
				require.False(t, updated.SeasonPackSkipRepackCompare)
				require.True(t, updated.SeasonPackSimplifyHDRCompare)
				require.True(t, updated.SeasonPackSimplifyWEBCompare)
				require.True(t, updated.SeasonPackSkipYearCompare)

				stored, err := store.GetSettings(t.Context())
				require.NoError(t, err)
				require.True(t, stored.SeasonPackEnabled)
				require.InDelta(t, 0.9, stored.SeasonPackCoverageThreshold, 0.001)
				require.Equal(t, []string{"season-pack", "cross-seed"}, stored.SeasonPackTags)
				require.Equal(t, "tv-hd", stored.SeasonPackCategory)
				require.False(t, stored.SeasonPackSkipRepackCompare)
				require.True(t, stored.SeasonPackSimplifyHDRCompare)
				require.True(t, stored.SeasonPackSimplifyWEBCompare)
				require.True(t, stored.SeasonPackSkipYearCompare)
			},
		},
		{
			name:       "patch persists season pack category",
			method:     http.MethodPatch,
			body:       `{"seasonPackCategory": " tv-uhd "}`,
			wantStatus: http.StatusOK,
			assert: func(t *testing.T, handler *CrossSeedHandler, _ *models.CrossSeedStore, _ *httptest.ResponseRecorder) {
				t.Helper()

				getReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/cross-seed/settings", nil)
				getResp := httptest.NewRecorder()

				handler.GetAutomationSettings(getResp, getReq)

				require.Equal(t, http.StatusOK, getResp.Code)

				var stored models.CrossSeedAutomationSettings
				require.NoError(t, json.NewDecoder(getResp.Body).Decode(&stored))
				require.Equal(t, "tv-uhd", stored.SeasonPackCategory)
			},
		},
		{
			name:       "put rejects invalid season pack threshold",
			method:     http.MethodPut,
			body:       `{"seasonPackCoverageThreshold": 0}`,
			wantStatus: http.StatusBadRequest,
			assert: func(t *testing.T, _ *CrossSeedHandler, _ *models.CrossSeedStore, resp *httptest.ResponseRecorder) {
				t.Helper()

				require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestCrossSeedStore(t)

			svc := &crossseed.Service{}
			setServiceField(t, svc, "automationStore", store)

			handler := &CrossSeedHandler{service: svc}

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/api/cross-seed/settings", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			switch tt.method {
			case http.MethodPatch:
				handler.PatchAutomationSettings(resp, req)
			default:
				handler.UpdateAutomationSettings(resp, req)
			}

			require.Equal(t, tt.wantStatus, resp.Code)
			tt.assert(t, handler, store, resp)
		})
	}
}
