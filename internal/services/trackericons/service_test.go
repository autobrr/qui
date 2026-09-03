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
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
	dataDir := t.TempDir()
	svc, err := NewService(dataDir, "qui-test")
	require.NoError(t, err)

	svc.client.Transport = waitForContextDoneTransport{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	host := "tracker.example.org"
	_, _ = svc.GetIcon(ctx, host, "")

	deadline := time.Now().Add(500 * time.Millisecond)
	for svc.canAttempt(host) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	require.False(t, svc.canAttempt(host))
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
