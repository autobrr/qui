// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func newTestSeasonPackRunStore(t *testing.T) *models.SeasonPackRunStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return models.NewSeasonPackRunStore(db)
}

func TestSeasonPackCheck_Returns400ForBadPayload(t *testing.T) {
	handler := &CrossSeedHandler{service: nil}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/cross-seed/season-pack/check", strings.NewReader(`{bad json`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.SeasonPackCheck(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "Invalid request body")
}

func TestSeasonPackApply_Returns400ForBadPayload(t *testing.T) {
	handler := &CrossSeedHandler{service: nil}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/cross-seed/season-pack/apply", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.SeasonPackApply(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "Invalid request body")
}

func TestListSeasonPackRuns_ReturnsRecentActivity(t *testing.T) {
	store := newTestSeasonPackRunStore(t)
	handler := &CrossSeedHandler{seasonPackRunStore: store}

	ctx := t.Context()
	for _, name := range []string{"Pack.S01.720p", "Pack.S02.1080p"} {
		_, err := store.Create(ctx, &models.SeasonPackRun{
			TorrentName: name,
			Phase:       "check",
			Status:      "not_ready",
			Reason:      "below_threshold",
		})
		require.NoError(t, err)
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/cross-seed/season-pack/runs", nil)
	resp := httptest.NewRecorder()

	handler.ListSeasonPackRuns(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)

	var runs []*models.SeasonPackRun
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 2)

	// Collect names; both should be present (order by created_at DESC, same timestamp = ID order)
	names := map[string]bool{runs[0].TorrentName: true, runs[1].TorrentName: true}
	require.True(t, names["Pack.S01.720p"])
	require.True(t, names["Pack.S02.1080p"])
}

func TestListSeasonPackRuns_RespectsLimit(t *testing.T) {
	store := newTestSeasonPackRunStore(t)
	handler := &CrossSeedHandler{seasonPackRunStore: store}

	ctx := t.Context()
	for i := range 5 {
		_, err := store.Create(ctx, &models.SeasonPackRun{
			TorrentName: "Pack.S0" + string(rune('1'+i)) + ".720p",
			Phase:       "check",
			Status:      "not_ready",
		})
		require.NoError(t, err)
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/cross-seed/season-pack/runs?limit=2", nil)
	resp := httptest.NewRecorder()

	handler.ListSeasonPackRuns(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)

	var runs []*models.SeasonPackRun
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&runs))
	require.Len(t, runs, 2)
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
		SeasonPackEnabled:           false,
		SeasonPackCoverageThreshold: 0.75,
	}

	threshold := 0.9
	patch := automationSettingsPatchRequest{
		SeasonPackEnabled:           new(true),
		SeasonPackCoverageThreshold: &threshold,
	}

	applyAutomationSettingsPatch(&existing, patch)

	require.True(t, existing.SeasonPackEnabled)
	require.InDelta(t, 0.9, existing.SeasonPackCoverageThreshold, 0.001)
}

func TestPatchAutomationSettings_IsEmptyIncludesSeasonPackFields(t *testing.T) {
	// All-nil patch should be empty
	patch := automationSettingsPatchRequest{}
	require.True(t, patch.isEmpty())

	// Setting season pack enabled should make it non-empty
	patch.SeasonPackEnabled = new(true)
	require.False(t, patch.isEmpty())

	// Reset and test threshold
	patch = automationSettingsPatchRequest{}
	threshold := 0.8
	patch.SeasonPackCoverageThreshold = &threshold
	require.False(t, patch.isEmpty())

	// Reset and test tags-only patch
	patch = automationSettingsPatchRequest{}
	tags := []string{"season-pack", "cross-seed"}
	patch.SeasonPackTags = &tags
	require.False(t, patch.isEmpty())
}
