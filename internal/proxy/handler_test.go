// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	"slices"
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

func TestParseCSVQueryValues(t *testing.T) {
	t.Helper()

	queryParams := url.Values{}
	queryParams.Add("filter", "unregistered, tracker_down")
	queryParams.Add("filter", "downloading")
	queryParams.Add("filter", "tracker_down")
	queryParams.Add("filter", "")

	parsed := parseCSVQueryValues(queryParams, "filter")
	require.True(t, slices.Equal([]string{"unregistered", "tracker_down", "downloading"}, parsed))
}

func TestParseCSVQueryValues_PreservesCategoryCasing(t *testing.T) {
	t.Helper()

	queryParams := url.Values{}
	queryParams.Add("category", "TV, Movies")
	queryParams.Add("category", "Movies")

	parsed := parseCSVQueryValues(queryParams, "category")
	require.True(t, slices.Equal([]string{"TV", "Movies"}, parsed))
}

func TestSplitStatusFilters(t *testing.T) {
	t.Helper()

	status, excludeStatus := splitStatusFilters([]string{"Unregistered", "downloading", "tracker_down", "active", "TRACKER_DOWN", "active"})
	require.True(t, slices.Equal([]string{"downloading", "active"}, status))
	require.True(t, slices.Equal([]string{"unregistered", "tracker_down"}, excludeStatus))
}

func TestParseHashesQueryValues(t *testing.T) {
	t.Helper()

	queryParams := url.Values{}
	queryParams.Add("hashes", "abc|def, ghi")
	queryParams.Add("hashes", "all")
	queryParams.Add("hashes", "DEF|jkl")

	hashes := parseHashesQueryValues(queryParams)
	require.True(t, slices.Equal([]string{"ABC", "DEF", "GHI", "JKL"}, hashes))
}

func TestBuildTorrentSearchFilters_EnhancedAndLegacyCompatibility(t *testing.T) {
	t.Helper()

	queryParams := url.Values{}
	queryParams.Add("status", "active, downloading")
	queryParams.Add("excludeStatus", "paused")
	queryParams.Add("filter", "unregistered,tracker_down,seeding")
	queryParams.Add("category", "Anime")
	queryParams.Add("categories", "TV, Movies")
	queryParams.Add("excludeCategories", "archive")
	queryParams.Add("tag", "manual")
	queryParams.Add("tags", "autobrr")
	queryParams.Add("excludeTags", "skip")
	queryParams.Add("trackers", "tracker-a,tracker-b")
	queryParams.Add("excludeTrackers", "tracker-c")
	queryParams.Add("hashes", "abc|def")
	queryParams.Add("hashes", "DEF|ghi")
	queryParams.Set("expr", " ratio > 1 ")

	filters := buildTorrentSearchFilters(queryParams)

	require.True(t, slices.Equal([]string{"active", "downloading", "seeding"}, filters.Status))
	require.True(t, slices.Equal([]string{"paused", "unregistered", "tracker_down"}, filters.ExcludeStatus))
	require.True(t, slices.Equal([]string{"Anime", "TV", "Movies"}, filters.Categories))
	require.True(t, slices.Equal([]string{"archive"}, filters.ExcludeCategories))
	require.True(t, slices.Equal([]string{"manual", "autobrr"}, filters.Tags))
	require.True(t, slices.Equal([]string{"skip"}, filters.ExcludeTags))
	require.True(t, slices.Equal([]string{"tracker-a", "tracker-b"}, filters.Trackers))
	require.True(t, slices.Equal([]string{"tracker-c"}, filters.ExcludeTrackers))
	require.True(t, slices.Equal([]string{"ABC", "DEF", "GHI"}, filters.Hashes))
	require.Equal(t, "ratio > 1", filters.Expr)
}
