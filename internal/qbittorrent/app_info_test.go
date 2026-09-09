// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestSupportsProcessInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		webAPIVersion string
		expected      bool
	}{
		{name: "supported web api", webAPIVersion: "2.15.1", expected: true},
		{name: "newer web api", webAPIVersion: "2.16.0", expected: true},
		{name: "older web api", webAPIVersion: "2.11.4", expected: false},
		{name: "empty version", webAPIVersion: "", expected: false},
		{name: "invalid version", webAPIVersion: "not-a-version", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, supportsProcessInfo(tc.webAPIVersion))
		})
	}
}

func TestBuildProcessInfo(t *testing.T) {
	processInfoErr := errors.New("process info unavailable")

	tests := []struct {
		name            string
		webAPIVersion   string
		processInfo     qbt.ProcessInfo
		processInfoErr  error
		wantFetchCalled bool
		wantLaunchTime  int64
		wantProcessInfo bool
	}{
		{
			name:            "includes process info for supported web api",
			webAPIVersion:   "2.15.1",
			processInfo:     qbt.ProcessInfo{LaunchTime: 1769331513},
			wantFetchCalled: true,
			wantLaunchTime:  1769331513,
			wantProcessInfo: true,
		},
		{
			name:            "skips process info for older web api",
			webAPIVersion:   "2.11.4",
			processInfo:     qbt.ProcessInfo{LaunchTime: 1769331513},
			wantFetchCalled: false,
			wantProcessInfo: false,
		},
		{
			name:            "omits process info when supported call fails",
			webAPIVersion:   "2.15.1",
			processInfoErr:  processInfoErr,
			wantFetchCalled: true,
			wantProcessInfo: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fetchCalled := false
			info := buildProcessInfo(tc.webAPIVersion, func() (qbt.ProcessInfo, error) {
				fetchCalled = true
				return tc.processInfo, tc.processInfoErr
			})

			require.Equal(t, tc.wantFetchCalled, fetchCalled)
			if !tc.wantProcessInfo {
				require.Nil(t, info)
				return
			}

			require.NotNil(t, info)
			require.Equal(t, tc.wantLaunchTime, info.LaunchTime)
		})
	}
}

// newAppInfoStub serves the app info endpoints and counts hits per path. A
// non-2xx versionStatus makes the version call fail without retries.
func newAppInfoStub(t *testing.T, versionStatus int) (*Client, func(path string) int) {
	t.Helper()

	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v2/app/version":
			time.Sleep(50 * time.Millisecond) // widen the window so callers overlap
			w.WriteHeader(versionStatus)
			_, _ = w.Write([]byte("v5.1.0"))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.4"))
		case "/api/v2/app/buildInfo":
			_, _ = w.Write([]byte(`{"qt":"6.7.2","libtorrent":"2.0.10","boost":"1.86","openssl":"3.3","zlib":"1.3","bitness":64}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := &Client{
		Client:     qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}),
		instanceID: 1,
	}
	hitCount := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[path]
	}
	return c, hitCount
}

type appInfoResult struct {
	info *AppInfo
	err  error
}

// callGetAppInfoConcurrently runs n overlapping GetAppInfo calls and returns every result.
func callGetAppInfoConcurrently(ctx context.Context, c *Client, n int) []appInfoResult {
	results := make([]appInfoResult, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			results[i].info, results[i].err = c.GetAppInfo(ctx)
		})
	}
	wg.Wait()
	return results
}

func TestGetAppInfoSharesOneRefreshAcrossConcurrentCallers(t *testing.T) {
	c, hitCount := newAppInfoStub(t, http.StatusOK)
	c.appInfoCache = &AppInfo{Version: "4.6.7", WebAPIVersion: "2.11.4"}
	c.appInfoFetchedAt = time.Now().Add(-2 * appInfoCacheTTL) // force the cache stale

	for _, r := range callGetAppInfoConcurrently(t.Context(), c, 8) {
		require.NoError(t, r.err)
		require.NotNil(t, r.info)
		require.Equal(t, "v5.1.0", r.info.Version)
		require.Equal(t, "2.11.4", r.info.WebAPIVersion)
	}
	require.Equal(t, 1, hitCount("/api/v2/app/version"))
	require.Equal(t, 1, hitCount("/api/v2/app/webapiVersion"))
	require.Equal(t, 1, hitCount("/api/v2/app/buildInfo"))
	require.Equal(t, 0, hitCount("/api/v2/app/processInfo"), "2.11.4 predates process info")
}

func TestGetAppInfoServesStaleCacheWhenRefreshFails(t *testing.T) {
	c, hitCount := newAppInfoStub(t, http.StatusInternalServerError)
	c.appInfoCache = &AppInfo{Version: "4.6.7", WebAPIVersion: "2.11.4"}
	c.appInfoFetchedAt = time.Now().Add(-2 * appInfoCacheTTL) // force the cache stale

	for _, r := range callGetAppInfoConcurrently(t.Context(), c, 8) {
		require.NoError(t, r.err)
		require.NotNil(t, r.info)
		require.Equal(t, "4.6.7", r.info.Version)
		require.Equal(t, "2.11.4", r.info.WebAPIVersion)
	}
	require.Equal(t, 1, hitCount("/api/v2/app/version"))
	require.Equal(t, 0, hitCount("/api/v2/app/webapiVersion"), "refresh stops at the first failure")
}

func TestGetAppInfoReturnsErrorWhenNoCacheAndRefreshFails(t *testing.T) {
	c, _ := newAppInfoStub(t, http.StatusInternalServerError)

	info, err := c.GetAppInfo(t.Context())
	require.Error(t, err)
	require.Nil(t, info)
}
