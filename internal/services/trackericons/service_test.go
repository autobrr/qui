// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackericons

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
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
