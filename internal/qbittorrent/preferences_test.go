// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestGetAppPreferencesServesStaleCacheWhenRefreshFails(t *testing.T) {
	c := &Client{
		Client:     qbt.NewClient(qbt.Config{Host: "http://127.0.0.1:0"}),
		instanceID: 1,
	}
	c.preferencesCache = &qbt.AppPreferences{AnnounceIP: "203.0.113.7"}
	c.preferencesFetchedAt = time.Now().Add(-2 * appPreferencesCacheTTL) // force the cache stale

	// A canceled context makes the refresh fail immediately, standing in for a
	// saturated qBittorrent WebUI that times out every request.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	prefs, err := c.GetAppPreferences(ctx)
	require.NoError(t, err)
	require.NotNil(t, prefs)
	require.Equal(t, "203.0.113.7", prefs.AnnounceIP)
}

func TestGetAppPreferencesReturnsErrorWhenNoCacheAndRefreshFails(t *testing.T) {
	c := &Client{
		Client:     qbt.NewClient(qbt.Config{Host: "http://127.0.0.1:0"}),
		instanceID: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	prefs, err := c.GetAppPreferences(ctx)
	require.Error(t, err)
	require.Nil(t, prefs)
}

func TestCachedPreferencesPreserveCompatibilityFields(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/app/preferences" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		_, _ = w.Write([]byte(`{"mail_notification_encryption_type":"SMTPS","torrent_files_backup_enabled":true,"torrent_files_backup_dir":"backup","export_dir":"legacy","mail_notification_ssl_enabled":true}`))
	}))
	defer srv.Close()
	client := &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL})}
	prefs, err := client.GetAppPreferences(t.Context())
	require.NoError(t, err)
	require.Equal(t, "SMTPS", prefs.MailNotificationEncryptionType)
	require.True(t, prefs.TorrentFilesBackupEnabled)
	prefs.TorrentFilesBackupDir = "changed"

	cached, err := client.GetAppPreferences(t.Context())
	require.NoError(t, err)
	require.Equal(t, "backup", cached.TorrentFilesBackupDir)
	require.Equal(t, int64(1), hits.Load())
	raw, err := client.cachedAppPreferencesJSON()
	require.NoError(t, err)
	var decoded qbt.AppPreferences
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, *cached, decoded)
	require.Equal(t, "legacy", decoded.ExportDir)
	require.True(t, decoded.MailNotificationSslEnabled)
}
