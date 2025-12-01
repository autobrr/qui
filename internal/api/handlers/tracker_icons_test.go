// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTrackerIconProvider implements TrackerIconProvider for testing
type mockTrackerIconProvider struct {
	icons   map[string]string
	iconErr error
	listErr error
}

func (m *mockTrackerIconProvider) GetIcon(ctx context.Context, host, trackerURL string) (string, error) {
	if m.iconErr != nil {
		return "", m.iconErr
	}
	if icon, ok := m.icons[host]; ok {
		return icon, nil
	}
	return "", nil
}

func (m *mockTrackerIconProvider) ListIcons(ctx context.Context) (map[string]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.icons, nil
}

func TestNewTrackerIconHandler(t *testing.T) {
	t.Parallel()

	mock := &mockTrackerIconProvider{}
	handler := NewTrackerIconHandler(mock)

	assert.NotNil(t, handler)
	assert.Equal(t, mock, handler.service)
}

func TestTrackerIconHandler_GetTrackerIcons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		icons          map[string]string
		listErr        error
		expectedStatus int
		checkBody      func(t *testing.T, body string)
	}{
		{
			name: "returns icons successfully",
			icons: map[string]string{
				"tracker1.com": "data:image/png;base64,abc123",
				"tracker2.org": "data:image/svg+xml;base64,xyz789",
			},
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				var result map[string]string
				err := json.Unmarshal([]byte(body), &result)
				require.NoError(t, err)
				assert.Equal(t, "data:image/png;base64,abc123", result["tracker1.com"])
				assert.Equal(t, "data:image/svg+xml;base64,xyz789", result["tracker2.org"])
			},
		},
		{
			name:           "returns empty map when no icons",
			icons:          map[string]string{},
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				var result map[string]string
				err := json.Unmarshal([]byte(body), &result)
				require.NoError(t, err)
				assert.Empty(t, result)
			},
		},
		{
			name:           "returns nil map as empty JSON object",
			icons:          nil,
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				// nil map encodes as null in JSON
				assert.Contains(t, body, "null")
			},
		},
		{
			name:           "returns error when list fails",
			listErr:        errors.New("database error"),
			expectedStatus: http.StatusInternalServerError,
			checkBody: func(t *testing.T, body string) {
				assert.Contains(t, body, "failed to list tracker icons")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockTrackerIconProvider{
				icons:   tt.icons,
				listErr: tt.listErr,
			}
			handler := NewTrackerIconHandler(mock)

			req := httptest.NewRequest(http.MethodGet, "/api/tracker-icons", nil)
			rec := httptest.NewRecorder()

			handler.GetTrackerIcons(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, rec.Body.String())
			}
		})
	}
}

func TestTrackerIconHandler_GetTrackerIcons_Headers(t *testing.T) {
	t.Parallel()

	mock := &mockTrackerIconProvider{
		icons: map[string]string{
			"example.com": "data:image/png;base64,test",
		},
	}
	handler := NewTrackerIconHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/tracker-icons", nil)
	rec := httptest.NewRecorder()

	handler.GetTrackerIcons(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
}
