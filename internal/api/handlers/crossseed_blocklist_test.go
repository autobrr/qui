// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func TestAddBlocklistEntryReturnsNotFoundWhenInstanceMissing(t *testing.T) {
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	handler := &CrossSeedHandler{
		service:       nil, // Should not be called for a missing instance
		instanceStore: instanceStore,
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/cross-seed/blocklist", strings.NewReader(`{
		"instanceId": 99999,
		"infoHash": "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		"note": "test"
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	handler.AddBlocklistEntry(resp, req)

	require.Equal(t, http.StatusNotFound, resp.Code)
	require.Contains(t, resp.Body.String(), "Instance not found")
}
