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
	"testing"

	"github.com/stretchr/testify/require"
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
	h := NewThemesHandler(stubThemesDir{dir: t.TempDir()}, stubPremium{ok: false})
	rec := doListCustomThemes(t, h)

	require.Equal(t, http.StatusForbidden, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "Premium access required", resp.Error)
}

func TestThemesHandler_PremiumCheckError(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{dir: t.TempDir()}, stubPremium{err: errors.New("boom")})
	rec := doListCustomThemes(t, h)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestThemesHandler_EmptyDirReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true})
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

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true})
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

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true})
	rec := doListCustomThemes(t, h)

	var resp CustomThemesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Themes, 1)
	require.Equal(t, "small.css", resp.Themes[0].Filename)
}

func TestThemesHandler_CapsFileCount(t *testing.T) {
	dir := t.TempDir()
	for i := range maxCustomThemeFiles + 10 {
		name := fmt.Sprintf("theme-%03d.css", i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(":root{}"), 0o600))
	}

	h := NewThemesHandler(stubThemesDir{dir: dir}, stubPremium{ok: true})
	rec := doListCustomThemes(t, h)

	var resp CustomThemesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Themes, maxCustomThemeFiles)
}

func TestThemesHandler_EnsureDirErrorReturnsEmpty(t *testing.T) {
	h := NewThemesHandler(stubThemesDir{dir: filepath.Join("non", "existent"), err: errors.New("mkdir failed")}, stubPremium{ok: true})
	rec := doListCustomThemes(t, h)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"themes":[]`)
}
