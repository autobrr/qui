// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeTrackerIconProvider struct {
	icons map[string]string
}

func (f fakeTrackerIconProvider) GetIcon(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (f fakeTrackerIconProvider) ListIcons(_ context.Context) (map[string]string, error) {
	return f.icons, nil
}

func TestTrackerIconHandler_GetTrackerIcons_NoStoreCaching(t *testing.T) {
	t.Parallel()

	h := NewTrackerIconHandler(fakeTrackerIconProvider{
		icons: map[string]string{"example.org": "data:image/png;base64,AAA"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tracker-icons", nil)
	rr := httptest.NewRecorder()
	h.GetTrackerIcons(rr, req)

	res := rr.Result()
	defer res.Body.Close()

	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "application/json", res.Header.Get("Content-Type"))
	require.Equal(t, "no-store", res.Header.Get("Cache-Control"))
}
