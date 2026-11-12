// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// PeerClientIconProvider returns the bytes of one peer client icon.
type PeerClientIconProvider interface {
	GetIcon(ctx context.Context, name string) ([]byte, string, error)
}

// PeerClientIconHandler serves peer client logos.
type PeerClientIconHandler struct {
	service PeerClientIconProvider
}

func NewPeerClientIconHandler(service PeerClientIconProvider) *PeerClientIconHandler {
	return &PeerClientIconHandler{service: service}
}

// GetPeerClientIcon serves one icon from the fixed list, or 404.
func (h *PeerClientIconHandler) GetPeerClientIcon(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.service.GetIcon(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		// A missing icon can arrive after the retry wait, so the browser must ask again.
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}

	// The headers of GitHub raw: a direct visit to an SVG runs no script.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The URL has no hash, so a release that moves a logo reaches the browser within a week.
	w.Header().Set("Cache-Control", "private, max-age=604800")
	_, _ = w.Write(data) //nolint:gosec // G705: bytes from a hash-checked list, served with a sandbox CSP and nosniff
}
