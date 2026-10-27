// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackericons

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"code.dny.dev/ssrf"
	"github.com/stretchr/testify/require"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestService_ListIcons_NormalizesFilenamesAndAddsWWWAlias(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	svc, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)

	iconPath := filepath.Join(dataDir, iconDirName, "MyTracker.COM.PNG")
	require.NoError(t, os.WriteFile(iconPath, testPNG(t), 0o600))

	icons, err := svc.ListIcons(context.Background())
	require.NoError(t, err)

	require.Contains(t, icons, "mytracker.com")
	require.Contains(t, icons, "www.mytracker.com")
	require.NotEmpty(t, icons["mytracker.com"])
	require.Equal(t, icons["mytracker.com"], icons["www.mytracker.com"])
}

func TestService_ListIcons_StripsWWWPrefixAlias(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	svc, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)

	iconPath := filepath.Join(dataDir, iconDirName, "www.Example.ORG.png")
	require.NoError(t, os.WriteFile(iconPath, testPNG(t), 0o600))

	icons, err := svc.ListIcons(context.Background())
	require.NoError(t, err)

	require.Contains(t, icons, "www.example.org")
	require.Contains(t, icons, "example.org")
	require.Equal(t, icons["www.example.org"], icons["example.org"])
}

type waitForContextDoneTransport struct{}

func (waitForContextDoneTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestService_GetIcon_RecordsFailureWhenContextExpiresDuringFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()
		svc, err := NewService(dataDir, "qui-test")
		require.NoError(t, err)

		svc.client.Transport = waitForContextDoneTransport{}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		host := "tracker.example.org"
		_, _ = svc.GetIcon(ctx, host, "")

		// GetIcon can return before the fetch goroutine records the failure.
		synctest.Wait()

		require.False(t, svc.canAttempt(host))
	})
}

// pngTransport serves a valid PNG for every request and counts them.
type pngTransport struct {
	png []byte
}

func (p *pngTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       io.NopCloser(bytes.NewReader(p.png)),
		Request:    req,
	}, nil
}

// nxdomainTransport fails every request the way a dead host does and counts them.
type nxdomainTransport struct {
	calls atomic.Int32
}

func (n *nxdomainTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n.calls.Add(1)
	return nil, &net.DNSError{Err: "no such host", Name: req.URL.Hostname(), IsNotFound: true}
}

func TestFailureCooldown_EscalatesToCap(t *testing.T) {
	t.Parallel()

	require.Equal(t, 30*time.Minute, failureCooldown(1))
	require.Equal(t, time.Hour, failureCooldown(2))
	require.Equal(t, 4*time.Hour, failureCooldown(4))
	require.Equal(t, maxFailureCooldown, failureCooldown(7))
	require.Equal(t, maxFailureCooldown, failureCooldown(100))
}

func TestService_RecordFailure_BacksOffProgressively(t *testing.T) {
	t.Parallel()

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	host := "dead.example.org"
	svc.recordFailure(host)
	svc.recordFailure(host)
	require.False(t, svc.canAttempt(host))

	svc.failureMu.Lock()
	f := svc.failures[host]
	require.Equal(t, 2, f.Attempts)
	// Just inside the second attempt's cooldown stays blocked; just past it opens up.
	f.LastFailure = time.Now().Add(-failureCooldown(2) + time.Minute)
	svc.failures[host] = f
	svc.failureMu.Unlock()
	require.False(t, svc.canAttempt(host))

	svc.failureMu.Lock()
	f.LastFailure = time.Now().Add(-failureCooldown(2) - time.Minute)
	svc.failures[host] = f
	svc.failureMu.Unlock()
	require.True(t, svc.canAttempt(host))
}

func TestService_FailureState_SurvivesRestart(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	svc, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)

	host := "dead.example.org"
	svc.recordFailure(host)
	svc.recordFailure(host)

	reloaded, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)
	require.False(t, reloaded.canAttempt(host))

	reloaded.failureMu.Lock()
	require.Equal(t, 2, reloaded.failures[host].Attempts)
	reloaded.failureMu.Unlock()
}

func TestService_LoadFailures_NullFileKeepsMapUsable(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, iconDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, iconDirName, failuresFileName), []byte("null"), 0o600))

	svc, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)

	require.NotPanics(t, func() { svc.recordFailure("dead.example.org") })
	require.False(t, svc.canAttempt("dead.example.org"))
}

func TestService_GetIcon_StopsCandidateWalkWhenHostDoesNotResolve(t *testing.T) {
	t.Parallel()

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)
	transport := &nxdomainTransport{}
	svc.client.Transport = transport

	host := "cdn.tracker.example.org"
	require.Len(t, generateHostCandidates(host), 4)

	_, err = svc.GetIcon(t.Context(), host, "")
	require.ErrorIs(t, err, ErrIconNotFound)
	require.Equal(t, int32(1), transport.calls.Load())
	require.False(t, svc.canAttempt(host))
}

func TestService_GetIcon_SuccessClearsPersistedFailure(t *testing.T) {
	t.Parallel()

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)
	svc.client.Transport = &pngTransport{png: testPNG(t)}

	host := "flaky.example.org"
	svc.failureMu.Lock()
	svc.failures[host] = failureState{Attempts: 1, LastFailure: time.Now().Add(-time.Hour)}
	svc.saveFailuresLocked()
	svc.failureMu.Unlock()
	persisted, err := os.ReadFile(svc.failuresPath())
	require.NoError(t, err)
	require.Contains(t, string(persisted), host)

	_, err = svc.GetIcon(t.Context(), host, "")
	require.NoError(t, err)

	persisted, err = os.ReadFile(svc.failuresPath())
	require.NoError(t, err)
	require.NotContains(t, string(persisted), host)

	svc.failureMu.Lock()
	require.Empty(t, svc.failures)
	svc.failureMu.Unlock()
}

func TestService_GetIcon_CachedIconIssuesNoRequest(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	svc, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)
	transport := &nxdomainTransport{}
	svc.client.Transport = transport

	host := "cached.example.org"
	require.NoError(t, os.WriteFile(svc.iconPath(host), testPNG(t), 0o600))

	path, err := svc.GetIcon(t.Context(), host, "")
	require.NoError(t, err)
	require.Equal(t, svc.iconPath(host), path)
	require.Equal(t, int32(0), transport.calls.Load())
}

func TestPublicOnly(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"10.0.0.5:443", "169.254.169.254:80", "100.64.0.1:443", "198.18.0.10:443"} {
		require.ErrorIs(t, publicOnly.Safe("tcp4", addr, nil), ssrf.ErrProhibitedIP, addr)
	}
	require.ErrorIs(t, publicOnly.Safe("tcp6", "[::1]:443", nil), ssrf.ErrProhibitedIP)

	// qui allows any port and NAT64.
	require.NoError(t, publicOnly.Safe("tcp4", "1.1.1.1:8443", nil))
	require.NoError(t, safeDial("tcp6", "[64:ff9b::101:101]:443", nil))
}

func TestSafeDial_RefusesNAT64ToPrivateIPv4(t *testing.T) {
	t.Parallel()

	// 64:ff9b::a9fe:a9fe carries 169.254.169.254, and 64:ff9b::a00:5 carries 10.0.0.5.
	for _, addr := range []string{"[64:ff9b::a9fe:a9fe]:80", "[64:ff9b::a00:5]:443"} {
		require.ErrorIs(t, safeDial("tcp6", addr, nil), ssrf.ErrProhibitedIP, addr)
	}
	require.ErrorIs(t, checkPublicHost(t.Context(), "64:ff9b::a9fe:a9fe"), ssrf.ErrProhibitedIP)
}

func TestService_FetchIconBytes_BlocksLocalServer(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	_, _, err = svc.fetchIconBytes(t.Context(), srv.URL+"/favicon.ico")
	require.ErrorIs(t, err, ssrf.ErrProhibitedIP)
	require.Zero(t, hits.Load())
}

func TestService_FetchIconBytes_BlocksRedirectToLocalServer(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer destination.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/favicon.ico", http.StatusFound)
	}))
	defer redirector.Close()

	// The proxy answers the first request with a redirect, so qui sends the
	// second request to the local server without a proxy.
	t.Setenv("http_proxy", "")
	t.Setenv("HTTP_PROXY", redirector.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	_, _, err = svc.fetchIconBytes(t.Context(), "http://1.1.1.1:8080/favicon.ico")
	require.ErrorIs(t, err, ssrf.ErrProhibitedIP)
	require.Zero(t, hits.Load())
}

func TestService_FetchIconBytes_AllowsConfiguredLocalProxy(t *testing.T) {
	icon := testPNG(t)
	var proxiedHost atomic.Pointer[string]
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHost.Store(&r.Host)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(icon)
	}))
	defer proxy.Close()

	t.Setenv("http_proxy", "")
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	// The proxy answers, so qui never dials 1.1.1.1.
	data, _, err := svc.fetchIconBytes(t.Context(), "http://1.1.1.1:8080/favicon.ico")
	require.NoError(t, err)
	require.Equal(t, icon, data)
	require.Equal(t, "1.1.1.1:8080", *proxiedHost.Load())
}

func TestService_FetchIconBytes_RefusesDirectRequestToProxy(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer proxy.Close()

	t.Setenv("http_proxy", "")
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	// Go sends a loopback target direct, so this request does not go through
	// the proxy. It must not reach the proxy as an ordinary request either.
	_, _, err = svc.fetchIconBytes(t.Context(), proxy.URL+"/admin")
	require.ErrorIs(t, err, ssrf.ErrProhibitedIP)
	require.Zero(t, hits.Load())
}

func TestService_FetchIconBytes_ProxyRefusesPrivateTarget(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer proxy.Close()

	t.Setenv("http_proxy", "")
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	svc, err := NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	_, _, err = svc.fetchIconBytes(t.Context(), "http://169.254.169.254/latest/meta-data/")
	require.ErrorIs(t, err, ssrf.ErrProhibitedIP)
	require.Zero(t, hits.Load())
}

func TestCheckPublicHost_ResolvesNames(t *testing.T) {
	t.Parallel()

	// localhost resolves from the hosts file, so this sends no DNS query.
	require.ErrorIs(t, checkPublicHost(t.Context(), "localhost"), ssrf.ErrProhibitedIP)
	// The resolver returns this literal as ::ffff:1.1.1.1.
	require.NoError(t, checkPublicHost(t.Context(), "1.1.1.1"))
	// The IDNA form of the full-width name is localhost.
	require.ErrorIs(t, checkPublicHost(t.Context(), "ｌｏｃａｌｈｏｓｔ"), ssrf.ErrProhibitedIP)
}
