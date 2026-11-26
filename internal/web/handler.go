// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package web

import (
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	fs      fs.FS
	baseURL string
	version string
}

func init() {
	_ = mime.AddExtensionType(".js", "application/javascript")
	_ = mime.AddExtensionType(".css", "text/css")
	_ = mime.AddExtensionType(".html", "text/html")
	_ = mime.AddExtensionType(".json", "application/json")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".woff", "font/woff")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
}

func NewHandler(version, baseURL string, embedFS fs.FS) *Handler {
	return &Handler{
		fs:      embedFS,
		baseURL: baseURL,
		version: version,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	if h.fs == nil {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Frontend not built. Run 'make frontend' to build the web UI.", http.StatusNotFound)
		})
		return
	}

	r.Get("/assets/*", h.serveAssets)

	r.Get("/registerSW.js", h.serveAssets)
	r.Get("/sw.js", h.serveAssets)
	r.Get("/workbox-{hash}.js", h.serveAssets)
	r.Get("/workbox-{hash}.js.map", h.serveAssets)
	r.Get("/manifest.webmanifest", h.serveAssets)

	r.Get("/qui.png", h.serveAssets)
	r.Get("/favicon.png", h.serveAssets)
	r.Get("/apple-touch-icon.png", h.serveAssets)
	r.Get("/pwa-192x192.png", h.serveAssets)
	r.Get("/pwa-512x512.png", h.serveAssets)
	r.Get("/swizzin.png", h.serveAssets)
	r.Get("/napster.png", h.serveAssets)

	spaRoot := h.baseURL
	if spaRoot == "" {
		spaRoot = "/"
	}
	r.Get("/index.html", func(w http.ResponseWriter, r *http.Request) {
		target := spaRoot
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently) //nolint:gosec // G710: same-origin relative target built from the configured base path; the query is carried through unchanged
	})

	r.Get("/", h.serveSPA)
	r.Get("/*", h.serveSPA)
}

func (h *Handler) serveAssets(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	if h.baseURL != "" && h.baseURL != "/" {
		baseWithoutSlash := strings.Trim(h.baseURL, "/")
		path = strings.TrimPrefix(path, baseWithoutSlash+"/")
	}

	file, err := h.fs.Open(path)
	if err != nil {
		log.Debug().
			Str("requested_url", r.URL.Path).
			Str("tried_path", path).
			Err(err).
			Msg("Asset not found")
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(path)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		switch ext {
		case ".js":
			contentType = "application/javascript"
		case ".css":
			contentType = "text/css"
		case ".html":
			contentType = "text/html"
		case ".json":
			contentType = "application/json"
		case ".png":
			contentType = "image/png"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".svg":
			contentType = "image/svg+xml"
		case ".woff":
			contentType = "font/woff"
		case ".woff2":
			contentType = "font/woff2"
		default:
			contentType = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", contentType)

	if strings.Contains(path, "-") && (ext == ".js" || ext == ".css") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	needsModification := h.baseURL != "" && h.baseURL != "/" &&
		(path == "registerSW.js" || path == "sw.js" || path == "manifest.webmanifest")

	if needsModification {
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		modifiedContent := string(content)
		basePrefix := strings.TrimSuffix(h.baseURL, "/")

		switch path {
		case "registerSW.js":
			modifiedContent = strings.ReplaceAll(modifiedContent, `'/sw.js'`, `'`+basePrefix+`/sw.js'`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"/sw.js"`, `"`+basePrefix+`/sw.js"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `scope: '/'`, `scope: '`+basePrefix+`/'`)
		case "manifest.webmanifest":
			modifiedContent = strings.ReplaceAll(modifiedContent, `"start_url":"/"`, `"start_url":"`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"start_url": "/"`, `"start_url": "`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"scope":"/"`, `"scope":"`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"scope": "/"`, `"scope": "`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"src":"pwa-`, `"src":"`+basePrefix+`/pwa-`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"src": "pwa-`, `"src": "`+basePrefix+`/pwa-`)
		}

		_, _ = w.Write([]byte(modifiedContent))
	} else {
		http.ServeContent(w, r, path, stat.ModTime(), file.(io.ReadSeeker))
	}
}

func (h *Handler) serveSPA(w http.ResponseWriter, r *http.Request) {
	file, err := h.fs.Open("index.html")
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	baseURL := h.baseURL
	if baseURL == "" {
		baseURL = "/"
	}

	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	scriptTag := fmt.Sprintf(`<script>window.__QUI_BASE_URL__=%q;window.__QUI_VERSION__=%q;</script>`, baseURL, h.version)

	modifiedContent := strings.Replace(
		string(content),
		"</head>",
		scriptTag+"</head>",
		1,
	)

	if baseURL != "/" {
		basePrefix := strings.TrimSuffix(baseURL, "/")

		modifiedContent = strings.ReplaceAll(modifiedContent, `src="/assets/`, `src="`+basePrefix+`/assets/`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/assets/`, `href="`+basePrefix+`/assets/`)

		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/favicon.png"`, `href="`+basePrefix+`/favicon.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/apple-touch-icon.png"`, `href="`+basePrefix+`/apple-touch-icon.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/qui.png"`, `href="`+basePrefix+`/qui.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `src="/swizzin.png"`, `src="`+basePrefix+`/swizzin.png"`)

		modifiedContent = strings.ReplaceAll(modifiedContent, `src="/registerSW.js"`, `src="`+basePrefix+`/registerSW.js"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/manifest.webmanifest"`, `href="`+basePrefix+`/manifest.webmanifest"`)

		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/pwa-192x192.png"`, `href="`+basePrefix+`/pwa-192x192.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/pwa-512x512.png"`, `href="`+basePrefix+`/pwa-512x512.png"`)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(modifiedContent))
}
