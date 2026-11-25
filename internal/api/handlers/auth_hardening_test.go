// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type passwordAuthRig struct {
	*AuthHandler
	warmed chan error
}

type emptyWarmupStore struct {
	dbinterface.Querier
	warmed chan error
}

func (s emptyWarmupStore) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := s.Querier.QueryContext(ctx, query, args...)
	if err == nil {
		if rows.Next() {
			err = errors.New("password auth fixture has an instance to warm")
		} else {
			err = rows.Err()
		}
		err = errors.Join(err, rows.Close())
	}
	s.warmed <- err
	return rows, err
}

func (h *passwordAuthRig) postLogin(t *testing.T, contentType, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	rec := post(t, h.sessionManager.LoadAndSave(http.HandlerFunc(h.Login)), "/api/auth/login", contentType, body, cookies...)
	if rec.Code == http.StatusOK {
		select {
		case err := <-h.warmed:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("login did not finish reading instances for session warmup")
		}
	}
	return rec
}

func newPasswordAuthHandler(t *testing.T) *passwordAuthRig {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "auth-hardening")
	authService := auth.NewService(db)
	_, err := authService.SetupUser(t.Context(), "alice", "password1234")
	require.NoError(t, err)

	warmed := make(chan error, 1)
	instanceStore, err := models.NewInstanceStore(emptyWarmupStore{Querier: db, warmed: warmed}, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	return &passwordAuthRig{warmed: warmed, AuthHandler: &AuthHandler{
		authService:    authService,
		sessionManager: scs.New(),
		config:         &domain.Config{},
		instanceStore:  instanceStore,
		loginLimiter:   newLoginLimiter(),
	}}
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

func sessionCookie(t *testing.T, h *passwordAuthRig, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if c.Name == h.sessionManager.Cookie.Name {
			return c
		}
	}
	t.Fatal("no session cookie in response")
	return nil
}

func sessionAuthenticated(t *testing.T, h *passwordAuthRig, cookie *http.Cookie) bool {
	t.Helper()

	ctx, err := h.sessionManager.Load(t.Context(), cookie.Value)
	require.NoError(t, err)
	return h.sessionManager.GetBool(ctx, "authenticated")
}

func TestLoginAndSetupRequireJSONContentType(t *testing.T) {
	h := newPasswordAuthHandler(t)
	setup := h.sessionManager.LoadAndSave(http.HandlerFunc(h.Setup))
	body := `{"username":"alice","password":"password1234"}`

	assert.Equal(t, http.StatusUnsupportedMediaType, h.postLogin(t, "text/plain", body).Code)
	assert.Equal(t, http.StatusUnsupportedMediaType, h.postLogin(t, "", body).Code)
	assert.Equal(t, http.StatusOK, h.postLogin(t, "application/json; charset=utf-8", body).Code)

	assert.Equal(t, http.StatusUnsupportedMediaType, post(t, setup, "/api/auth/setup", "application/x-www-form-urlencoded", body).Code)
	assert.Equal(t, http.StatusBadRequest, post(t, setup, "/api/auth/setup", "application/json", body).Code)
}

func TestLoginLimitsFailedPasswordAttempts(t *testing.T) {
	h := newPasswordAuthHandler(t)
	wrong := `{"username":"alice","password":"wrong-password"}`
	right := `{"username":"alice","password":"password1234"}`

	for i := range 5 {
		assert.Equal(t, http.StatusUnauthorized, h.postLogin(t, "application/json", wrong).Code, "attempt %d", i+1)
	}

	assert.Equal(t, http.StatusTooManyRequests, h.postLogin(t, "application/json", wrong).Code)
	assert.Equal(t, http.StatusTooManyRequests, h.postLogin(t, "application/json", right).Code)
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
	change := h.sessionManager.LoadAndSave(http.HandlerFunc(h.ChangePassword))
	creds := `{"username":"alice","password":"password1234"}`

	first := sessionCookie(t, h, h.postLogin(t, "application/json", creds))
	second := sessionCookie(t, h, h.postLogin(t, "application/json", creds))
	require.True(t, sessionAuthenticated(t, h, first))
	require.True(t, sessionAuthenticated(t, h, second))

	rec := post(t, change, "/api/auth/change-password", "application/json",
		`{"currentPassword":"password1234","newPassword":"password5678"}`, first)
	require.Equal(t, http.StatusOK, rec.Code)

	assert.False(t, sessionAuthenticated(t, h, first))
	assert.False(t, sessionAuthenticated(t, h, second))

	cleared := sessionCookie(t, h, rec)
	assert.Empty(t, cleared.Value)
	assert.Negative(t, cleared.MaxAge)

	assert.Equal(t, http.StatusOK, h.postLogin(t, "application/json",
		`{"username":"alice","password":"password5678"}`).Code)
}

type failingDeleteStore struct{ *memstore.MemStore }

func (failingDeleteStore) Delete(string) error { return errors.New("store down") }

func TestLoginFailsWhenTokenRenewalFails(t *testing.T) {
	h := newPasswordAuthHandler(t)
	store := memstore.New()
	h.sessionManager.Store = failingDeleteStore{store}
	creds := `{"username":"alice","password":"password1234"}`

	existing := sessionCookie(t, h, h.postLogin(t, "application/json", creds))

	rec := h.postLogin(t, "application/json", creds, existing)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.False(t, sessionAuthenticated(t, h, existing), "the old session must not remain authenticated")
	for _, c := range rec.Result().Cookies() {
		if c.Name == h.sessionManager.Cookie.Name {
			assert.False(t, sessionAuthenticated(t, h, c), "the response cookie must not authenticate")
		}
	}

	validateReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/validate", nil)
	validateReq.AddCookie(existing)
	validateRec := httptest.NewRecorder()
	h.sessionManager.LoadAndSave(http.HandlerFunc(h.Validate)).ServeHTTP(validateRec, validateReq)
	assert.Equal(t, http.StatusUnauthorized, validateRec.Code)

	h.sessionManager.Store = store
	retry := h.postLogin(t, "application/json", creds, existing)
	require.Equal(t, http.StatusOK, retry.Code)
	renewed := sessionCookie(t, h, retry)
	assert.NotEqual(t, existing.Value, renewed.Value)
	assert.True(t, sessionAuthenticated(t, h, renewed))
	assert.False(t, sessionAuthenticated(t, h, existing))
}
