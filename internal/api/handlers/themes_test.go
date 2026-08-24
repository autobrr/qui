// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/themes"
)

// stubPremium controls the premium-access result for the themes handler tests.
type stubPremium struct {
	ok  bool
	err error
}

func (s stubPremium) HasPremiumAccess(context.Context) (bool, error) {
	return s.ok, s.err
}

// stubThemesDir points the handler at a fixed directory (usually t.TempDir()).
type stubThemesDir struct {
	dir string
	err error
}

func (s stubThemesDir) EnsureCustomThemesDir() (string, error) {
	return s.dir, s.err
}

func doListCustomThemes(t *testing.T, h *ThemesHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/themes/custom", nil)
	rec := httptest.NewRecorder()
	h.ListCustomThemes(rec, req)
	return rec
}

func TestThemesHandler_NotPremium(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{dir: t.TempDir()}, stubPremium{ok: false}, nil, nil, nil)
	rec := doListCustomThemes(t, h)

	require.Equal(t, http.StatusForbidden, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "Premium access required", resp.Error)
}

func TestThemesHandler_PremiumCheckError(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{dir: t.TempDir()}, stubPremium{err: errors.New("boom")}, nil, nil, nil)
	rec := doListCustomThemes(t, h)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestThemesHandler_EmptyDirReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true}, nil, nil, nil)
	rec := doListCustomThemes(t, h)

	require.Equal(t, http.StatusOK, rec.Code)
	// Must serialize as [] not null so the frontend can map over it safely.
	require.Contains(t, rec.Body.String(), `"themes":[]`)

	var resp CustomThemesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Themes)
	require.Equal(t, filepath.Clean(dir), filepath.Clean(resp.Directory))
}

func TestThemesHandler_ListsOnlyRegularCSSFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocean.css"), []byte(":root{--primary:blue}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nope"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "subdir", "nested.css"), []byte(":root{}"), 0o600))

	// Symlink to a .css outside the directory; must NOT be followed. Best-effort,
	// since symlink creation needs privileges on some platforms.
	outside := filepath.Join(t.TempDir(), "evil.css")
	require.NoError(t, os.WriteFile(outside, []byte(":root{--primary:red}"), 0o600))
	_ = os.Symlink(outside, filepath.Join(dir, "linked.css"))

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true}, nil, nil, nil)
	rec := doListCustomThemes(t, h)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp CustomThemesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.Len(t, resp.Themes, 1)
	require.Equal(t, "ocean.css", resp.Themes[0].Filename)
	require.Equal(t, "ocean", resp.Themes[0].ID)
	require.Equal(t, ":root{--primary:blue}", resp.Themes[0].CSS)
}

func TestThemesHandler_SkipsOversizeFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.css"), []byte(":root{}"), 0o600))
	big := make([]byte, maxCustomThemeFileSize+1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "huge.css"), big, 0o600))

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true}, nil, nil, nil)
	rec := doListCustomThemes(t, h)

	var resp CustomThemesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Themes, 1)
	require.Equal(t, "small.css", resp.Themes[0].Filename)
}

func TestReadCustomThemeCSSRejectsOversizeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.css")
	big := make([]byte, maxCustomThemeFileSize+1)
	require.NoError(t, os.WriteFile(path, big, 0o600))

	css, ok := readCustomThemeCSS(path)

	require.False(t, ok)
	require.Nil(t, css)
}

func TestThemesHandler_CapsFileCount(t *testing.T) {
	dir := t.TempDir()
	for i := range maxCustomThemeFiles + 10 {
		name := fmt.Sprintf("theme-%03d.css", i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(":root{}"), 0o600))
	}

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true}, nil, nil, nil)
	rec := doListCustomThemes(t, h)

	var resp CustomThemesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Themes, maxCustomThemeFiles)
}

func TestThemesHandler_EnsureDirErrorReturnsEmpty(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{dir: filepath.Join("non", "existent"), err: errors.New("mkdir failed")}, stubPremium{ok: true}, nil, nil, nil)
	rec := doListCustomThemes(t, h)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"themes":[]`)
}

// stubThemeSettings is an in-memory themeSettingsStore.
type stubThemeSettings struct {
	saved *models.ThemeSettings
	err   error
}

func (s *stubThemeSettings) Get(context.Context) (*models.ThemeSettings, error) {
	return s.saved, s.err
}

func (s *stubThemeSettings) Set(_ context.Context, ts *models.ThemeSettings) error {
	if s.err != nil {
		return s.err
	}
	s.saved = ts
	return nil
}

func doUpdateThemeSettings(t *testing.T, h *ThemesHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/themes/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdateThemeSettings(rec, req)
	return rec
}

// recordingPublisher records published activity events.
type recordingPublisher struct {
	events []activity.Event
}

func (p *recordingPublisher) Publish(ev activity.Event) {
	p.events = append(p.events, ev)
}

func TestThemeSettings_UpdatePublishesActivity(t *testing.T) {
	pub := &recordingPublisher{}
	h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: true}, &stubThemeSettings{}, nil, pub)

	rec := doUpdateThemeSettings(t, h, `{"themeId":"minimal"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, pub.events, 1)
	require.Equal(t, activity.KindThemeSettings, pub.events[0].Kind)
}

func TestThemeSettings_NoActivityOnStoreFailure(t *testing.T) {
	pub := &recordingPublisher{}
	h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: true}, &stubThemeSettings{err: errors.New("boom")}, nil, pub)

	rec := doUpdateThemeSettings(t, h, `{"themeId":"minimal"}`)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Empty(t, pub.events)
}

func TestThemeSettings_UpdateWithoutPremium(t *testing.T) {
	store := &stubThemeSettings{}
	h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: false}, store, nil, nil)

	rec := doUpdateThemeSettings(t, h, `{"themeId":"minimal"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, store.saved)
}

func TestThemeSettings_UpdateValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing themeId", `{"mode":"dark"}`},
		{"bad mode", `{"themeId":"minimal","mode":"neon"}`},
		{"bad json", `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubThemeSettings{}
			h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: true}, store, nil, nil)
			rec := doUpdateThemeSettings(t, h, tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, store.saved)
		})
	}
}

func TestThemeSettings_UpdateAndGet(t *testing.T) {
	store := &stubThemeSettings{}
	h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: true}, store, nil, nil)

	rec := doUpdateThemeSettings(t, h, `{"themeId":"minimal","variation":"blue"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, &models.ThemeSettings{ThemeID: "minimal", Mode: "auto", Variation: "blue"}, store.saved)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/themes/settings", nil)
	getRec := httptest.NewRecorder()
	h.GetThemeSettings(getRec, req)
	require.Equal(t, http.StatusOK, getRec.Code)

	var got models.ThemeSettings
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	require.Equal(t, *store.saved, got)
}

func TestThemeSettings_GetEmpty(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: true}, &stubThemeSettings{}, nil, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/themes/settings", nil)
	rec := httptest.NewRecorder()
	h.GetThemeSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "null", strings.TrimSpace(rec.Body.String()))
}

func TestThemeSettings_UpdateUnknownTheme(t *testing.T) {
	store := &stubThemeSettings{}
	h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: true}, store, nil, nil)

	rec := doUpdateThemeSettings(t, h, `{"themeId":"not-a-theme"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, store.saved)

	// Custom theme ids pass without registry validation.
	rec = doUpdateThemeSettings(t, h, `{"themeId":"custom:mytheme"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "custom:mytheme", store.saved.ThemeID)
}

func doListThemes(t *testing.T, h *ThemesHandler) []BuiltinTheme {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/themes", nil)
	rec := httptest.NewRecorder()
	h.ListThemes(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Themes []BuiltinTheme `json:"themes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp.Themes
}

func TestListThemes_FreeAlwaysHaveCSS(t *testing.T) {
	for _, premium := range []bool{false, true} {
		h := NewThemesHandler(stubThemesDir{}, stubPremium{ok: premium}, &stubThemeSettings{}, nil, nil)
		for _, theme := range doListThemes(t, h) {
			if theme.Premium {
				continue
			}
			require.NotEmpty(t, theme.CSS, "free theme %s must always include CSS (premium=%v)", theme.ID, premium)
			require.Nil(t, theme.Preview)
		}
	}
}

// TestBuildBuiltinThemeList uses synthetic themes because the premium CSS
// files are not in the public repo: gating tests against the real registry
// would silently assert nothing in CI.
func TestBuildBuiltinThemeList(t *testing.T) {
	list := []themes.Theme{
		{ID: "free", Name: "Free", CSS: "free-css", Preview: themes.Preview{Light: map[string]string{"--primary": "red"}}},
		{ID: "paid", Name: "Paid", Premium: true, CSS: "paid-css", Preview: themes.Preview{Light: map[string]string{"--primary": "gold"}}},
	}

	// Unlicensed: no premium CSS regardless of auth or selection.
	unlicensed := buildBuiltinThemeList(list, false, true, "paid")
	require.Equal(t, "free-css", unlicensed[0].CSS)
	require.Nil(t, unlicensed[0].Preview)
	require.Empty(t, unlicensed[1].CSS, "premium CSS leaked without a license")
	require.Equal(t, "gold", unlicensed[1].Preview.Light["--primary"])

	// Licensed and authenticated: full premium CSS.
	authed := buildBuiltinThemeList(list, true, true, "")
	require.Equal(t, "paid-css", authed[1].CSS)
	require.Nil(t, authed[1].Preview)

	// Licensed but anonymous: premium CSS only for the selected theme.
	anonSelected := buildBuiltinThemeList(list, true, false, "paid")
	require.Equal(t, "paid-css", anonSelected[1].CSS, "the selected premium theme must paint the login page")

	anonOther := buildBuiltinThemeList(list, true, false, "free")
	require.Empty(t, anonOther[1].CSS, "an unselected premium theme must not leak CSS to anonymous callers")
	require.Equal(t, "gold", anonOther[1].Preview.Light["--primary"])
}

func TestListThemes_PremiumCheckErrorServesFree(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{}, stubPremium{err: errors.New("boom")}, &stubThemeSettings{}, nil, nil)
	require.NotEmpty(t, doListThemes(t, h))
}
