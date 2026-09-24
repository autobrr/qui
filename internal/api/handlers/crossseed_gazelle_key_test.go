// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// A save that changes an OPS or RED key checks the key with the tracker first (#2809).
func TestAutomationSettingsChecksGazelleKeyOnSave(t *testing.T) {
	const (
		accepted = `{"status":"success","response":{"username":"user"}}`
		rejected = `{"status":"failure","error":"invalid token"}`
	)
	tests := []struct {
		name         string
		method       string
		body         string
		reply        string // "" means the tracker is down
		wantStatus   int
		wantRequests int32
		wantText     string
		host         string // stored key to read; "" means orpheus.network
		wantStored   string
	}{
		{
			name:         "accepted key saves",
			method:       http.MethodPatch,
			body:         `{"gazelleEnabled":true,"orpheusApiKey":"good-key"}`,
			reply:        accepted,
			wantStatus:   http.StatusOK,
			wantRequests: 1,
			wantStored:   "good-key",
		},
		{
			name:         "rejected key fails the save",
			method:       http.MethodPatch,
			body:         `{"gazelleEnabled":true,"orpheusApiKey":"bad-key"}`,
			reply:        rejected,
			wantStatus:   http.StatusBadRequest,
			wantRequests: 1,
			wantText:     "OPS rejected the API key or this IP: invalid token",
		},
		{
			name:         "rejected key fails a put",
			method:       http.MethodPut,
			body:         `{"seasonPackCoverageThreshold":0.75,"gazelleEnabled":true,"redactedApiKey":"bad-key"}`,
			reply:        rejected,
			wantStatus:   http.StatusBadRequest,
			wantRequests: 1,
			wantText:     "RED rejected the API key or this IP",
			host:         "redacted.sh",
		},
		{
			name:         "one rejected key saves neither key",
			method:       http.MethodPatch,
			body:         `{"gazelleEnabled":true,"redactedApiKey":"bad-key","orpheusApiKey":"good-key"}`,
			reply:        rejected,
			wantStatus:   http.StatusBadRequest,
			wantRequests: 1,
			wantText:     "RED rejected the API key or this IP",
		},
		{
			name:       "disabled gazelle saves without a check",
			method:     http.MethodPatch,
			body:       `{"gazelleEnabled":false,"orpheusApiKey":"bad-key"}`,
			reply:      rejected,
			wantStatus: http.StatusOK, // the store hides keys while gazelle is off
		},
		{
			name:         "other tracker failure saves with a warning",
			method:       http.MethodPatch,
			body:         `{"gazelleEnabled":true,"orpheusApiKey":"good-key"}`,
			reply:        `{"status":"failure","error":"rate limit exceeded"}`,
			wantStatus:   http.StatusOK,
			wantRequests: 1,
			wantText:     "could not check the OPS API key",
			wantStored:   "good-key",
		},
		{
			name:       "tracker down saves with a warning",
			method:     http.MethodPatch,
			body:       `{"gazelleEnabled":true,"orpheusApiKey":"good-key"}`,
			wantStatus: http.StatusOK,
			wantText:   "could not check the OPS API key",
			wantStored: "good-key",
		},
		{
			name:       "placeholder and empty keys send no request",
			method:     http.MethodPatch,
			body:       `{"gazelleEnabled":true,"orpheusApiKey":"<redacted>","redactedApiKey":""}`,
			reply:      rejected,
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if got := r.URL.Query().Get("action"); got != "index" {
					t.Errorf("action = %q, want index", got)
				}
				_, _ = w.Write([]byte(tt.reply))
			}))
			t.Cleanup(server.Close)
			if tt.reply == "" {
				server.Close() // loopback with nothing listening: a connection error
			}

			handler, store := newTestCrossSeedHandler(t)
			handler.gazelleBaseURL = server.URL

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/api/cross-seed/settings", strings.NewReader(tt.body))
			resp := httptest.NewRecorder()
			if tt.method == http.MethodPut {
				handler.UpdateAutomationSettings(resp, req)
			} else {
				handler.PatchAutomationSettings(resp, req)
			}

			require.Equal(t, tt.wantStatus, resp.Code, resp.Body.String())
			require.Equal(t, tt.wantRequests, requests.Load())
			var body struct {
				Error   string `json:"error"`
				Warning string `json:"warning"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			if tt.wantText == "" {
				require.Empty(t, body.Error+body.Warning)
			}
			require.Contains(t, body.Error+body.Warning, tt.wantText)

			key, _, err := store.GetDecryptedGazelleAPIKey(t.Context(), cmp.Or(tt.host, "orpheus.network"))
			require.NoError(t, err)
			require.Equal(t, tt.wantStored, key)
		})
	}
}
