// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func newTestCrossSeedStore(t *testing.T) *models.CrossSeedStore {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "crossseed-put")

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	return store
}

func newTestCrossSeedHandler(t *testing.T) (*CrossSeedHandler, *models.CrossSeedStore) {
	t.Helper()

	store := newTestCrossSeedStore(t)
	svc := crossseed.NewServiceWithAutomationStore(store)
	return &CrossSeedHandler{service: svc}, store
}

func TestAutomationSettingsTitleRescuePatch(t *testing.T) {
	handler, store := newTestCrossSeedHandler(t)

	// Enable then disable in order: the false patch must count as a real
	// update, not an empty request.
	steps := []struct {
		name string
		body string
		want bool
	}{
		{name: "enable", body: `{"rescueTitleMismatches":true}`, want: true},
		{name: "disable", body: `{"rescueTitleMismatches":false}`, want: false},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/cross-seed/settings", strings.NewReader(step.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			handler.PatchAutomationSettings(resp, req)

			require.Equal(t, http.StatusOK, resp.Code)
			var updated models.CrossSeedAutomationSettings
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
			require.Equal(t, step.want, updated.RescueTitleMismatches)

			stored, err := store.GetSettings(t.Context())
			require.NoError(t, err)
			require.Equal(t, step.want, stored.RescueTitleMismatches)
		})
	}
}

func TestAutomationSettingsSeasonPackRequests(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		setup      func(*testing.T, *CrossSeedHandler)
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
			setup: func(t *testing.T, handler *CrossSeedHandler) {
				t.Helper()

				req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/cross-seed/settings", strings.NewReader(`{
					"seasonPackEnabled": true,
					"seasonPackCoverageThreshold": 0.8,
					"seasonPackTags": ["season-pack", "sonarr"],
					"seasonPackSkipRepackCompare": false,
					"seasonPackSimplifyHdrCompare": true,
					"seasonPackSimplifyWebCompare": true,
					"seasonPackSkipYearCompare": true
				}`))
				req.Header.Set("Content-Type", "application/json")
				resp := httptest.NewRecorder()

				handler.UpdateAutomationSettings(resp, req)

				require.Equal(t, http.StatusOK, resp.Code)
			},
			assert: func(t *testing.T, handler *CrossSeedHandler, _ *models.CrossSeedStore, _ *httptest.ResponseRecorder) {
				t.Helper()

				getReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/cross-seed/settings", nil)
				getResp := httptest.NewRecorder()

				handler.GetAutomationSettings(getResp, getReq)

				require.Equal(t, http.StatusOK, getResp.Code)

				var stored models.CrossSeedAutomationSettings
				require.NoError(t, json.NewDecoder(getResp.Body).Decode(&stored))
				require.Equal(t, "tv-uhd", stored.SeasonPackCategory)
				require.True(t, stored.SeasonPackEnabled)
				require.InDelta(t, 0.8, stored.SeasonPackCoverageThreshold, 0.001)
				require.Equal(t, []string{"season-pack", "sonarr"}, stored.SeasonPackTags)
			},
		},
		{
			name:       "put rejects zero season pack threshold",
			method:     http.MethodPut,
			body:       `{"seasonPackCoverageThreshold": 0}`,
			wantStatus: http.StatusBadRequest,
			assert: func(t *testing.T, _ *CrossSeedHandler, _ *models.CrossSeedStore, resp *httptest.ResponseRecorder) {
				t.Helper()

				require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
			},
		},
		{
			name:       "put rejects negative season pack threshold",
			method:     http.MethodPut,
			body:       `{"seasonPackCoverageThreshold": -0.1}`,
			wantStatus: http.StatusBadRequest,
			assert: func(t *testing.T, _ *CrossSeedHandler, _ *models.CrossSeedStore, resp *httptest.ResponseRecorder) {
				t.Helper()

				require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
			},
		},
		{
			name:       "put rejects season pack threshold above one",
			method:     http.MethodPut,
			body:       `{"seasonPackCoverageThreshold": 1.5}`,
			wantStatus: http.StatusBadRequest,
			assert: func(t *testing.T, _ *CrossSeedHandler, _ *models.CrossSeedStore, resp *httptest.ResponseRecorder) {
				t.Helper()

				require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
			},
		},
		{
			name:       "put rejects omitted season pack threshold",
			method:     http.MethodPut,
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			assert: func(t *testing.T, _ *CrossSeedHandler, _ *models.CrossSeedStore, resp *httptest.ResponseRecorder) {
				t.Helper()

				require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store := newTestCrossSeedHandler(t)
			if tt.setup != nil {
				tt.setup(t, handler)
			}

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

func TestAutomationSettingsAutoResumeBudget(t *testing.T) {
	putBody := func(extra string) string {
		if extra == "" {
			return `{"seasonPackCoverageThreshold": 0.75}`
		}
		return `{"seasonPackCoverageThreshold": 0.75, ` + extra + `}`
	}

	tests := []struct {
		name       string
		method     string
		body       string
		setup      string // optional PUT body applied first
		want       int
		wantStatus int // 0 means 200 OK
	}{
		{
			name:   "put without the field keeps the default",
			method: http.MethodPut,
			body:   putBody(""),
			want:   models.DefaultAutoResumeMaxDownloadMB,
		},
		{
			name:   "put with explicit zero stores zero",
			method: http.MethodPut,
			body:   putBody(`"autoResumeMaxDownloadMb": 0`),
			want:   0,
		},
		{
			name:   "put with custom value stores it",
			method: http.MethodPut,
			body:   putBody(`"autoResumeMaxDownloadMb": 200`),
			want:   200,
		},
		{
			name:       "put with negative value is rejected",
			method:     http.MethodPut,
			body:       putBody(`"autoResumeMaxDownloadMb": -5`),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "patch with negative value is rejected",
			method:     http.MethodPatch,
			body:       `{"autoResumeMaxDownloadMb": -1}`,
			setup:      putBody(`"autoResumeMaxDownloadMb": 200`),
			want:       200,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "patch without the field preserves the stored value",
			method: http.MethodPatch,
			body:   `{"findIndividualEpisodes": true}`,
			setup:  putBody(`"autoResumeMaxDownloadMb": 200`),
			want:   200,
		},
		{
			name:   "patch with explicit zero stores zero",
			method: http.MethodPatch,
			body:   `{"autoResumeMaxDownloadMb": 0}`,
			setup:  putBody(`"autoResumeMaxDownloadMb": 200`),
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store := newTestCrossSeedHandler(t)

			if tt.setup != "" {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/cross-seed/settings", strings.NewReader(tt.setup))
				req.Header.Set("Content-Type", "application/json")
				resp := httptest.NewRecorder()
				handler.UpdateAutomationSettings(resp, req)
				require.Equal(t, http.StatusOK, resp.Code)
			}

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/api/cross-seed/settings", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			switch tt.method {
			case http.MethodPatch:
				handler.PatchAutomationSettings(resp, req)
			default:
				handler.UpdateAutomationSettings(resp, req)
			}
			wantStatus := tt.wantStatus
			if wantStatus == 0 {
				wantStatus = http.StatusOK
			}
			require.Equal(t, wantStatus, resp.Code)

			if tt.wantStatus != 0 && tt.setup == "" {
				return
			}
			stored, err := store.GetSettings(t.Context())
			require.NoError(t, err)
			require.Equal(t, tt.want, stored.AutoResumeMaxDownloadMB)
		})
	}
}

func TestAutomationSettingsPooledPartialCompletion(t *testing.T) {
	tests := []struct {
		name   string
		method string
		setup  string
		body   string
		want   bool
	}{
		{name: "put omitted defaults false", method: http.MethodPut, body: `{"seasonPackCoverageThreshold":0.75}`, want: false},
		{name: "put true", method: http.MethodPut, body: `{"seasonPackCoverageThreshold":0.75,"pooledPartialCompletionEnabled":true}`, want: true},
		{name: "patch true", method: http.MethodPatch, body: `{"pooledPartialCompletionEnabled":true}`, want: true},
		{name: "patch false", method: http.MethodPatch, setup: `{"seasonPackCoverageThreshold":0.75,"pooledPartialCompletionEnabled":true}`, body: `{"pooledPartialCompletionEnabled":false}`, want: false},
		{name: "patch omitted preserves true", method: http.MethodPatch, setup: `{"seasonPackCoverageThreshold":0.75,"pooledPartialCompletionEnabled":true}`, body: `{"findIndividualEpisodes":true}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store := newTestCrossSeedHandler(t)
			if tt.setup != "" {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/cross-seed/settings", strings.NewReader(tt.setup))
				resp := httptest.NewRecorder()
				handler.UpdateAutomationSettings(resp, req)
				require.Equal(t, http.StatusOK, resp.Code)
			}

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/api/cross-seed/settings", strings.NewReader(tt.body))
			resp := httptest.NewRecorder()
			if tt.method == http.MethodPatch {
				handler.PatchAutomationSettings(resp, req)
			} else {
				handler.UpdateAutomationSettings(resp, req)
			}
			require.Equal(t, http.StatusOK, resp.Code)
			stored, err := store.GetSettings(t.Context())
			require.NoError(t, err)
			require.Equal(t, tt.want, stored.PooledPartialCompletionEnabled)
		})
	}
}
