// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
)

func setCrossSeedServiceField[T any](service *crossseed.Service, fieldName string, value T) {
	field := reflect.ValueOf(service).Elem().FieldByName(fieldName)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func TestUpdateAutomationSettings_RejectsSubMiBPooledRecheckLimit(t *testing.T) {
	t.Parallel()

	handler := &CrossSeedHandler{}
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPut,
		"/api/cross-seed/settings",
		strings.NewReader(`{"maxMissingBytesAfterRecheck":1048575}`),
	)
	rec := httptest.NewRecorder()

	handler.UpdateAutomationSettings(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"maxMissingBytesAfterRecheck must be one MiB or greater"}`, rec.Body.String())
}

func TestPatchAutomationSettings_RejectsSubMiBPooledRecheckLimit(t *testing.T) {
	t.Parallel()

	handler := &CrossSeedHandler{}
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPatch,
		"/api/cross-seed/settings",
		strings.NewReader(`{"maxMissingBytesAfterRecheck":1048575}`),
	)
	rec := httptest.NewRecorder()

	handler.PatchAutomationSettings(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"maxMissingBytesAfterRecheck must be one MiB or greater"}`, rec.Body.String())
}

func TestUpdateAutomationSettings_MergesWithExistingSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "crossseed-settings.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	store, err := models.NewCrossSeedStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	existing := models.DefaultCrossSeedAutomationSettings()
	existing.Enabled = false
	existing.RunIntervalMinutes = 120
	existing.TargetInstanceIDs = []int{1}
	existing.TargetIndexerIDs = []int{2}
	existing.RSSSourceCategories = []string{"tv"}
	existing.WebhookSourceTags = []string{"keep-webhook-tag"}
	existing.RSSAutomationTags = []string{"keep-rss-tag"}
	existing.SkipAutoResumeRSS = true
	existing.SkipPieceBoundarySafetyCheck = false
	existing.MaxMissingBytesAfterRecheck = 200 * 1024 * 1024

	_, err = store.UpsertSettings(ctx, existing)
	require.NoError(t, err)

	service := &crossseed.Service{}
	setCrossSeedServiceField(service, "automationStore", store)
	handler := &CrossSeedHandler{service: service}

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPut,
		"/api/cross-seed/settings",
		strings.NewReader(`{
			"enabled": true,
			"runIntervalMinutes": 45,
			"targetInstanceIds": [9],
			"targetIndexerIds": [10]
		}`),
	)
	rec := httptest.NewRecorder()

	handler.UpdateAutomationSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var updated models.CrossSeedAutomationSettings
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))

	assert.True(t, updated.Enabled)
	assert.Equal(t, 45, updated.RunIntervalMinutes)
	assert.Equal(t, []int{9}, updated.TargetInstanceIDs)
	assert.Equal(t, []int{10}, updated.TargetIndexerIDs)
	assert.Equal(t, []string{"tv"}, updated.RSSSourceCategories)
	assert.Equal(t, []string{"keep-webhook-tag"}, updated.WebhookSourceTags)
	assert.Equal(t, []string{"keep-rss-tag"}, updated.RSSAutomationTags)
	assert.True(t, updated.SkipAutoResumeRSS)
	assert.False(t, updated.SkipPieceBoundarySafetyCheck)
	assert.EqualValues(t, 200*1024*1024, updated.MaxMissingBytesAfterRecheck)
}
