// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// An export from before the tick boxes went away still carries includedInStats.
func TestTrackerCustomizationCreateIgnoresIncludedInStats(t *testing.T) {
	t.Parallel()

	db := testdb.NewMigratedSQLite(t, "tracker-customizations-handler")
	handler := NewTrackerCustomizationHandler(models.NewTrackerCustomizationStore(db), nil)

	body := `{"displayName":"Tracker","domains":["a.example.invalid","b.example.invalid"],"includedInStats":["b.example.invalid"]}`
	rec := httptest.NewRecorder()
	handler.Create(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/tracker-customizations", strings.NewReader(body)))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "includedInStats")

	list := httptest.NewRecorder()
	handler.List(list, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/tracker-customizations", nil))
	require.Equal(t, http.StatusOK, list.Code)
	require.Contains(t, list.Body.String(), `"domains":["a.example.invalid","b.example.invalid"]`)
	require.NotContains(t, list.Body.String(), "includedInStats")
}
