// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/version"
)

// stubReleaseProvider lets the version handler tests control the cached release
// without performing a live update check.
type stubReleaseProvider struct {
	release *version.Release
}

func (s stubReleaseProvider) GetLatestRelease(context.Context) *version.Release {
	return s.release
}

func TestVersionHandler_GetVersion_NoUpdate(t *testing.T) {
	handler := NewVersionHandler(stubReleaseProvider{release: nil}, "v1.2.3")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	handler.GetVersion(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp VersionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "v1.2.3", resp.Version)
	require.False(t, resp.UpdateAvailable)
	require.Empty(t, resp.LatestVersion)
}

func TestVersionHandler_GetVersion_UpdateAvailable(t *testing.T) {
	handler := NewVersionHandler(stubReleaseProvider{release: &version.Release{
		TagName:     "v1.3.0",
		PublishedAt: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
	}}, "v1.2.3")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	handler.GetVersion(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp VersionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "v1.2.3", resp.Version)
	require.True(t, resp.UpdateAvailable)
	require.Equal(t, "v1.3.0", resp.LatestVersion)
}
