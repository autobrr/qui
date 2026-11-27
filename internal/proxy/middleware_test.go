// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestClientAPIKeyMiddlewareLogsAShortUserAgentForAnUnknownKey(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "proxy-user-agent")
	router := chi.NewRouter()
	router.With(ClientAPIKeyMiddleware(models.NewClientAPIKeyStore(db))).
		Get("/proxy/{api-key}/*", func(http.ResponseWriter, *http.Request) {})

	var logs bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = previous })

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/proxy/unknown/api/v2/app/version", nil)
	req.Header.Set("User-Agent", "qui-test/"+strings.Repeat("a", 1<<20))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var userAgents []string
	for line := range strings.Lines(logs.String()) {
		var entry struct {
			Message   string `json:"message"`
			UserAgent string `json:"user_agent"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry.Message == "Invalid client API key" {
			userAgents = append(userAgents, entry.UserAgent)
		}
	}
	require.Len(t, userAgents, 1)
	require.Len(t, userAgents[0], 512)
	require.Equal(t, "qui-test/"+strings.Repeat("a", 503), userAgents[0])
}
