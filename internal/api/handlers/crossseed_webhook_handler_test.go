// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/crossseed"
)

func TestWebhookCheckHandler_BadRequestPaths(t *testing.T) {
	t.Parallel()

	handler := NewCrossSeedHandler(&crossseed.Service{}, nil, nil)

	tests := []struct {
		name    string
		body    string
		want    int
		message string
	}{
		{
			name:    "invalid json",
			body:    "{",
			want:    http.StatusBadRequest,
			message: "Invalid request body",
		},
		{
			name:    "missing torrent data",
			body:    `{"instanceIds":[1]}`,
			want:    http.StatusBadRequest,
			message: "torrentData is required",
		},
		{
			name:    "invalid base64",
			body:    `{"torrentData":"not-base64"}`,
			want:    http.StatusBadRequest,
			message: "invalid webhook request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/cross-seed/webhook/check", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			handler.WebhookCheck(rec, req)

			require.Equal(t, tt.want, rec.Code)
			require.Contains(t, rec.Body.String(), tt.message)
		})
	}
}
