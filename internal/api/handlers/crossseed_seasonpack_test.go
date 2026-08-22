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
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func newTestSeasonPackRunStore(t *testing.T) *models.SeasonPackRunStore {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "crossseed-seasonpack")
	return models.NewSeasonPackRunStore(db)
}

func TestSeasonPackHandlers_RejectBadPayloads(t *testing.T) {
	handler := &CrossSeedHandler{service: nil}

	tests := []struct {
		name   string
		path   string
		body   string
		invoke func(*CrossSeedHandler, http.ResponseWriter, *http.Request)
	}{
		{
			name:   "season pack check",
			path:   "/api/cross-seed/season-pack/check",
			body:   `{bad json`,
			invoke: (*CrossSeedHandler).SeasonPackCheck,
		},
		{
			name:   "season pack apply",
			path:   "/api/cross-seed/season-pack/apply",
			body:   `not json`,
			invoke: (*CrossSeedHandler).SeasonPackApply,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			tt.invoke(handler, resp, req)

			require.Equal(t, http.StatusBadRequest, resp.Code)
			require.Contains(t, resp.Body.String(), "Invalid request body")
		})
	}
}

func TestListSeasonPackRuns(t *testing.T) {
	store := newTestSeasonPackRunStore(t)
	handler := &CrossSeedHandler{seasonPackRunStore: store}

	ctx := t.Context()
	createdNames := []string{
		"Pack.S01.720p",
		"Pack.S02.1080p",
		"Pack.S03.2160p",
	}
	for _, name := range createdNames {
		_, err := store.Create(ctx, &models.SeasonPackRun{
			TorrentName: name,
			Phase:       "check",
			Status:      "not_ready",
			Reason:      "below_threshold",
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		path      string
		wantNames []string
	}{
		{
			name:      "default limit",
			path:      "/api/cross-seed/season-pack/runs",
			wantNames: []string{"Pack.S03.2160p", "Pack.S02.1080p", "Pack.S01.720p"},
		},
		{
			name:      "explicit limit",
			path:      "/api/cross-seed/season-pack/runs?limit=2",
			wantNames: []string{"Pack.S03.2160p", "Pack.S02.1080p"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.path, nil)
			resp := httptest.NewRecorder()

			handler.ListSeasonPackRuns(resp, req)

			require.Equal(t, http.StatusOK, resp.Code)

			var runs []*models.SeasonPackRun
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
			require.Len(t, runs, len(tt.wantNames))
			for i, wantName := range tt.wantNames {
				require.Equal(t, wantName, runs[i].TorrentName)
			}
		})
	}
}

func TestListSeasonPackRuns_ReturnsEmptyArrayWhenNoRuns(t *testing.T) {
	store := newTestSeasonPackRunStore(t)
	handler := &CrossSeedHandler{seasonPackRunStore: store}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/cross-seed/season-pack/runs", nil)
	resp := httptest.NewRecorder()

	handler.ListSeasonPackRuns(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.JSONEq(t, `[]`, resp.Body.String())
}

func TestListSeasonPackRuns_Returns503WhenStoreMissing(t *testing.T) {
	handler := &CrossSeedHandler{}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/cross-seed/season-pack/runs", nil)
	resp := httptest.NewRecorder()

	handler.ListSeasonPackRuns(resp, req)

	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
	require.Contains(t, resp.Body.String(), "Season pack run store not configured")
}

func TestPatchAutomationSettings_RejectsInvalidSeasonPackThreshold(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "threshold zero",
			payload: `{"seasonPackCoverageThreshold": 0}`,
		},
		{
			name:    "threshold negative",
			payload: `{"seasonPackCoverageThreshold": -0.5}`,
		},
		{
			name:    "threshold above 1",
			payload: `{"seasonPackCoverageThreshold": 1.5}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &CrossSeedHandler{service: nil}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/cross-seed/settings", strings.NewReader(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			handler.PatchAutomationSettings(resp, req)

			require.Equal(t, http.StatusBadRequest, resp.Code)
			require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
		})
	}
}

func TestPatchAutomationSettings_AppliesSeasonPackFields(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		SeasonPackEnabled:            false,
		SeasonPackCoverageThreshold:  0.75,
		SeasonPackSkipRepackCompare:  true,
		SeasonPackSimplifyHDRCompare: false,
		SeasonPackSimplifyWEBCompare: false,
		SeasonPackSkipYearCompare:    false,
		SeasonPackTVDBAPIKey:         "keep-key",
		SeasonPackTVDBPIN:            "keep-pin",
	}

	threshold := 0.9
	// Test fixtures exercise trimming behavior; these are not real credentials.
	tvdbCredential := "  tvdb value  "
	subscriberCredential := "  subscriber value  "
	patch := automationSettingsPatchRequest{
		SeasonPackEnabled:            new(true),
		SeasonPackCoverageThreshold:  &threshold,
		SeasonPackSkipRepackCompare:  new(false),
		SeasonPackSimplifyHDRCompare: new(true),
		SeasonPackSimplifyWEBCompare: new(true),
		SeasonPackSkipYearCompare:    new(true),
		SeasonPackTVDBAPIKey:         &tvdbCredential,
		SeasonPackTVDBPIN:            &subscriberCredential,
	}

	applyAutomationSettingsPatch(&existing, patch)

	require.True(t, existing.SeasonPackEnabled)
	require.InDelta(t, 0.9, existing.SeasonPackCoverageThreshold, 0.001)
	require.False(t, existing.SeasonPackSkipRepackCompare)
	require.True(t, existing.SeasonPackSimplifyHDRCompare)
	require.True(t, existing.SeasonPackSimplifyWEBCompare)
	require.True(t, existing.SeasonPackSkipYearCompare)
	require.Equal(t, "tvdb value", existing.SeasonPackTVDBAPIKey)
	require.Equal(t, "subscriber value", existing.SeasonPackTVDBPIN)
}

func TestPatchAutomationSettings_IsEmptyIncludesSeasonPackFields(t *testing.T) {
	require.True(t, automationSettingsPatchRequest{}.isEmpty())

	tests := []struct {
		name  string
		patch automationSettingsPatchRequest
	}{
		{
			name:  "season pack enabled",
			patch: automationSettingsPatchRequest{SeasonPackEnabled: new(true)},
		},
		{
			name:  "season pack threshold",
			patch: automationSettingsPatchRequest{SeasonPackCoverageThreshold: func() *float64 { v := 0.8; return &v }()},
		},
		{
			name:  "season pack tags",
			patch: automationSettingsPatchRequest{SeasonPackTags: func() *[]string { v := []string{"season-pack", "cross-seed"}; return &v }()},
		},
		{
			name:  "season pack tvdb api key",
			patch: automationSettingsPatchRequest{SeasonPackTVDBAPIKey: func() *string { v := " tvdb value "; return &v }()},
		},
		{
			name:  "season pack tvdb pin",
			patch: automationSettingsPatchRequest{SeasonPackTVDBPIN: func() *string { v := " subscriber value "; return &v }()},
		},
		{
			name:  "season pack skip repack compare",
			patch: automationSettingsPatchRequest{SeasonPackSkipRepackCompare: new(true)},
		},
		{
			name:  "season pack simplify hdr compare",
			patch: automationSettingsPatchRequest{SeasonPackSimplifyHDRCompare: new(true)},
		},
		{
			name:  "season pack simplify web compare",
			patch: automationSettingsPatchRequest{SeasonPackSimplifyWEBCompare: new(true)},
		},
		{
			name:  "season pack skip year compare",
			patch: automationSettingsPatchRequest{SeasonPackSkipYearCompare: new(true)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, tt.patch.isEmpty())
		})
	}
}

func TestNormalizeSeasonPackCategoryRules(t *testing.T) {
	tests := []struct {
		name string
		in   []models.SeasonPackCategoryRule
		want []models.SeasonPackCategoryRule
	}{
		{
			name: "trims, lowercases resolution, uppercases source",
			in:   []models.SeasonPackCategoryRule{{Resolution: " 2160P ", Source: " bluray ", Category: " tv-uhd "}},
			want: []models.SeasonPackCategoryRule{{Resolution: "2160p", Source: "BLURAY", Category: "tv-uhd"}},
		},
		{
			name: "empty source means any and is kept",
			in:   []models.SeasonPackCategoryRule{{Resolution: "1080p", Source: "", Category: "tv-hd"}},
			want: []models.SeasonPackCategoryRule{{Resolution: "1080p", Source: "", Category: "tv-hd"}},
		},
		{
			name: "unrecognized source drops the rule instead of widening to any",
			in:   []models.SeasonPackCategoryRule{{Resolution: "1080p", Source: "DVDRIP", Category: "tv-hd"}},
			want: []models.SeasonPackCategoryRule{},
		},
		{
			name: "drops rules missing resolution or category",
			in: []models.SeasonPackCategoryRule{
				{Resolution: "", Source: "WEB", Category: "tv-hd"},
				{Resolution: "1080p", Source: "WEB", Category: ""},
			},
			want: []models.SeasonPackCategoryRule{},
		},
		{
			name: "dedupes on resolution and source keeping the first",
			in: []models.SeasonPackCategoryRule{
				{Resolution: "1080p", Source: "WEB", Category: "first"},
				{Resolution: "1080p", Source: "WEB", Category: "second"},
			},
			want: []models.SeasonPackCategoryRule{{Resolution: "1080p", Source: "WEB", Category: "first"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeSeasonPackCategoryRules(tt.in))
		})
	}
}
