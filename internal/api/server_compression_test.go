// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestCompressionMiddlewareTorrentContentDownload(t *testing.T) {
	fileData := bytes.Repeat([]byte("sample file content"), 200)
	compressor, err := httpcompression.DefaultAdapter(httpcompression.MinSize(1024))
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(compressionMiddleware(compressor, "/qui/"))
	router.Get("/qui/api/instances/{instanceID}/torrents/{hash}/files/{fileIndex}/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "sample.bin", time.Time{}, bytes.NewReader(fileData))
	})

	path := "/qui/api/instances/7/torrents/sample/files/3/download"
	t.Run("full", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		require.Equal(t, http.StatusOK, res.Code)
		require.Equal(t, fileData, res.Body.Bytes())
		require.Empty(t, res.Header().Get("Content-Encoding"))
		require.Equal(t, strconv.Itoa(len(fileData)), res.Header().Get("Content-Length"))
		require.Empty(t, res.Header().Get("Content-Range"))
	})

	t.Run("range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("Range", "bytes=100-199")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		require.Equal(t, http.StatusPartialContent, res.Code)
		require.Equal(t, fileData[100:200], res.Body.Bytes())
		require.Empty(t, res.Header().Get("Content-Encoding"))
		require.Equal(t, "100", res.Header().Get("Content-Length"))
		require.Equal(t, "bytes 100-199/"+strconv.Itoa(len(fileData)), res.Header().Get("Content-Range"))
	})
}
