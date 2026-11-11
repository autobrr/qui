// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package syncowner_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/testutil"
)

func TestOwner_WriteToSnapshotTorrentFailsNextSyncWithHash(t *testing.T) {
	const hash = "aaaa000000000000000000000000000000000001"
	qbtClient := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).WithHTTPClient(&http.Client{
		Transport: testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if r.URL.Path != "/api/v2/sync/maindata" {
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result(), nil
			}
			if r.URL.Query().Get("rid") == "0" {
				_, _ = fmt.Fprintf(rec, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1}}}`, hash)
			} else {
				_, _ = rec.WriteString(`{"rid":2}`)
			}
			return rec.Result(), nil
		}),
	})
	owner := syncowner.New(qbtClient, qbt.DefaultSyncOptions())
	require.NoError(t, owner.Sync(t.Context()))

	torrent, ok := owner.SnapshotUnchecked().Torrent(hash)
	require.True(t, ok)
	torrent.Name = "changed by a reader"

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = owner.Sync(t.Context())
	}()
	require.NotNil(t, recovered, "the sync after a write to a shared torrent must fail")
	require.Contains(t, fmt.Sprint(recovered), hash)
}
