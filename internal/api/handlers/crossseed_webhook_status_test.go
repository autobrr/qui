// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/crossseed"
)

func TestWebhookResponseStatus(t *testing.T) {
	tests := []struct {
		name     string
		response *crossseed.WebhookCheckResponse
		want     int
	}{
		{
			name:     "nil response treated as server error",
			response: nil,
			want:     http.StatusInternalServerError,
		},
		{
			name: "completed matches return 200",
			response: &crossseed.WebhookCheckResponse{
				CanCrossSeed: true,
				Matches: []crossseed.WebhookCheckMatch{
					{InstanceID: 1},
				},
			},
			want: http.StatusOK,
		},
		{
			name: "pending matches return 202",
			response: &crossseed.WebhookCheckResponse{
				CanCrossSeed: false,
				Matches: []crossseed.WebhookCheckMatch{
					{InstanceID: 1},
				},
			},
			want: http.StatusAccepted,
		},
		{
			name: "no matches return 404",
			response: &crossseed.WebhookCheckResponse{
				CanCrossSeed: false,
				Matches:      nil,
			},
			want: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := webhookResponseStatus(tt.response)
			require.Equal(t, tt.want, got)
		})
	}
}
