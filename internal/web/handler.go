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
	// Ensure MIME types are properly registered
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".html", "text/html")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".woff", "font/woff")
	mime.AddExtensionType(".woff2", "font/woff2")
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
		// No frontend available - this should only happen in development
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Frontend not built. Run 'make frontend' to build the web UI.", http.StatusNotFound)
		})
		return
	}

	// Serve static assets with proper MIME types
	r.Get("/assets/*", h.serveAssets)

	// Serve PWA files
	r.Get("/registerSW.js", h.serveAssets)
	r.Get("/sw.js", h.serveAssets)
	r.Get("/workbox-{hash}.js", h.serveAssets)
	r.Get("/workbox-{hash}.js.map", h.serveAssets)
	r.Get("/manifest.webmanifest", h.serveAssets)

	// Serve favicon and other root assets
	r.Get("/qui.png", h.serveAssets)
	r.Get("/favicon.png", h.serveAssets)
	r.Get("/apple-touch-icon.png", h.serveAssets)
	r.Get("/pwa-192x192.png", h.serveAssets)
	r.Get("/pwa-512x512.png", h.serveAssets)
	r.Get("/swizzin.png", h.serveAssets)
	r.Get("/napster.png", h.serveAssets)

	// SPA routes
	r.Get("/", h.serveSPA)
	r.Get("/*", h.serveSPA)
}

func (h *Handler) serveAssets(w http.ResponseWriter, r *http.Request) {
	// Get the file path, removing the leading slash
	path := strings.TrimPrefix(r.URL.Path, "/")

	// If we have a base URL, it might still be in the path
	if h.baseURL != "" && h.baseURL != "/" {
		baseWithoutSlash := strings.Trim(h.baseURL, "/")
		path = strings.TrimPrefix(path, baseWithoutSlash+"/")
	}

	// Try to open the file from embedded FS
	file, err := h.fs.Open(path)
	if err != nil {
		// Log the error for debugging
		log.Debug().
			Str("requested_url", r.URL.Path).
			Str("tried_path", path).
			Err(err).
			Msg("Asset not found")
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Set proper content type based on file extension
	ext := filepath.Ext(path)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		// Fallback for common web assets
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

	// Set cache headers for assets (1 year for immutable assets with hash in filename)
	if strings.Contains(path, "-") && (ext == ".js" || ext == ".css") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	// For specific files that need dynamic path modification when using baseURL
	needsModification := h.baseURL != "" && h.baseURL != "/" &&
		(path == "registerSW.js" || path == "sw.js" || path == "manifest.webmanifest")

	if needsModification {
		// Read the file content for modification
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		modifiedContent := string(content)
		basePrefix := strings.TrimSuffix(h.baseURL, "/")

		// Modify service worker files to use correct base URL
		if path == "registerSW.js" {
			// Fix service worker registration paths
			modifiedContent = strings.ReplaceAll(modifiedContent, `'/sw.js'`, `'`+basePrefix+`/sw.js'`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"/sw.js"`, `"`+basePrefix+`/sw.js"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `scope: '/'`, `scope: '`+basePrefix+`/'`)
		} else if path == "manifest.webmanifest" {
			// Fix manifest paths (icons, start_url, etc.)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"start_url":"/"`, `"start_url":"`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"start_url": "/"`, `"start_url": "`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"scope":"/"`, `"scope":"`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"scope": "/"`, `"scope": "`+basePrefix+`/"`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"src":"pwa-`, `"src":"`+basePrefix+`/pwa-`)
			modifiedContent = strings.ReplaceAll(modifiedContent, `"src": "pwa-`, `"src": "`+basePrefix+`/pwa-`)
		}

		w.Write([]byte(modifiedContent))
	} else {
		// Serve the file normally
		http.ServeContent(w, r, path, stat.ModTime(), file.(io.ReadSeeker))
	}
}

func (h *Handler) serveSPA(w http.ResponseWriter, r *http.Request) {
	// Always serve index.html for SPA routes
	file, err := h.fs.Open("index.html")
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Read the entire file to inject base URL
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Inject base URL as a global variable before other scripts
	// This allows the frontend to access it without needing a rebuild
	baseURL := h.baseURL
	if baseURL == "" {
		baseURL = "/"
	}

	// Ensure baseURL ends with /
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	// Create the script tag to inject both base URL and version metadata
	scriptTag := fmt.Sprintf(`<script>window.__QUI_BASE_URL__=%q;window.__QUI_VERSION__=%q;</script>`, baseURL, h.version)

	// Inject before the closing </head> tag
	modifiedContent := strings.Replace(
		string(content),
		"</head>",
		scriptTag+"</head>",
		1,
	)

	// If we have a base URL other than /, we need to fix asset paths
	if baseURL != "/" {
		basePrefix := strings.TrimSuffix(baseURL, "/")

		// Fix asset paths
		modifiedContent = strings.ReplaceAll(modifiedContent, `src="/assets/`, `src="`+basePrefix+`/assets/`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/assets/`, `href="`+basePrefix+`/assets/`)

		// Fix favicon and app icons
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/favicon.png"`, `href="`+basePrefix+`/favicon.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/apple-touch-icon.png"`, `href="`+basePrefix+`/apple-touch-icon.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/qui.png"`, `href="`+basePrefix+`/qui.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `src="/swizzin.png"`, `src="`+basePrefix+`/swizzin.png"`)

		// Fix PWA files
		modifiedContent = strings.ReplaceAll(modifiedContent, `src="/registerSW.js"`, `src="`+basePrefix+`/registerSW.js"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/manifest.webmanifest"`, `href="`+basePrefix+`/manifest.webmanifest"`)

		// Fix PWA icons (if they exist in the HTML)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/pwa-192x192.png"`, `href="`+basePrefix+`/pwa-192x192.png"`)
		modifiedContent = strings.ReplaceAll(modifiedContent, `href="/pwa-512x512.png"`, `href="`+basePrefix+`/pwa-512x512.png"`)
	}

	// Set content type and write response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(modifiedContent))
}
