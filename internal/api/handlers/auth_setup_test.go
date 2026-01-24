// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/domain"
)

func TestSetupForbiddenWhenOIDCEnabled(t *testing.T) {
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	authService := auth.NewService(db)
	sessionManager := scs.New()

	config := &domain.Config{
		OIDCEnabled: true,
	}

	handler := &AuthHandler{
		authService:    authService,
		sessionManager: sessionManager,
		config:         config,
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/setup", strings.NewReader(`{"username":"alice","password":"password1234"}`))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()

	handler.Setup(resp, req)

	assert.Equal(t, http.StatusForbidden, resp.Code)
	assert.Contains(t, resp.Body.String(), "Setup is disabled when OIDC is enabled")
}

func TestNewAuthHandlerFailsWhenOIDCInitFails(t *testing.T) {
	authService := &auth.Service{}
	sessionManager := scs.New()

	config := &domain.Config{
		OIDCEnabled: true,
		// Missing mandatory OIDC settings so initialization fails before network calls.
	}

	_, err := NewAuthHandler(authService, sessionManager, config, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OIDC issuer is required")
}
