// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	pathPasskey  = "abc123xyz"
	queryPasskey = "querypasskey7"
	pathOrigin   = "https://tracker.example.invalid"
	queryOrigin  = "https://query.example.invalid"
	pathTracker  = pathOrigin + "/" + pathPasskey + "/announce"
	queryTracker = queryOrigin + "/announce?passkey=" + queryPasskey
)

// newQBittorrentStub answers as a qBittorrent with two torrents on one content
// path, each with a passkey in another place of its announce URL.
func newQBittorrentStub(t *testing.T) *httptest.Server {
	t.Helper()
	torrent := func(name, tracker string, addedOn int) map[string]any {
		return map[string]any{
			"name": name, "state": "uploading", "added_on": addedOn, "size": 100, "total_size": 100, "progress": 1,
			"save_path": "/data", "content_path": "/data/Show.S01", "tracker": tracker,
			"magnet_uri": "magnet:?xt=urn:btih:" + name + "&tr=" + url.QueryEscape(tracker),
			"trackers":   []map[string]any{{"url": tracker}},
		}
	}
	responses := map[string]any{
		"/api/v2/sync/maindata": map[string]any{
			"rid": 1, "full_update": true,
			"torrents": map[string]any{
				"aaa": torrent("aaa", pathTracker, 2),
				"bbb": torrent("bbb", queryTracker, 1),
			},
			"trackers": map[string]any{pathTracker: []string{"aaa"}, queryTracker: []string{"bbb"}},
		},
		"/api/v2/app/preferences":     map[string]any{"add_trackers_enabled": true, "add_trackers": pathTracker + "\n\n" + queryTracker},
		"/api/v2/app/buildInfo":       map[string]any{},
		"/api/v2/torrents/files":      []map[string]any{{"index": 0, "name": "Show.S01/file.mkv", "size": 100}},
		"/api/v2/torrents/categories": map[string]any{},
		"/api/v2/torrents/tags":       []string{},
		"/api/v2/torrentcreator/status": []map[string]any{
			{"taskID": "task", "status": "Finished", "trackers": []string{pathTracker, queryTracker}},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.16.0")
			return
		case "/api/v2/app/version":
			_, _ = io.WriteString(w, "v5.2.0")
			return
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
			return
		case "/api/v2/torrents/trackers":
			rows := map[string]string{"aaa": pathTracker, "bbb": queryTracker}
			_ = json.MarshalWrite(w, []map[string]any{
				{"url": "** [DHT] **", "status": 0},
				{"url": rows[strings.ToLower(r.URL.Query().Get("hash"))], "status": 2},
			})
			return
		}
		if response, ok := responses[r.URL.Path]; ok {
			_ = json.MarshalWrite(w, response)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type passkeyRig struct {
	*permissionRig
	instanceID int
	runID      int64
	cookie     *http.Cookie
}

func newPasskeyRig(t *testing.T) *passkeyRig {
	t.Helper()
	deps := newTestDependencies(t)
	db := testdb.NewMigratedSQLite(t, "api-passkeys")

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)

	instance, err := instanceStore.Create(t.Context(), "stub", newQBittorrentStub(t).URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	backupStore := models.NewBackupStore(db)
	run := &models.BackupRun{InstanceID: instance.ID, Kind: models.BackupRunKindManual, Status: models.BackupRunStatusSuccess, RequestedBy: "test", RequestedAt: time.Now()}
	require.NoError(t, backupStore.CreateRun(t.Context(), run))
	require.NoError(t, backupStore.InsertItems(t.Context(), run.ID, []models.BackupItem{
		{TorrentHash: "aaa", Name: "aaa", Category: new("snapshot"), SizeBytes: 100},
		{TorrentHash: "bbb", Name: "bbb", Category: new("snapshot"), SizeBytes: 100},
	}))

	deps.InstanceStore = instanceStore
	deps.ClientPool = clientPool
	deps.SyncManager = syncManager
	deps.BackupService = backups.NewService(backupStore, syncManager, backups.Config{DataDir: t.TempDir()}, nil)
	deps.CrossSeedService = crossseed.NewService(instanceStore, syncManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)

	router, err := NewServer(deps).Handler()
	require.NoError(t, err)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	rig := &passkeyRig{permissionRig: &permissionRig{deps: deps, url: srv.URL}, instanceID: instance.ID, runID: run.ID}
	rig.cookie = rig.sessionCookie(t)
	return rig
}

func (rig *passkeyRig) request(ctx context.Context, t *testing.T, method, path, key, body string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, rig.url+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (rig *passkeyRig) fetch(t *testing.T, method, path, key, body string, cookie *http.Cookie) (int, string) {
	t.Helper()
	resp := rig.request(t.Context(), t, method, path, key, body, cookie)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// streamInit opens a stream and returns the data of its init frame. The stream
// stays open until ctx ends.
func (rig *passkeyRig) streamInit(ctx context.Context, t *testing.T, limit int, key string, cookie *http.Cookie, expression string) string {
	t.Helper()
	filters, err := json.Marshal(qbittorrent.FilterOptions{Expr: expression})
	require.NoError(t, err)
	streams := fmt.Sprintf(`[{"key":"view","instanceId":%d,"limit":%d,"filters":%s}]`, rig.instanceID, limit, filters)
	resp := rig.request(ctx, t, http.MethodGet, "/api/stream?streams="+url.QueryEscape(streams), key, "", cookie)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	reader := bufio.NewReader(resp.Body)
	var event string
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if name, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(name)
		}
		if data, ok := strings.CutPrefix(line, "data:"); ok && event == "init" {
			return data
		}
	}
}

func TestReadKeyGetsNoTrackerPasskey(t *testing.T) {
	rig := newPasskeyRig(t)
	readKey := rig.key(t,
		models.Permission{Area: models.AreaTorrents, Level: models.LevelRead},
		models.Permission{Area: models.AreaCrossSeed, Level: models.LevelRead},
		models.Permission{Area: models.AreaBackups, Level: models.LevelRead},
		models.Permission{Area: models.AreaInstances, Level: models.LevelRead},
	)
	writeKey := rig.key(t,
		models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite},
		models.Permission{Area: models.AreaCrossSeed, Level: models.LevelRead},
		models.Permission{Area: models.AreaBackups, Level: models.LevelRead},
		models.Permission{Area: models.AreaInstances, Level: models.LevelRead},
	)
	id := rig.instanceID
	both := []string{pathPasskey, queryPasskey}
	bothOrigins := []string{pathOrigin, queryOrigin}

	for _, tc := range []struct {
		name, method, path, body string
		passkeys, origins        []string
	}{
		{"torrent trackers, passkey in the path", http.MethodGet, fmt.Sprintf("/api/instances/%d/torrents/aaa/trackers", id), "", []string{pathPasskey}, []string{pathOrigin, "** [DHT] **"}},
		{"torrent trackers, passkey in the query", http.MethodGet, fmt.Sprintf("/api/instances/%d/torrents/bbb/trackers", id), "", []string{queryPasskey}, []string{queryOrigin, "** [DHT] **"}},
		{"instance trackers", http.MethodGet, fmt.Sprintf("/api/instances/%d/trackers", id), "", both, bothOrigins},
		{"torrent list", http.MethodGet, fmt.Sprintf("/api/instances/%d/torrents", id), "", both, bothOrigins},
		{"cross-instance list", http.MethodGet, fmt.Sprintf("/api/torrents/cross-instance?instanceIds=%d", id), "", both, bothOrigins},
		{"local matches, passkey in the query", http.MethodGet, fmt.Sprintf("/api/cross-seed/torrents/%d/aaa/local-matches", id), "", []string{queryPasskey}, []string{queryOrigin}},
		{"local matches, passkey in the path", http.MethodGet, fmt.Sprintf("/api/cross-seed/torrents/%d/bbb/local-matches", id), "", []string{pathPasskey}, []string{pathOrigin}},
		{"delete check", http.MethodPost, fmt.Sprintf("/api/instances/%d/torrents/delete-cross-seeds", id), `{"hashes":["aaa"],"action":"delete","deleteFiles":true}`, []string{queryPasskey}, []string{queryOrigin}},
		{"torrent creator status", http.MethodGet, fmt.Sprintf("/api/instances/%d/torrent-creator/status", id), "", both, bothOrigins},
		{"preferences", http.MethodGet, fmt.Sprintf("/api/instances/%d/preferences", id), "", both, bothOrigins},
		{"restore preview", http.MethodPost, fmt.Sprintf("/api/instances/%d/backups/runs/%d/restore/preview", id, rig.runID), `{"mode":"overwrite"}`, both, bothOrigins},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := rig.fetch(t, tc.method, tc.path, readKey, tc.body, nil)
			require.Equal(t, http.StatusOK, status, body)
			for _, passkey := range tc.passkeys {
				require.NotContains(t, body, passkey)
			}
			for _, origin := range tc.origins {
				require.Contains(t, body, origin)
			}

			// After the read key, so that the read key cannot have changed a cached row.
			for name, request := range map[string]struct {
				key    string
				cookie *http.Cookie
			}{"write key": {key: writeKey}, "session": {cookie: rig.cookie}} {
				status, body := rig.fetch(t, tc.method, tc.path, request.key, tc.body, request.cookie)
				require.Equal(t, http.StatusOK, status, "%s: %s", name, body)
				for _, passkey := range tc.passkeys {
					require.Contains(t, body, passkey, name)
				}
			}
		})
	}

	t.Run("a key with only Backups: write gets no passkey from a restore dry run", func(t *testing.T) {
		key := rig.key(t, models.Permission{Area: models.AreaBackups, Level: models.LevelWrite})
		path := fmt.Sprintf("/api/instances/%d/backups/runs/%d/restore", id, rig.runID)
		const dryRun = `{"mode":"overwrite","dryRun":true}`
		status, body := rig.fetch(t, http.MethodPost, path, key, dryRun, nil)
		require.Equal(t, http.StatusOK, status, body)
		require.NotContains(t, body, pathPasskey)
		require.Contains(t, body, pathOrigin)

		status, body = rig.fetch(t, http.MethodPost, path, "", dryRun, rig.cookie)
		require.Equal(t, http.StatusOK, status, body)
		require.Contains(t, body, pathPasskey)
	})

	t.Run("a key with only Cross-seed: read gets no passkey from local matches", func(t *testing.T) {
		key := rig.key(t, models.Permission{Area: models.AreaCrossSeed, Level: models.LevelRead})
		status, body := rig.fetch(t, http.MethodGet, fmt.Sprintf("/api/cross-seed/torrents/%d/aaa/local-matches", id), key, "", nil)
		require.Equal(t, http.StatusOK, status, body)
		require.NotContains(t, body, queryPasskey)
		require.Contains(t, body, queryOrigin)
	})
}

// The list rows of a read key hide the passkeys, and the torrents of the Sync
// snapshot, which every reader shares, keep their tracker URLs.
func TestReadKeyListLeavesTheSyncSnapshotWhole(t *testing.T) {
	rig := newPasskeyRig(t)
	readKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelRead})

	status, body := rig.fetch(t, http.MethodGet, fmt.Sprintf("/api/instances/%d/torrents", rig.instanceID), readKey, "", nil)
	require.Equal(t, http.StatusOK, status, body)
	require.NotContains(t, body, pathPasskey)
	require.NotContains(t, body, queryPasskey)

	client, err := rig.deps.ClientPool.GetClient(t.Context(), rig.instanceID)
	require.NoError(t, err)
	snapshot := client.SyncOwner().SnapshotUnchecked()
	require.NotNil(t, snapshot)
	for hash, tracker := range map[string]string{"aaa": pathTracker, "bbb": queryTracker} {
		torrent, ok := snapshot.Torrent(hash)
		require.True(t, ok, hash)
		require.Equal(t, tracker, torrent.Tracker, hash)
		require.Len(t, torrent.Trackers, 1, hash)
		require.Equal(t, tracker, torrent.Trackers[0].Url, hash)
	}
}

func TestMagnetFieldNeedsTorrentsWrite(t *testing.T) {
	rig := newPasskeyRig(t)
	readKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelRead})
	writeKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})
	path := fmt.Sprintf("/api/instances/%d/torrents/field", rig.instanceID)
	field := func(name string) string { return `{"field":"` + name + `","hashes":["aaa"]}` }

	status, body := rig.fetch(t, http.MethodPost, path, readKey, field("magnet_uri"), nil)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "API key needs Torrents: write\n", body)

	status, body = rig.fetch(t, http.MethodPost, path, readKey, field("name"), nil)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, "aaa")

	status, body = rig.fetch(t, http.MethodPost, path, writeKey, field("magnet_uri"), nil)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, pathPasskey)

	status, body = rig.fetch(t, http.MethodPost, path, "", field("magnet_uri"), rig.cookie)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, pathPasskey)
}

// A subscriber that joins a group gets the retained frame of the group, so the
// test runs each order of a read key and a session on the same stream.
func TestStreamKeepsPasskeysFromReadKey(t *testing.T) {
	rig := newPasskeyRig(t)
	readKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelRead})
	writeKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})

	requireHidden := func(t *testing.T, frame string) {
		t.Helper()
		require.NotContains(t, frame, pathPasskey)
		require.NotContains(t, frame, queryPasskey)
		require.Contains(t, frame, pathOrigin)
		require.Contains(t, frame, queryOrigin)
	}
	requireFull := func(t *testing.T, frame string) {
		t.Helper()
		require.Contains(t, frame, pathPasskey)
		require.Contains(t, frame, queryPasskey)
	}

	t.Run("a session opens the stream first", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		requireFull(t, rig.streamInit(ctx, t, 100, "", rig.cookie, ""))
		requireHidden(t, rig.streamInit(ctx, t, 100, readKey, nil, ""))
		requireFull(t, rig.streamInit(ctx, t, 100, writeKey, nil, ""))
	})

	t.Run("a read key opens the stream first", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		requireHidden(t, rig.streamInit(ctx, t, 101, readKey, nil, ""))
		requireFull(t, rig.streamInit(ctx, t, 101, "", rig.cookie, ""))
	})
}

// regression: #3852
func TestReadKeyCannotFilterTrackerPasskeys(t *testing.T) {
	rig := newPasskeyRig(t)
	readKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelRead})
	writeKey := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})
	for _, tc := range []struct {
		name, expression string
		readTotal        int
	}{
		{"tracker path", `Tracker contains "abc123xyz"`, 0},
		{"tracker query", `Tracker contains "querypasskey7"`, 0},
		{"trackers path", `any(Trackers, {.Url contains "abc123xyz"})`, 0},
		{"trackers query", `any(Trackers, {.Url contains "querypasskey7"})`, 0},
		{"magnet path", `MagnetURI contains "abc123xyz"`, 0},
		{"magnet query", `MagnetURI contains "querypasskey7"`, 0},
		{"tracker host", `Tracker contains "tracker.example.invalid"`, 1},
		{"trackers host", `any(Trackers, {.Url contains "tracker.example.invalid"})`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filters, err := json.Marshal(qbittorrent.FilterOptions{Expr: tc.expression})
			require.NoError(t, err)
			for _, endpoint := range []struct{ method, path, body string }{
				{http.MethodGet, fmt.Sprintf("/api/instances/%d/torrents?filters=%s", rig.instanceID, url.QueryEscape(string(filters))), ""},
				{http.MethodGet, fmt.Sprintf("/api/torrents/cross-instance?instanceIds=%d&filters=%s", rig.instanceID, url.QueryEscape(string(filters))), ""},
				{http.MethodPost, fmt.Sprintf("/api/instances/%d/torrents/field", rig.instanceID), fmt.Sprintf(`{"field":"name","selectAll":true,"filters":%s}`, filters)},
				{http.MethodPost, fmt.Sprintf("/api/instances/%d/torrents/delete-cross-seeds", rig.instanceID), fmt.Sprintf(`{"action":"delete","deleteFiles":true,"selectAll":true,"filters":%s}`, filters)},
				{http.MethodPost, "/api/instances/0/torrents/delete-cross-seeds", fmt.Sprintf(`{"action":"delete","deleteFiles":true,"selectAll":true,"instanceIds":[%d],"filters":%s}`, rig.instanceID, filters)},
			} {
				for _, auth := range []struct {
					key    string
					cookie *http.Cookie
					total  int
				}{{readKey, nil, tc.readTotal}, {writeKey, nil, 1}, {"", rig.cookie, 1}} {
					status, body := rig.fetch(t, endpoint.method, endpoint.path, auth.key, endpoint.body, auth.cookie)
					if strings.HasSuffix(endpoint.path, "/delete-cross-seeds") {
						if auth.total == 0 {
							require.Equal(t, http.StatusBadRequest, status, body)
							require.Contains(t, body, "No torrents match the selection criteria")
						} else {
							require.Equal(t, http.StatusOK, status, body)
							require.Contains(t, body, `"matches"`)
						}
						continue
					}
					require.Equal(t, http.StatusOK, status, body)
					var result struct {
						Total int `json:"total"`
					}
					require.NoError(t, json.Unmarshal([]byte(body), &result))
					require.Equal(t, auth.total, result.Total, "%s: %s", endpoint.path, body)
				}
			}
			for _, readFirst := range []bool{true, false} {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				limit := 100
				if readFirst {
					limit++
				}
				auths := []struct {
					key    string
					cookie *http.Cookie
					total  int
				}{{readKey, nil, tc.readTotal}, {"", rig.cookie, 1}, {writeKey, nil, 1}}
				if !readFirst {
					auths[0], auths[1] = auths[1], auths[0]
				}
				for _, auth := range auths {
					frame := rig.streamInit(ctx, t, limit, auth.key, auth.cookie, tc.expression)
					var result struct {
						Data struct {
							Total int `json:"total"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal([]byte(frame), &result))
					require.Equal(t, auth.total, result.Data.Total, "read first %t: %s", readFirst, frame)
				}
			}
		})
	}
}
