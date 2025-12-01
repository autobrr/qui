// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createMockFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>Hello World</body></html>`),
		},
		"assets/app.js": &fstest.MapFile{
			Data: []byte(`console.log('app');`),
		},
		"assets/style.css": &fstest.MapFile{
			Data: []byte(`body { color: black; }`),
		},
		"favicon.png": &fstest.MapFile{
			Data: []byte{0x89, 0x50, 0x4E, 0x47}, // PNG magic bytes
		},
		"registerSW.js": &fstest.MapFile{
			Data: []byte(`navigator.serviceWorker.register('/sw.js', {scope: '/'})`),
		},
		"manifest.webmanifest": &fstest.MapFile{
			Data: []byte(`{"start_url":"/","scope":"/"}`),
		},
	}
}

func TestNewHandler(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()

	tests := []struct {
		name    string
		version string
		baseURL string
		fs      fs.FS
	}{
		{"with fs and default base", "1.0.0", "/", mockFS},
		{"with custom base URL", "1.0.0", "/qui/", mockFS},
		{"with nil fs", "1.0.0", "/", nil},
		{"empty version", "", "/", mockFS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := NewHandler(tt.version, tt.baseURL, tt.fs)

			require.NotNil(t, h)
			assert.Equal(t, tt.version, h.version)
			assert.Equal(t, tt.baseURL, h.baseURL)
			assert.Equal(t, tt.fs, h.fs)
		})
	}
}

func TestHandler_ServeAssets_JSFile(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/javascript")
	assert.Equal(t, `console.log('app');`, rec.Body.String())
}

func TestHandler_ServeAssets_CSSFile(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/assets/style.css", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/css")
	assert.Equal(t, `body { color: black; }`, rec.Body.String())
}

func TestHandler_ServeAssets_Favicon(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/favicon.png", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "image/png")
}

func TestHandler_ServeAssets_NotFound(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Test the serveAssets handler for non-existent asset
	req := httptest.NewRequest(http.MethodGet, "/assets/nonexistent.js", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	// The handler should return 404 for non-existent assets
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_ServeSPA(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	paths := []string{
		"/",
		"/dashboard",
		"/settings",
		"/any/nested/path",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
			assert.Contains(t, rec.Body.String(), "Hello World")
		})
	}
}

func TestHandler_ServeSPA_InjectsBaseURL(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/qui/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `window.__QUI_BASE_URL__="/qui/"`)
	assert.Contains(t, body, `window.__QUI_VERSION__="1.0.0"`)
}

func TestHandler_ServeSPA_InjectsVersion(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("2.5.0-beta", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `window.__QUI_VERSION__="2.5.0-beta"`)
}

func TestHandler_NilFS(t *testing.T) {
	t.Parallel()

	h := NewHandler("1.0.0", "/", nil)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Frontend not built")
}

func TestHandler_BaseURLPathRewriting(t *testing.T) {
	t.Parallel()

	mockFS := createMockFS()
	h := NewHandler("1.0.0", "/myapp/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	t.Run("registerSW.js path rewriting", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/registerSW.js", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "/myapp/sw.js")
		assert.Contains(t, body, "scope: '/myapp/'")
	})

	t.Run("manifest.webmanifest path rewriting", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, `"/myapp/"`)
	})
}

func TestHandler_CacheHeaders(t *testing.T) {
	t.Parallel()

	// Create FS with hashed asset filename
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body></body></html>`),
		},
		"assets/app-abc123.js": &fstest.MapFile{
			Data: []byte(`console.log('hashed asset');`),
		},
		"assets/style-def456.css": &fstest.MapFile{
			Data: []byte(`body {}`),
		},
		"assets/app.js": &fstest.MapFile{
			Data: []byte(`console.log('non-hashed');`),
		},
	}

	h := NewHandler("1.0.0", "/", mockFS)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	t.Run("hashed JS file gets immutable cache", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("hashed CSS file gets immutable cache", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/assets/style-def456.css", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	})

	t.Run("non-hashed file doesn't get immutable cache", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		// Non-hashed assets shouldn't have immutable cache
		assert.NotContains(t, rec.Header().Get("Cache-Control"), "immutable")
	})
}

func TestMIMETypes(t *testing.T) {
	t.Parallel()

	mockFS := fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte(`<html></html>`)},
		"assets/test.js":    &fstest.MapFile{Data: []byte(`var a=1;`)},
		"assets/test.css":   &fstest.MapFile{Data: []byte(`body{}`)},
		"assets/test.json":  &fstest.MapFile{Data: []byte(`{}`)},
		"assets/test.svg":   &fstest.MapFile{Data: []byte(`<svg></svg>`)},
		"assets/test.woff":  &fstest.MapFile{Data: []byte{0x00}},
		"assets/test.woff2": &fstest.MapFile{Data: []byte{0x00}},
		"assets/test.png":   &fstest.MapFile{Data: []byte{0x89, 0x50, 0x4E, 0x47}},
	}

	h := NewHandler("1.0.0", "/", mockFS)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	tests := []struct {
		path        string
		contentType string
	}{
		{"/assets/test.js", "application/javascript"},
		{"/assets/test.css", "text/css"},
		{"/assets/test.json", "application/json"},
		{"/assets/test.svg", "image/svg+xml"},
		{"/assets/test.woff", "font/woff"},
		{"/assets/test.woff2", "font/woff2"},
		{"/assets/test.png", "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Header().Get("Content-Type"), tt.contentType)
		})
	}
}
