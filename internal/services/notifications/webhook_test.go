// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseWebhookURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "valid https",
			input: "webhook://https://example.com/api/v1/webhook/key123",
			want:  "https://example.com/api/v1/webhook/key123",
		},
		{
			name:  "valid http",
			input: "webhook://http://localhost:8080/hook",
			want:  "http://localhost:8080/hook",
		},
		{
			name:    "rejects ftp scheme",
			input:   "webhook://ftp://example.com/hook",
			wantErr: "webhook target must be http or https",
		},
		{
			name:    "rejects bare path",
			input:   "webhook://just-a-path",
			wantErr: "webhook target must be http or https",
		},
		{
			name:    "rejects missing host",
			input:   "webhook://https:///no-host",
			wantErr: "webhook target host required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseWebhookURL(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSendWebhookSuccess(t *testing.T) {
	t.Parallel()

	var captured struct {
		method      string
		contentType string
		userAgent   string
		body        []byte
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.contentType = r.Header.Get("Content-Type")
		captured.userAgent = r.Header.Get("User-Agent")
		captured.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	svc := &Service{}
	rawURL := "webhook://" + server.URL + "/api/v1/webhook/testkey"
	event := Event{
		Type:         EventTorrentCompleted,
		TorrentName:  "Some.Movie.2025.1080p.BluRay.x264-GROUP",
		TorrentHash:  "abcdef0123456789abcdef0123456789abcdef01",
		InstanceID:   1,
		InstanceName: "qBittorrent",
	}

	err := svc.sendWebhook(context.Background(), rawURL, event, "Torrent completed", "message")
	require.NoError(t, err)

	require.Equal(t, http.MethodPost, captured.method)
	require.Equal(t, "application/json", captured.contentType)
	require.Equal(t, "qui", captured.userAgent)

	var payload webhookPayload
	require.NoError(t, json.Unmarshal(captured.body, &payload))
	require.Equal(t, "qui", payload.SourceApp)
	require.Equal(t, "torrent_completed", payload.Type)
	require.Equal(t, 1, payload.InstanceID)
	require.Equal(t, "qBittorrent", payload.InstanceName)
	require.NotNil(t, payload.Torrent)
	require.NotNil(t, payload.Torrent.Name)
	require.Equal(t, "Some.Movie.2025.1080p.BluRay.x264-GROUP", *payload.Torrent.Name)
}

func TestSendWebhookHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	t.Cleanup(server.Close)

	svc := &Service{}
	rawURL := "webhook://" + server.URL + "/hook"

	err := svc.sendWebhook(context.Background(), rawURL, Event{Type: EventTorrentAdded}, "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
	require.Contains(t, err.Error(), "server error")
}

func TestBuildWebhookPayloadTorrentEvent(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	event := Event{
		Type:                   EventTorrentAdded,
		TorrentName:            "Example.Movie.2026.1080p",
		TorrentHash:            "abcdef0123456789abcdef0123456789abcdef01",
		TorrentState:           "downloading",
		TorrentProgress:        0.25,
		TorrentRatio:           0.1,
		TorrentTotalSizeBytes:  20_000_000_000,
		TorrentDownloadedBytes: 5_000_000_000,
		TorrentAmountLeftBytes: 15_000_000_000,
		TorrentDlSpeedBps:      25_000_000,
		TorrentUpSpeedBps:      1_000_000,
		TorrentNumSeeds:        120,
		TorrentNumLeechs:       35,
		TrackerDomain:          "tracker.example.com",
		Category:               "movies",
		Tags:                   []string{"seed", "bluray"},
		InstanceID:             1,
		InstanceName:           "qBittorrent",
	}

	payload := svc.buildWebhookPayload(context.Background(), event, "title", "message")
	require.Equal(t, "qui", payload.SourceApp)
	require.Equal(t, "torrent_added", payload.Type)
	require.Equal(t, 1, payload.InstanceID)
	require.Equal(t, "qBittorrent", payload.InstanceName)
	require.False(t, payload.Timestamp.IsZero())

	require.NotNil(t, payload.Torrent)
	require.NotNil(t, payload.Torrent.Name)
	require.Equal(t, "Example.Movie.2026.1080p", *payload.Torrent.Name)
	require.NotNil(t, payload.Torrent.TrackerDomain)
	require.Equal(t, "tracker.example.com", *payload.Torrent.TrackerDomain)
	require.NotNil(t, payload.Torrent.Category)
	require.Equal(t, "movies", *payload.Torrent.Category)
	require.Equal(t, []string{"bluray", "seed"}, payload.Torrent.Tags)

	require.Nil(t, payload.Backup)
	require.Nil(t, payload.DirScan)
	require.Nil(t, payload.OrphanScan)
	require.Nil(t, payload.CrossSeed)
	require.Nil(t, payload.Automations)
}

func TestBuildWebhookPayloadCrossSeedEvent(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	event := Event{
		Type: EventCrossSeedAutomationSucceeded,
		CrossSeed: &CrossSeedEventData{
			RunID:      9,
			Mode:       "rss",
			Status:     "success",
			FeedItems:  120,
			Candidates: 8,
			Added:      3,
			Failed:     1,
			Skipped:    5,
			Samples:    []string{"Some.Movie.2025", "Another.Show.S01E01"},
		},
		InstanceID:   1,
		InstanceName: "qBittorrent",
	}

	payload := svc.buildWebhookPayload(context.Background(), event, "title", "message")
	require.Equal(t, "qui", payload.SourceApp)
	require.Equal(t, "cross_seed_automation_succeeded", payload.Type)
	require.NotNil(t, payload.CrossSeed)
	require.Equal(t, int64(9), payload.CrossSeed.RunID)
	require.Equal(t, "rss", payload.CrossSeed.Mode)
	require.Equal(t, 3, payload.CrossSeed.Added)
	require.Equal(t, []string{"Some.Movie.2025", "Another.Show.S01E01"}, payload.CrossSeed.Samples)
	require.Nil(t, payload.Torrent)
}

func TestBuildWebhookPayloadOmitsEmptySubObjects(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	payload := svc.buildWebhookPayload(context.Background(), Event{}, "", "")

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &raw))

	require.Contains(t, raw, "source_app")
	require.Contains(t, raw, "type")
	require.Contains(t, raw, "timestamp")

	require.NotContains(t, raw, "instance_id")
	require.NotContains(t, raw, "instance_name")
	require.NotContains(t, raw, "torrent")
	require.NotContains(t, raw, "backup")
	require.NotContains(t, raw, "dir_scan")
	require.NotContains(t, raw, "orphan_scan")
	require.NotContains(t, raw, "cross_seed")
	require.NotContains(t, raw, "automations")
	require.NotContains(t, raw, "error_messages")
}

func TestBuildWebhookPayloadTestEvent(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	payload := svc.buildWebhookPayload(context.Background(), Event{}, "Test notification", "This is a test.")

	require.Equal(t, "qui", payload.SourceApp)
	require.Equal(t, "test", payload.Type)
}

func TestSendWebhookNoAuthHeaders(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	svc := &Service{}
	rawURL := "webhook://" + server.URL + "/hook"
	err := svc.sendWebhook(context.Background(), rawURL, Event{Type: EventTorrentAdded, TorrentName: "test"}, "", "")
	require.NoError(t, err)
	require.Empty(t, capturedHeaders.Get("X-API-Key"))
	require.Empty(t, capturedHeaders.Get("Authorization"))
}

func TestValidateURLWebhookScheme(t *testing.T) {
	t.Parallel()

	err := ValidateURL("webhook://https://example.com/api/v1/webhook/key")
	require.NoError(t, err)

	err = ValidateURL("webhook://ftp://example.com/hook")
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhook target must be http or https")
}
