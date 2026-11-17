// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"encoding/json/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

var shareRoutes = []routeKey{
	{http.MethodPost, "/api/shares"},
	{http.MethodGet, "/api/shares"},
	{http.MethodGet, "/api/shares/settings"},
	{http.MethodPut, "/api/shares/settings"},
	{http.MethodPost, "/api/shares/1/stop"},
	{http.MethodPost, "/api/shares/1/unlock"},
	{http.MethodGet, "/api/shares/1/torrent"},
	{http.MethodDelete, "/api/shares/1"},
}

func TestShareStartRoute(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)

	status, _ := rig.sendBody(t, http.MethodPost, "/api/shares", "", cookie, `{"instanceId":1,"hashes":[]}`)
	require.Equal(t, http.StatusBadRequest, status, "a body without torrents")

	t.Run("a refusal answers 409 with its reason", func(t *testing.T) {
		status, body := rig.sendBody(t, http.MethodPost, "/api/shares", "", cookie, `{"instanceId":1,"hashes":["aaaa"]}`)
		require.Equal(t, http.StatusConflict, status)
		var res struct {
			Error   string `json:"error"`
			Refused []struct {
				Hash   string `json:"hash"`
				Reason string `json:"reason"`
			} `json:"refused"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &res))
		reason := "Set a tracker in Settings → Shares: the port and the public address of qui's tracker, or a tracker URL."
		require.Equal(t, reason, res.Error)
		require.Len(t, res.Refused, 1)
		require.Equal(t, "aaaa", res.Refused[0].Hash)
		require.Equal(t, reason, res.Refused[0].Reason)
	})
}

// newShareRig builds the router on a Share service whose database the test
// fills through the returned store.
func newShareRig(t *testing.T) (*permissionRig, *share.Store, *transfer.Store) {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "api-shares")
	store := share.NewStore(db)
	helpers, err := transfer.NewStore(t.Context(), db)
	require.NoError(t, err)
	rig := newPermissionRig(t, func(deps *Dependencies) {
		svc := share.NewService(share.Deps{
			Store:           store,
			Settings:        share.NewSettingsStore(db),
			TrackerSettings: sharetracker.NewSettingsStore(db),
			Helpers:         helpers,
			Instances:       deps.InstanceStore,
			Sync:            deps.SyncManager,
			TrackerHost:     "127.0.0.1",
			Config:          share.DefaultConfig(),
		})
		t.Cleanup(svc.Close)
		deps.ShareService = svc
	})
	return rig, store, helpers
}

func TestShareListAndRemoveRoutes(t *testing.T) {
	rig, store, helpers := newShareRig(t)
	cookie := rig.sessionCookie(t)
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	add := func(helperHash string, state share.State, reason string, endedAt time.Time) int64 {
		sh := &share.Share{
			InstanceID: 1, Hash: "aaaa", Name: "Show.S01.1080p-GRP", Files: []string{"Show.S01E01.mkv"}, Size: 200,
			HelperHash: helperHash, Tracker: share.TrackerOwn,
			OwnerSecret: "owner", RecipientSecret: "recipient", TorrentFile: []byte("d8:announce0:e"),
			EndAtFirstDownload: true, ExpiresAt: created.Add(7 * 24 * time.Hour), State: state, EndReason: reason, CreatedAt: created,
			EndedAt: endedAt, Downloaded: 50, Uploaded: 60,
			Peers: []share.Peer{{Address: "198.51.100.7:6881", Progress: 0.25, SeenAt: created}},
		}
		require.NoError(t, store.Create(t.Context(), sh))
		require.NoError(t, helpers.RegisterHelper(t.Context(), transfer.Helper{
			InstanceID: 1, Hash: sh.HelperHash, Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: sh.ID,
		}))
		return sh.ID
	}
	active := add(strings.Repeat("b", 40), share.StateActive, "", time.Time{})
	ended := add(strings.Repeat("c", 40), share.StateEnded, "The Share expired.", created.Add(time.Hour))

	status, body := rig.sendBody(t, http.MethodGet, "/api/shares", "", cookie, "")
	require.Equal(t, http.StatusOK, status)
	var list []map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	require.Len(t, list, 2)
	require.Equal(t, map[string]any{
		"id": float64(ended), "instanceId": float64(1), "hash": "aaaa", "name": "Show.S01.1080p-GRP", "size": float64(200),
		"tracker": "own", "endAtFirstDownload": true, "firstDownloaderOnly": false, "state": "ended",
		"endReason": "The Share expired.", "endReasonCode": "expired",
		"createdAt": "2026-11-01T12:00:00Z", "expiresAt": "2026-11-08T12:00:00Z", "endedAt": "2026-11-01T13:00:00Z",
		"peers":    []any{map[string]any{"address": "198.51.100.7:6881", "progress": 0.25, "seenAt": "2026-11-01T12:00:00Z"}},
		"progress": 0.25, "downloaded": float64(50), "uploaded": float64(60),
	}, list[0])
	require.Nil(t, list[1]["endedAt"])

	status, _ = rig.sendBody(t, http.MethodDelete, "/api/shares/"+strconv.FormatInt(active, 10), "", cookie, "")
	require.Equal(t, http.StatusConflict, status, "an active Share stays")

	status, _ = rig.sendBody(t, http.MethodDelete, "/api/shares/"+strconv.FormatInt(ended, 10), "", cookie, "")
	require.Equal(t, http.StatusNoContent, status)
	_, err := store.Get(t.Context(), ended)
	require.ErrorIs(t, err, share.ErrNotFound)

	status, _ = rig.sendBody(t, http.MethodDelete, "/api/shares/"+strconv.FormatInt(ended, 10), "", cookie, "")
	require.Equal(t, http.StatusNotFound, status)

	status, body = rig.sendBody(t, http.MethodGet, "/api/shares/"+strconv.FormatInt(active, 10)+"/torrent", "", cookie, "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "d8:announce0:e", body)
}

func TestShareActionRoutesAnswer404ForAnUnknownShare(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)
	for _, route := range shareRoutes[4:] {
		status, _ := rig.sendBody(t, route.Method, route.Path, "", cookie, "")
		require.Equal(t, http.StatusNotFound, status, "%s %s", route.Method, route.Path)
	}
	status, _ := rig.sendBody(t, http.MethodPost, "/api/shares/x/stop", "", cookie, "")
	require.Equal(t, http.StatusBadRequest, status)
}

func TestShareSettingsRoutes(t *testing.T) {
	rig, store, _ := newShareRig(t)
	cookie := rig.sessionCookie(t)

	status, body := rig.sendBody(t, http.MethodGet, "/api/shares/settings", "", cookie, "")
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"port":0,"publicAddress":"","trustedProxies":[],"trackerUrl":"","maxActive":10,"peersPerShare":20,
		"defaults":{"days":7,"endAtFirstDownload":true,"firstDownloaderOnly":true},"trackerInUse":""}`, body)

	saved := `{"port":0,"publicAddress":"","trustedProxies":["10.0.0.0/8"],"trackerUrl":"https://tracker.example/announce",
		"maxActive":3,"peersPerShare":5,"defaults":{"days":2,"endAtFirstDownload":false,"firstDownloaderOnly":false}}`
	status, body = rig.sendBody(t, http.MethodPut, "/api/shares/settings", "", cookie, saved)
	require.Equal(t, http.StatusOK, status, body)
	want := `{"port":0,"publicAddress":"","trustedProxies":["10.0.0.0/8"],"trackerUrl":"https://tracker.example/announce",
		"maxActive":3,"peersPerShare":5,"defaults":{"days":2,"endAtFirstDownload":false,"firstDownloaderOnly":false},"trackerInUse":"outside"}`
	require.JSONEq(t, want, body)
	status, body = rig.sendBody(t, http.MethodGet, "/api/shares/settings", "", cookie, "")
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, want, body)

	status, body = rig.sendBody(t, http.MethodPut, "/api/shares/settings", "", cookie,
		`{"maxActive":0,"peersPerShare":5,"defaults":{"days":2}}`)
	require.Equal(t, http.StatusBadRequest, status, body)

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })
	port := busy.Addr().(*net.TCPAddr).Port
	status, body = rig.sendBody(t, http.MethodPut, "/api/shares/settings", "", cookie,
		`{"port":`+strconv.Itoa(port)+`,"publicAddress":"http://127.0.0.1","maxActive":3,"peersPerShare":5,"defaults":{"days":2}}`)
	require.Equal(t, http.StatusConflict, status)
	require.JSONEq(t, `{"error":"Another program uses port `+strconv.Itoa(port)+`. Pick another port."}`, body)

	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Create(t.Context(), &share.Share{
		InstanceID: 1, Hash: "aaaa", Name: "Show.S01.1080p-GRP", Files: []string{"Show.S01E01.mkv"}, Size: 200,
		HelperHash: strings.Repeat("b", 40), Tracker: share.TrackerOwn, OwnerSecret: "owner", RecipientSecret: "recipient",
		TorrentFile: []byte("d8:announce0:e"), ExpiresAt: created.Add(7 * 24 * time.Hour), State: share.StateActive, CreatedAt: created,
	}))
	status, body = rig.sendBody(t, http.MethodPut, "/api/shares/settings", "", cookie,
		`{"port":`+strconv.Itoa(port)+`,"publicAddress":"http://127.0.0.1","maxActive":3,"peersPerShare":5,"defaults":{"days":2}}`)
	require.Equal(t, http.StatusConflict, status)
	require.JSONEq(t, `{"error":"End the active Shares on qui's tracker first: their .torrent files carry the old address."}`, body)
}
