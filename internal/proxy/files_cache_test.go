// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
)

// regression: #3934
func TestProxyClearsCachedFilesForUppercaseHash(t *testing.T) {
	t.Parallel()

	upper := strings.ToUpper(proxyHashA)
	tests := []struct {
		endpoint string
		handle   func(*Handler) http.HandlerFunc
		body     url.Values
	}{
		{
			endpoint: "torrents/renameFile",
			handle:   func(h *Handler) http.HandlerFunc { return h.handleRenameFile },
			body:     url.Values{"hash": {upper}, "oldPath": {"Show.S01E01.mkv"}, "newPath": {"Show.S01E01.Renamed.mkv"}},
		},
		{
			endpoint: "torrents/setLocation",
			handle:   func(h *Handler) http.HandlerFunc { return h.handleSetLocation },
			body:     url.Values{"hashes": {upper}, "location": {"/downloads/movies"}},
		},
		{
			endpoint: "torrents/delete",
			handle:   func(h *Handler) http.HandlerFunc { return h.handleDeleteTorrents },
			body:     url.Values{"hashes": {upper}, "deleteFiles": {"false"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			t.Parallel()
			r := newProxyRigWorker(t, false)
			files := filesmanager.NewService(r.db)
			r.handler.syncManager.SetFilesManager(files)
			require.NoError(t, files.CacheFiles(t.Context(), r.instanceID, proxyHashA, qbittorrent.CachedFilesWrite{Files: qbt.TorrentFiles{{Name: "Show.S01E01.mkv", Size: 1}}}))
			cached, err := files.GetCachedFiles(t.Context(), r.instanceID, proxyHashA)
			require.NoError(t, err)
			require.NotNil(t, cached, "the file list must be cached before the proxied call")

			r.post(tt.handle(r.handler), tt.endpoint, tt.body.Encode())

			cached, err = files.GetCachedFiles(t.Context(), r.instanceID, proxyHashA)
			require.NoError(t, err)
			require.Nil(t, cached, "a proxied %s with an uppercase hash must clear the cached file list", tt.endpoint)
		})
	}
}
