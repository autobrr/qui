// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type PeerClientIconProvider interface {
	GetIcon(ctx context.Context, name string) ([]byte, string, error)
}

type PeerClientIconHandler struct {
	service PeerClientIconProvider
}

func NewPeerClientIconHandler(service PeerClientIconProvider) *PeerClientIconHandler {
	return &PeerClientIconHandler{service: service}
}

func (h *PeerClientIconHandler) GetPeerClientIcon(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.service.GetIcon(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=604800")
	_, _ = w.Write(data) //nolint:gosec // G705: bytes from a hash-checked list, served with a sandbox CSP and nosniff
}
