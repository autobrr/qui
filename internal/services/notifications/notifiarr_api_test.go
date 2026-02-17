// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateNotifiarrAPIKeySkipsNonNotifiarrAPI(t *testing.T) {
	t.Parallel()

	err := ValidateNotifiarrAPIKey(context.Background(), "discord://token@channel")
	require.NoError(t, err)
}

func TestValidateNotifiarrAPIKeyValid(t *testing.T) {
	t.Parallel()

	var (
		hits int32
		ch   = make(chan struct {
			key  string
			path string
		}, 1)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		ch <- struct {
			key  string
			path string
		}{
			key:  r.Header.Get("X-API-Key"),
			path: r.URL.Path,
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	endpoint := server.URL + "/api/v1/notification/qui"
	rawURL := "notifiarrapi://abc123?endpoint=" + url.QueryEscape(endpoint)

	err := ValidateNotifiarrAPIKey(context.Background(), rawURL)
	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&hits))
	select {
	case got := <-ch:
		require.Equal(t, "abc123", got.key)
		require.Equal(t, "/api/v1/user/validate", got.path)
	case <-time.After(time.Second):
		t.Fatal("expected validation request")
	}
}

func TestValidateNotifiarrAPIKeyInvalid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid key"))
	}))
	t.Cleanup(server.Close)

	endpoint := server.URL + "/api/v1/notification/qui"
	rawURL := "notifiarrapi://abc123?endpoint=" + url.QueryEscape(endpoint)

	err := ValidateNotifiarrAPIKey(context.Background(), rawURL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "notifiarr api key invalid")
	require.Contains(t, err.Error(), "invalid key")
}

func TestBuildNotifiarrAPIDataIncludesStructuredFields(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	event := Event{
		Type: EventCrossSeedAutomationFailed,
		CrossSeed: &CrossSeedEventData{
			RunID:      9,
			Mode:       "rss",
			Status:     "partial",
			FeedItems:  120,
			Candidates: 8,
			Added:      3,
			Failed:     1,
			Skipped:    4,
			Samples:    []string{"Example.One", "Example.Two"},
		},
		ErrorMessage:  "indexer timeout",
		ErrorMessages: []string{"indexer timeout", "upstream 502"},
	}

	data := svc.buildNotifiarrAPIData(context.Background(), event, "title", "message")
	require.NotNil(t, data.CrossSeed)
	require.Equal(t, int64(9), data.CrossSeed.RunID)
	require.Equal(t, "rss", data.CrossSeed.Mode)
	require.Equal(t, "partial", data.CrossSeed.Status)

	require.GreaterOrEqual(t, len(data.ErrorMessages), 2)
	require.Equal(t, "indexer timeout", data.ErrorMessages[0])
	require.True(t, slices.Contains(data.ErrorMessages, "upstream 502"))
	require.False(t, slices.Contains(data.ErrorMessages, ""))
	require.False(t, slices.Contains(data.ErrorMessages, "   "))
	require.NotEmpty(t, strings.TrimSpace(data.Description))
}

func TestBuildNotifiarrAPIDataIncludesTorrentFields(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	addedOn := time.Now().Add(-10 * time.Second).Unix()
	etaSeconds := int64(3600)
	event := Event{
		Type:                   EventTorrentAdded,
		TorrentName:            "Example.Movie.2026.1080p",
		TorrentHash:            "abcdef0123456789abcdef0123456789abcdef01",
		TorrentAddedOn:         addedOn,
		TorrentETASeconds:      etaSeconds,
		TorrentState:           "downloading",
		TorrentProgress:        0.25,
		TorrentTotalSizeBytes:  20_000_000_000,
		TorrentDownloadedBytes: 5_000_000_000,
		TorrentAmountLeftBytes: 15_000_000_000,
		TorrentDlSpeedBps:      25_000_000,
		TorrentUpSpeedBps:      1_000_000,
		TorrentNumSeeds:        120,
		TorrentNumLeechs:       35,
	}

	data := svc.buildNotifiarrAPIData(context.Background(), event, "title", "message")
	require.NotNil(t, data.Torrent)
	require.NotNil(t, data.Torrent.AddedAt)
	require.Equal(t, time.Unix(addedOn, 0).UTC(), *data.Torrent.AddedAt)
	require.NotNil(t, data.Torrent.EtaSeconds)
	require.Equal(t, etaSeconds, *data.Torrent.EtaSeconds)
	require.NotNil(t, data.Torrent.EstimatedCompletionAt)
	require.True(t, data.Torrent.EstimatedCompletionAt.Equal(data.Timestamp.Add(time.Duration(etaSeconds)*time.Second)))
	require.NotNil(t, data.Torrent.State)
	require.Equal(t, "downloading", *data.Torrent.State)
	require.NotNil(t, data.Torrent.Progress)
	require.InDelta(t, 0.25, *data.Torrent.Progress, 1e-9)
	require.NotNil(t, data.Torrent.TotalSizeBytes)
	require.Equal(t, int64(20_000_000_000), *data.Torrent.TotalSizeBytes)
	require.NotNil(t, data.Torrent.DownloadedBytes)
	require.Equal(t, int64(5_000_000_000), *data.Torrent.DownloadedBytes)
	require.NotNil(t, data.Torrent.AmountLeftBytes)
	require.Equal(t, int64(15_000_000_000), *data.Torrent.AmountLeftBytes)
	require.NotNil(t, data.Torrent.DlSpeedBps)
	require.Equal(t, int64(25_000_000), *data.Torrent.DlSpeedBps)
	require.NotNil(t, data.Torrent.UpSpeedBps)
	require.Equal(t, int64(1_000_000), *data.Torrent.UpSpeedBps)
	require.NotNil(t, data.Torrent.NumSeeds)
	require.Equal(t, int64(120), *data.Torrent.NumSeeds)
	require.NotNil(t, data.Torrent.NumLeechs)
	require.Equal(t, int64(35), *data.Torrent.NumLeechs)
}
