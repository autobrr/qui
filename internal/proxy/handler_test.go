package proxy

import (
	"context"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
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
		baseCase := baseCase

		t.Run(baseCase.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil, nil, baseCase.baseURL)
			require.NotNil(t, h)

			for _, tc := range instanceCases {
				tc := tc

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

func TestHandlerShouldHandleSearch(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, "/")

	tests := []struct {
		name     string
		method   string
		path     string
		query    string
		expected bool
	}{
		{
			name:     "GET request with search parameter",
			method:   "GET",
			path:     "/api/v2/torrents/info",
			query:    "search=ubuntu",
			expected: true,
		},
		{
			name:     "GET request without search parameter",
			method:   "GET",
			path:     "/api/v2/torrents/info",
			query:    "",
			expected: false,
		},
		{
			name:     "POST request with search parameter",
			method:   "POST",
			path:     "/api/v2/torrents/info",
			query:    "search=ubuntu",
			expected: false,
		},
		{
			name:     "GET request to different endpoint",
			method:   "GET",
			path:     "/api/v2/app/version",
			query:    "search=ubuntu",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := tt.path
			if tt.query != "" {
				reqURL += "?" + tt.query
			}
			req := httptest.NewRequest(tt.method, reqURL, nil)
			result := h.shouldHandleSearch(req)
			require.Equal(t, tt.expected, result)
		})
	}
}
