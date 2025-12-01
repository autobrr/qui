// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/update"
)

func TestNewVersionHandler(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	updateSvc := update.NewService(log, true, "1.0.0", "test")

	h := NewVersionHandler(updateSvc)
	require.NotNil(t, h)
	assert.Equal(t, updateSvc, h.updateService)
}

func TestVersionHandler_GetLatestVersion(t *testing.T) {
	t.Parallel()

	t.Run("no release available", func(t *testing.T) {
		t.Parallel()

		log := zerolog.Nop()
		updateSvc := update.NewService(log, true, "1.0.0", "test")

		h := NewVersionHandler(updateSvc)

		req := httptest.NewRequest(http.MethodGet, "/version/latest", nil)
		rec := httptest.NewRecorder()

		h.GetLatestVersion(rec, req)

		// When no release is available, should return 204 No Content
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestLatestVersionResponse_Struct(t *testing.T) {
	t.Parallel()

	resp := LatestVersionResponse{
		TagName:     "v1.2.3",
		Name:        "Release 1.2.3",
		HTMLURL:     "https://github.com/org/repo/releases/v1.2.3",
		PublishedAt: "2024-01-15T10:00:00Z",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded LatestVersionResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, resp.TagName, decoded.TagName)
	assert.Equal(t, resp.Name, decoded.Name)
	assert.Equal(t, resp.HTMLURL, decoded.HTMLURL)
	assert.Equal(t, resp.PublishedAt, decoded.PublishedAt)
}

func TestLatestVersionResponse_OmitEmptyName(t *testing.T) {
	t.Parallel()

	resp := LatestVersionResponse{
		TagName:     "v1.0.0",
		HTMLURL:     "https://example.com",
		PublishedAt: "2024-01-01T00:00:00Z",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Name should be omitted when empty
	assert.NotContains(t, string(data), `"name":`)
}
