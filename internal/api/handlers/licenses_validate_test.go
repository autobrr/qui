// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/services/license"
)

func TestValidateLicense_MissingStoredLicenseReturns404(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "licenses.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repo := database.NewLicenseRepo(db)
	service := license.NewLicenseService(repo, nil, nil, t.TempDir())

	handler := NewLicenseHandler(service)

	req := httptest.NewRequest(http.MethodPost, "/api/license/validate", bytes.NewReader([]byte(`{"licenseKey":"missing"}`)))
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.Username, "tester"))

	rr := httptest.NewRecorder()
	handler.ValidateLicense(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "license not found")
}
