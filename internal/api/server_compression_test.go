// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
)

func TestServerCompression(t *testing.T) {
	deps := newTestDependencies(t)
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("logPath = 'qui.log'\n"), 0o600))
	var err error
	deps.Config, err = config.New(configDir, "test")
	require.NoError(t, err)
	deps.Config.Config.AuthDisabled = true
	deps.Config.Config.IAcknowledgeThisIsABadIdea = true
	deps.Config.Config.AuthDisabledAllowedCIDRs = []string{"127.0.0.1/32"}
	fileData := bytes.Repeat([]byte("sample file content\n"), 200)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "qui.log"), fileData, 0o600))
	for day := 11; day <= 30; day++ {
		name := "qui-2026-01-" + strconv.Itoa(day) + "T00-00-00.000.log"
		require.NoError(t, os.WriteFile(filepath.Join(configDir, name), fileData, 0o600))
	}

	for _, baseURL := range []string{"/", "/qui/"} {
		t.Run(baseURL, func(t *testing.T) {
			deps.Config.Config.BaseURL = baseURL
			router, err := NewServer(deps).Handler()
			require.NoError(t, err)
			apiPrefix := strings.TrimSuffix(baseURL, "/") + "/api"

			t.Run("torrent download", func(t *testing.T) {
				path := apiPrefix + "/instances/7/torrents/sample/files/3/download"
				handler := withRouteRegisteredMiddleware(t, router, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/octet-stream")
					http.ServeContent(w, r, "sample.bin", time.Time{}, bytes.NewReader(fileData))
				}))
				for _, tc := range []struct {
					name         string
					rangeHeader  string
					status       int
					body         []byte
					contentRange string
				}{
					{"full", "", http.StatusOK, fileData, ""},
					{"range", "bytes=100-199", http.StatusPartialContent, fileData[100:200], "bytes 100-199/" + strconv.Itoa(len(fileData))},
				} {
					t.Run(tc.name, func(t *testing.T) {
						req := compressionRequest(t, path, "gzip")
						req.Header.Set("Range", tc.rangeHeader)
						res := httptest.NewRecorder()
						handler.ServeHTTP(res, req)
						require.Equal(t, tc.status, res.Code)
						require.Equal(t, tc.body, res.Body.Bytes())
						require.Empty(t, res.Header().Get("Content-Encoding"))
						require.Equal(t, strconv.Itoa(len(tc.body)), res.Header().Get("Content-Length"))
						require.Equal(t, tc.contentRange, res.Header().Get("Content-Range"))
					})
				}
			})

			for _, path := range []string{"/stream", "/logs/stream", "/instances/7/rss/events"} {
				t.Run(path, func(t *testing.T) {
					for _, accept := range []string{"text/event-stream", ""} {
						t.Run("accept="+accept, func(t *testing.T) {
							const frame = "event: connected\ndata: {}\n\n"
							res := &streamCompressionRecorder{ResponseRecorder: httptest.NewRecorder()}
							handler := withRouteRegisteredMiddleware(t, router, apiPrefix+path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
								w.Header().Set("Content-Type", "text/event-stream")
								_, err := io.WriteString(w, frame)
								assert.NoError(t, err)
								controller := http.NewResponseController(w)
								assert.NoError(t, controller.Flush())
								assert.Equal(t, frame, res.bodyAtFlush, "the event should arrive before the handler returns")
								assert.NoError(t, controller.SetWriteDeadline(time.Now().Add(time.Second)))
							}))
							req := compressionRequest(t, apiPrefix+path, "gzip")
							req.Header.Set("Accept", accept)
							handler.ServeHTTP(res, req)
							require.Equal(t, http.StatusOK, res.Code)
							require.Empty(t, res.Header().Get("Content-Encoding"))
							require.True(t, res.deadlineSet)
						})
					}
				})
			}

			for _, path := range []string{"/openapi.json", "/logs/files", "/logs/files/qui.log"} {
				t.Run(path, func(t *testing.T) {
					plain := httptest.NewRecorder()
					router.ServeHTTP(plain, compressionRequest(t, apiPrefix+path, "identity"))
					require.Equal(t, http.StatusOK, plain.Code)
					require.Empty(t, plain.Header().Get("Content-Encoding"))
					require.Greater(t, plain.Body.Len(), 1024)
					if path == "/logs/files/qui.log" {
						require.Equal(t, fileData, plain.Body.Bytes())
					} else {
						require.True(t, json.Valid(plain.Body.Bytes()))
					}
					for _, accept := range []string{"application/json", "text/event-stream"} {
						req := compressionRequest(t, apiPrefix+path, "gzip")
						req.Header.Set("Accept", accept)
						res := httptest.NewRecorder()
						router.ServeHTTP(res, req)
						require.Equal(t, http.StatusOK, res.Code)
						require.Equal(t, "gzip", res.Header().Get("Content-Encoding"))
						reader, err := gzip.NewReader(res.Body)
						require.NoError(t, err)
						body, err := io.ReadAll(reader)
						require.NoError(t, err)
						require.NoError(t, reader.Close())
						require.Equal(t, plain.Body.Bytes(), body)
					}
				})
			}

			t.Run("small JSON", func(t *testing.T) {
				res := httptest.NewRecorder()
				router.ServeHTTP(res, compressionRequest(t, apiPrefix+"/auth/me", "gzip"))
				require.Equal(t, http.StatusOK, res.Code)
				require.JSONEq(t, `{"username":"admin","auth_method":"none"}`, res.Body.String())
				require.Empty(t, res.Header().Get("Content-Encoding"))
			})

			for _, path := range []string{"/instances/7/torrents/sample/files", "/instances/7/torrents/sample/files/3/mediainfo", "/instances/7/rss/items"} {
				t.Run("neighbor "+path, func(t *testing.T) {
					data := `{"sample":"` + strings.Repeat("content ", 200) + `"}`
					handler := withRouteRegisteredMiddleware(t, router, apiPrefix+path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_, err := io.WriteString(w, data)
						assert.NoError(t, err)
					}))
					res := httptest.NewRecorder()
					handler.ServeHTTP(res, compressionRequest(t, apiPrefix+path, "gzip"))
					require.Equal(t, "gzip", res.Header().Get("Content-Encoding"))
				})
			}
		})
	}
}

func TestServerUncompressedRoutesRequireAuthentication(t *testing.T) {
	deps := newTestDependencies(t)
	for _, baseURL := range []string{"/", "/qui/"} {
		deps.Config.Config.BaseURL = baseURL
		router, err := NewServer(deps).Handler()
		require.NoError(t, err)
		for _, path := range []string{"/instances/7/torrents/sample/files/3/download", "/stream", "/logs/stream", "/instances/7/rss/events"} {
			t.Run(baseURL+path, func(t *testing.T) {
				res := httptest.NewRecorder()
				router.ServeHTTP(res, compressionRequest(t, strings.TrimSuffix(baseURL, "/")+"/api"+path, "gzip"))
				require.Equal(t, http.StatusForbidden, res.Code)
			})
		}
	}
}

func compressionRequest(t *testing.T, path, encoding string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Accept-Encoding", encoding)
	return req
}

// Wrap testHandler with the middleware registered for the route that matches path.
func withRouteRegisteredMiddleware(t *testing.T, router *chi.Mux, path string, testHandler http.Handler) http.Handler {
	t.Helper()
	pattern := router.Find(chi.NewRouteContext(), http.MethodGet, path)
	require.NotEmpty(t, pattern)
	var handler http.Handler
	err := chi.Walk(router, func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && route == pattern {
			require.Nil(t, handler, "route should be registered once")
			handler = chi.Chain(middlewares...).Handler(testHandler)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, handler, "registered route %s", pattern)
	return handler
}

type streamCompressionRecorder struct {
	*httptest.ResponseRecorder
	bodyAtFlush string
	deadlineSet bool
}

func (r *streamCompressionRecorder) Flush() {
	r.bodyAtFlush = r.Body.String()
	r.ResponseRecorder.Flush()
}

func (r *streamCompressionRecorder) SetWriteDeadline(time.Time) error {
	r.deadlineSet = true
	return nil
}
