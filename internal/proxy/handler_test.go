// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestHandlerRewriteRequest_PathJoining(t *testing.T) {
	t.Helper()

	const (
		apiKey     = "abc123"
		instanceID = 7
		clientName = "autobrr"
	)

	baseCases := []struct {
		name        string
		baseURL     string
		requestPath string
	}{
		{
			name:        "root base",
			baseURL:     "/",
			requestPath: "/proxy/" + apiKey + "/api/v2/app/webapiVersion",
		},
		{
			name:        "custom base",
			baseURL:     "/qui/",
			requestPath: "/qui/proxy/" + apiKey + "/api/v2/app/webapiVersion",
		},
	}

	instanceCases := []struct {
		name         string
		instanceHost string
		expectedPath string
	}{
		{
			name:         "with sub-path",
			instanceHost: "https://example.com/qbittorrent",
			expectedPath: "/qbittorrent/api/v2/app/webapiVersion",
		},
		{
			name:         "with sub-path and port",
			instanceHost: "http://192.0.2.10:8080/qbittorrent",
			expectedPath: "/qbittorrent/api/v2/app/webapiVersion",
		},
		{
			name:         "root host",
			instanceHost: "https://example.com",
			expectedPath: "/api/v2/app/webapiVersion",
		},
	}

	for _, baseCase := range baseCases {

		t.Run(baseCase.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil, nil, nil, nil, baseCase.baseURL)
			require.NotNil(t, h)

			for _, tc := range instanceCases {

				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest("GET", baseCase.requestPath, nil)

					routeCtx := chi.NewRouteContext()
					routeCtx.URLParams.Add("api-key", apiKey)
					ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)

					instanceURL, err := url.Parse(tc.instanceHost)
					require.NoError(t, err)

					proxyCtx := &proxyContext{
						instanceID:  instanceID,
						instanceURL: instanceURL,
					}

					ctx = context.WithValue(ctx, ClientAPIKeyContextKey, &models.ClientAPIKey{
						ClientName: clientName,
						InstanceID: instanceID,
					})
					ctx = context.WithValue(ctx, InstanceIDContextKey, instanceID)
					ctx = context.WithValue(ctx, proxyContextKey, proxyCtx)

					req = req.WithContext(ctx)
					outReq := req.Clone(ctx)

					pr := &httputil.ProxyRequest{
						In:  req,
						Out: outReq,
					}

					h.rewriteRequest(pr)

					require.Equal(t, tc.expectedPath, pr.Out.URL.Path)
					require.Equal(t, tc.expectedPath, pr.Out.URL.RawPath)
					require.Equal(t, instanceURL.Host, pr.Out.URL.Host)
				})
			}
		})
	}
}

// Note: Intercept logic is now handled by chi routes, not by dynamic checking

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandleSyncMainDataCapturesBodyWithoutLeadingZeros(t *testing.T) {
	t.Helper()

	handler := NewHandler(nil, nil, nil, nil, nil, nil, "/")
	require.NotNil(t, handler)

	payload := []byte(`{"rid":1,"full_update":false}`)

	handler.proxy.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(len(payload)),
			Body:          io.NopCloser(bytes.NewReader(payload)),
			Header:        make(http.Header),
			Request:       req,
		}
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/abc123/sync/maindata", nil)

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("api-key", "abc123")

	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	ctx = context.WithValue(ctx, ClientAPIKeyContextKey, &models.ClientAPIKey{
		ClientName: "test-client",
		InstanceID: 1,
	})
	ctx = context.WithValue(ctx, InstanceIDContextKey, 1)

	instanceURL, err := url.Parse("http://qbittorrent.example")
	require.NoError(t, err)

	ctx = context.WithValue(ctx, proxyContextKey, &proxyContext{
		instanceID:  1,
		instanceURL: instanceURL,
	})

	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	var parseErrorLogged atomic.Bool

	origLogger := log.Logger
	log.Logger = log.Logger.Hook(zerolog.HookFunc(func(e *zerolog.Event, level zerolog.Level, msg string) {
		if level == zerolog.ErrorLevel && msg == "Failed to parse sync/maindata response" {
			parseErrorLogged.Store(true)
		}
	}))
	defer func() {
		log.Logger = origLogger
	}()

	handler.handleSyncMainData(rec, req)

	require.False(t, parseErrorLogged.Load(), "expected sync/maindata response to parse successfully")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, payload, rec.Body.Bytes())
}

func TestHandler_ProxyUsesInstanceHTTPClientTransport(t *testing.T) {
	t.Helper()

	handler := NewHandler(nil, nil, nil, nil, nil, nil, "/")
	require.NotNil(t, handler)

	rt, ok := handler.proxy.Transport.(*RetryTransport)
	require.True(t, ok, "expected handler to configure RetryTransport")
	require.NotNil(t, rt.baseSelector, "expected RetryTransport selector to be configured")

	var selectedCalled atomic.Bool
	selected := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		selectedCalled.Store(true)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/v2/torrents/add", strings.NewReader("x"))
	ctx := context.WithValue(req.Context(), proxyContextKey, &proxyContext{
		httpClient: &http.Client{Transport: selected},
	})
	req = req.WithContext(ctx)

	resp, err := handler.proxy.Transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, selectedCalled.Load(), "expected instance transport to be used")
	require.NoError(t, resp.Body.Close())
}

func TestInjectBaseTag(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		baseURL  string
		expected string
	}{
		{
			name:     "basic HTML with head tag",
			html:     "<html><head><title>Test</title></head><body></body></html>",
			baseURL:  "/proxy/abc123/",
			expected: "<html><head><base href=\"/proxy/abc123/\"><title>Test</title></head><body></body></html>",
		},
		{
			name:     "HTML with existing base tag",
			html:     "<html><head><base href=\"/other/\"><title>Test</title></head><body></body></html>",
			baseURL:  "/proxy/abc123/",
			expected: "<html><head><base href=\"/other/\"><title>Test</title></head><body></body></html>", // Should not inject
		},
		{
			name:     "HTML without head tag",
			html:     "<html><body>No head tag</body></html>",
			baseURL:  "/proxy/abc123/",
			expected: "<html><body>No head tag</body></html>", // Should not inject
		},
		{
			name:     "HTML with head attributes",
			html:     "<html><head lang=\"en\"><title>Test</title></head><body></body></html>",
			baseURL:  "/proxy/def456/",
			expected: "<html><head lang=\"en\"><base href=\"/proxy/def456/\"><title>Test</title></head><body></body></html>",
		},
		{
			name:     "HTML with uppercase HEAD tag",
			html:     "<html><HEAD><title>Test</title></HEAD><body></body></html>",
			baseURL:  "/proxy/xyz789/",
			expected: "<html><HEAD><base href=\"/proxy/xyz789/\"><title>Test</title></HEAD><body></body></html>",
		},
		{
			name:     "HTML with mixed case head tag",
			html:     "<html><Head><title>Test</title></Head><body></body></html>",
			baseURL:  "/qui/proxy/test123/",
			expected: "<html><Head><base href=\"/qui/proxy/test123/\"><title>Test</title></Head><body></body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectBaseTag([]byte(tt.html), tt.baseURL)
			require.Equal(t, tt.expected, string(result))
		})
	}
}

func TestModifyResponse_HTMLContent(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, nil, nil, "/")
	require.NotNil(t, handler)

	tests := []struct {
		name            string
		contentType     string
		body            string
		apiKey          string
		expectModified  bool
		expectedBaseURL string
	}{
		{
			name:            "HTML response with text/html content type",
			contentType:     "text/html",
			body:            "<html><head><title>Test</title></head><body></body></html>",
			apiKey:          "abc123",
			expectModified:  true,
			expectedBaseURL: "/proxy/abc123/",
		},
		{
			name:            "HTML response with text/html; charset=utf-8",
			contentType:     "text/html; charset=utf-8",
			body:            "<html><head><title>Test</title></head><body></body></html>",
			apiKey:          "def456",
			expectModified:  true,
			expectedBaseURL: "/proxy/def456/",
		},
		{
			name:           "JSON response should not be modified",
			contentType:    "application/json",
			body:           `{"status":"ok"}`,
			apiKey:         "abc123",
			expectModified: false,
		},
		{
			name:           "JavaScript response should not be modified",
			contentType:    "application/javascript",
			body:           "console.log('test');",
			apiKey:         "abc123",
			expectModified: false,
		},
		{
			name:           "CSS response should not be modified",
			contentType:    "text/css",
			body:           "body { margin: 0; }",
			apiKey:         "abc123",
			expectModified: false,
		},
		{
			name:           "Image response should not be modified",
			contentType:    "image/png",
			body:           "binary data",
			apiKey:         "abc123",
			expectModified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock response
			resp := &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(strings.NewReader(tt.body)),
				Header:        make(http.Header),
				ContentLength: int64(len(tt.body)),
			}
			resp.Header.Set("Content-Type", tt.contentType)

			// Create a mock request with API key in route context
			req := httptest.NewRequest("GET", "/proxy/"+tt.apiKey+"/", nil)
			routeCtx := chi.NewRouteContext()
			routeCtx.URLParams.Add("api-key", tt.apiKey)
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
			req = req.WithContext(ctx)
			resp.Request = req

			// Modify the response
			err := handler.modifyResponse(resp)
			require.NoError(t, err)

			// Read the modified body
			bodyBytes, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			if tt.expectModified {
				// Should contain the base tag
				require.Contains(t, string(bodyBytes), "<base href=\""+tt.expectedBaseURL+"\">")
			} else {
				// Should be unchanged
				require.Equal(t, tt.body, string(bodyBytes))
			}
		})
	}
}

func TestModifyResponse_WithCustomBasePath(t *testing.T) {
	// Test with a custom base path like /qui/
	handler := NewHandler(nil, nil, nil, nil, nil, nil, "/qui/")
	require.NotNil(t, handler)

	body := "<html><head><title>Test</title></head><body></body></html>"
	apiKey := "test123"

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(strings.NewReader(body)),
		Header:        make(http.Header),
		ContentLength: int64(len(body)),
	}
	resp.Header.Set("Content-Type", "text/html")

	req := httptest.NewRequest("GET", "/qui/proxy/"+apiKey+"/", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("api-key", apiKey)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	req = req.WithContext(ctx)
	resp.Request = req

	err := handler.modifyResponse(resp)
	require.NoError(t, err)

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Should contain the base tag with the custom base path
	require.Contains(t, string(bodyBytes), "<base href=\"/qui/proxy/test123/\">")
}

