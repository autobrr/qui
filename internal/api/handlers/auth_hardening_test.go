// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func newPasswordAuthHandler(t *testing.T) *AuthHandler {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "auth-hardening")
	authService := auth.NewService(db)
	_, err := authService.SetupUser(t.Context(), "alice", "password1234")
	require.NoError(t, err)

	// Login warms instances in the background; an empty store keeps that a no-op.
	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	return &AuthHandler{
		authService:    authService,
		sessionManager: scs.New(),
		config:         &domain.Config{},
		instanceStore:  instanceStore,
		loginLimiter:   newLoginLimiter(),
	}
}

func post(t *testing.T, h http.Handler, path, contentType, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, h *AuthHandler, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if c.Name == h.sessionManager.Cookie.Name {
			return c
		}
	}
	t.Fatal("no session cookie in response")
	return nil
}

func sessionAuthenticated(t *testing.T, h *AuthHandler, cookie *http.Cookie) bool {
	t.Helper()

	ctx, err := h.sessionManager.Load(t.Context(), cookie.Value)
	require.NoError(t, err)
	return h.sessionManager.GetBool(ctx, "authenticated")
}

func TestLoginAndSetupRequireJSONContentType(t *testing.T) {
	h := newPasswordAuthHandler(t)
	login := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Login))
	setup := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Setup))
	body := `{"username":"alice","password":"password1234"}`

	assert.Equal(t, http.StatusUnsupportedMediaType, post(t, login, "/api/auth/login", "text/plain", body).Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, post(t, login, "/api/auth/login", "", body).Code)
	assert.Equal(t, http.StatusOK, post(t, login, "/api/auth/login", "application/json; charset=utf-8", body).Code)

	// Setup is already complete, so a JSON request gets 400 and a non-JSON one 415.
	assert.Equal(t, http.StatusUnsupportedMediaType, post(t, setup, "/api/auth/setup", "application/x-www-form-urlencoded", body).Code)
	assert.Equal(t, http.StatusBadRequest, post(t, setup, "/api/auth/setup", "application/json", body).Code)
}

func TestLoginLimitsFailedPasswordAttempts(t *testing.T) {
	h := newPasswordAuthHandler(t)
	login := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Login))
	wrong := `{"username":"alice","password":"wrong-password"}`
	right := `{"username":"alice","password":"password1234"}`

	for i := range 5 {
		assert.Equal(t, http.StatusUnauthorized, post(t, login, "/api/auth/login", "application/json", wrong).Code, "attempt %d", i+1)
	}

	assert.Equal(t, http.StatusTooManyRequests, post(t, login, "/api/auth/login", "application/json", wrong).Code)
	assert.Equal(t, http.StatusTooManyRequests, post(t, login, "/api/auth/login", "application/json", right).Code)
}

func TestLoginLimitHoldsUnderParallelAttempts(t *testing.T) {
	h := newPasswordAuthHandler(t)
	login := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Login))
	wrong := `{"username":"alice","password":"wrong-password"}`

	codes := make(chan int, 6)
	for range 6 {
		go func() { codes <- post(t, login, "/api/auth/login", "application/json", wrong).Code }()
	}
	got := map[int]int{}
	for range 6 {
		got[<-codes]++
	}
	assert.Equal(t, map[int]int{http.StatusUnauthorized: 5, http.StatusTooManyRequests: 1}, got)
}

func TestChangePasswordEndsEverySession(t *testing.T) {
	h := newPasswordAuthHandler(t)
	login := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Login))
	change := h.sessionManager.LoadAndSave(http.HandlerFunc(h.ChangePassword))
	creds := `{"username":"alice","password":"password1234"}`

	first := sessionCookie(t, h, post(t, login, "/api/auth/login", "application/json", creds))
	second := sessionCookie(t, h, post(t, login, "/api/auth/login", "application/json", creds))
	require.True(t, sessionAuthenticated(t, h, first))
	require.True(t, sessionAuthenticated(t, h, second))

	rec := post(t, change, "/api/auth/change-password", "application/json",
		`{"currentPassword":"password1234","newPassword":"password5678"}`, first)
	require.Equal(t, http.StatusOK, rec.Code)

	assert.False(t, sessionAuthenticated(t, h, first))
	assert.False(t, sessionAuthenticated(t, h, second))

	// The browser that changed the password gets its cookie cleared.
	cleared := sessionCookie(t, h, rec)
	assert.Empty(t, cleared.Value)
	assert.Negative(t, cleared.MaxAge)

	assert.Equal(t, http.StatusOK, post(t, login, "/api/auth/login", "application/json",
		`{"username":"alice","password":"password5678"}`).Code)
}

// failingDeleteStore makes RenewToken fail for a request that already carries
// a session: scs deletes the old token before it issues the new one.
type failingDeleteStore struct{ *memstore.MemStore }

func (failingDeleteStore) Delete(string) error { return errors.New("store down") }

func TestLoginFailsWhenTokenRenewalFails(t *testing.T) {
	h := newPasswordAuthHandler(t)
	h.sessionManager.Store = failingDeleteStore{memstore.New()}
	login := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Login))
	creds := `{"username":"alice","password":"password1234"}`

	existing := sessionCookie(t, h, post(t, login, "/api/auth/login", "application/json", creds))

	rec := post(t, login, "/api/auth/login", "application/json", creds, existing)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	for _, c := range rec.Result().Cookies() {
		assert.NotEqual(t, h.sessionManager.Cookie.Name, c.Name, "no session cookie must be issued")
	}
}
