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

func TestUpdateAutomationSettings_PersistsSeasonPackFields(t *testing.T) {
	store := newTestCrossSeedStore(t)

	svc := &crossseed.Service{}
	setServiceField(t, svc, "automationStore", store)

	handler := &CrossSeedHandler{service: svc}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/cross-seed/settings", strings.NewReader(`{
		"seasonPackEnabled": true,
		"seasonPackCoverageThreshold": 0.9,
		"seasonPackTags": ["season-pack", "cross-seed"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.UpdateAutomationSettings(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)

	var updated models.CrossSeedAutomationSettings
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
	require.True(t, updated.SeasonPackEnabled)
	require.InDelta(t, 0.9, updated.SeasonPackCoverageThreshold, 0.001)
	require.Equal(t, []string{"season-pack", "cross-seed"}, updated.SeasonPackTags)

	stored, err := store.GetSettings(t.Context())
	require.NoError(t, err)
	require.True(t, stored.SeasonPackEnabled)
	require.InDelta(t, 0.9, stored.SeasonPackCoverageThreshold, 0.001)
	require.Equal(t, []string{"season-pack", "cross-seed"}, stored.SeasonPackTags)
}

func TestUpdateAutomationSettings_RejectsInvalidSeasonPackThreshold(t *testing.T) {
	store := newTestCrossSeedStore(t)

	svc := &crossseed.Service{}
	setServiceField(t, svc, "automationStore", store)

	handler := &CrossSeedHandler{service: svc}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/cross-seed/settings", strings.NewReader(`{
		"seasonPackCoverageThreshold": 0
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.UpdateAutomationSettings(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "Season pack coverage threshold")
}
