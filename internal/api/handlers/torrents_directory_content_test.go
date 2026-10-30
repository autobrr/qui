// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
)

func TestParseDirectoryContentMode(t *testing.T) {
	tests := []struct {
		raw  string
		want qbt.DirectoryContentMode
		ok   bool
	}{
		{raw: "", want: qbt.DirectoryContentDirs, ok: true},
		{raw: " ", want: qbt.DirectoryContentDirs, ok: true},
		{raw: "dirs", want: qbt.DirectoryContentDirs, ok: true},
		{raw: "files", want: qbt.DirectoryContentFiles, ok: true},
		{raw: "all", want: qbt.DirectoryContentAll, ok: true},
		{raw: "bogus", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, ok := parseDirectoryContentMode(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseDirectoryContentMode(%q) = %q, %v; want %q, %v", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestGetDirectoryContentRejectsUnknownMode(t *testing.T) {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", "1")
	ctx := context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/getDirectoryContent?dirPath=/data&mode=bogus", http.NoBody)
	rec := httptest.NewRecorder()

	// syncManager is nil: validation must reject the mode before any client call.
	(&TorrentsHandler{}).GetDirectoryContent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
