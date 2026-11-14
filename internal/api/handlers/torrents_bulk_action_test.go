// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestValidateBulkActionRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		req       BulkActionRequest
		shouldErr bool
	}{
		{
			name: "pause has no extra required params",
			req: BulkActionRequest{
				Action: "pause",
			},
			shouldErr: false,
		},
		{
			name: "add tags requires tags",
			req: BulkActionRequest{
				Action: "addTags",
			},
			shouldErr: true,
		},
		{
			name: "set location requires location",
			req: BulkActionRequest{
				Action: "setLocation",
			},
			shouldErr: true,
		},
		{
			name: "edit trackers requires both old and new urls",
			req: BulkActionRequest{
				Action: "editTrackers",
			},
			shouldErr: true,
		},
		{
			name: "add trackers requires payload",
			req: BulkActionRequest{
				Action: "addTrackers",
			},
			shouldErr: true,
		},
		{
			name: "remove trackers accepts payload",
			req: BulkActionRequest{
				Action:      "removeTrackers",
				TrackerURLs: "udp://tracker.example.com:80/announce",
			},
			shouldErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateBulkActionRequest(tc.req)
			if tc.shouldErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestAddBulkTarget_DeduplicatesByInstanceAndHash(t *testing.T) {
	t.Parallel()

	targetsByInstance := make(map[int][]string)
	seen := make(map[int]map[string]struct{})

	addBulkTarget(targetsByInstance, seen, 1, "ABC")
	addBulkTarget(targetsByInstance, seen, 1, "abc")
	addBulkTarget(targetsByInstance, seen, 2, "abc")

	if len(targetsByInstance[1]) != 1 {
		t.Fatalf("expected one hash for instance 1, got %d", len(targetsByInstance[1]))
	}
	if len(targetsByInstance[2]) != 1 {
		t.Fatalf("expected one hash for instance 2, got %d", len(targetsByInstance[2]))
	}
}

func TestAppendTargetsFromCrossInstanceTorrents_RespectsExclusions(t *testing.T) {
	t.Parallel()

	torrents := []qbittorrent.CrossInstanceTorrentView{
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "aaa"}},
			InstanceID:  1,
		},
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "bbb"}},
			InstanceID:  1,
		},
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "ccc"}},
			InstanceID:  2,
		},
		// A hybrid torrent excluded by its v1 hash: the row lists it under Hash
		// (the v2 hash), so a Hash-only match would still act on it (#1481, #1571).
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "ddd", InfohashV1: "eee"}},
			InstanceID:  1,
		},
	}

	targetsByInstance := make(map[int][]string)
	seen := make(map[int]map[string]struct{})
	excludeHashes := map[string]struct{}{"bbb": {}, "eee": {}}
	excludeTargets := buildExcludeTargetSet([]BulkActionTarget{
		{InstanceID: 2, Hash: "ccc"},
	})

	appendTargetsFromCrossInstanceTorrents(targetsByInstance, seen, torrents, excludeHashes, excludeTargets)

	if len(targetsByInstance[1]) != 1 || targetsByInstance[1][0] != "aaa" {
		t.Fatalf("expected only hash aaa for instance 1, got %+v", targetsByInstance[1])
	}
	if len(targetsByInstance[2]) != 0 {
		t.Fatalf("expected no hashes for instance 2, got %+v", targetsByInstance[2])
	}
}

func TestNormalizeInstanceIDs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     []int
		expected  []int
		shouldErr bool
	}{
		{name: "empty input", input: nil, expected: nil},
		{name: "sorts ids", input: []int{5, 2, 9}, expected: []int{2, 5, 9}},
		{name: "rejects duplicate ids", input: []int{1, 2, 1}, shouldErr: true},
		{name: "rejects non-positive ids", input: []int{1, 0}, shouldErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual, err := normalizeInstanceIDs(tc.input)
			if tc.shouldErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(actual, tc.expected) {
				t.Fatalf("unexpected normalized ids: got %v want %v", actual, tc.expected)
			}
		})
	}
}

func TestParseInstanceIDsParam(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		expected  []int
		shouldErr bool
	}{
		{name: "empty param", input: "", expected: nil},
		{name: "parses and sorts", input: "7,2,4", expected: []int{2, 4, 7}},
		{name: "ignores empty parts", input: "1, ,3", expected: []int{1, 3}},
		{name: "rejects invalid token", input: "1,abc", shouldErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual, err := parseInstanceIDsParam(tc.input)
			if tc.shouldErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(actual, tc.expected) {
				t.Fatalf("unexpected parsed ids: got %v want %v", actual, tc.expected)
			}
		})
	}
}

// TestBulkAction_UnifiedScopeRejectsBareHashes pins the unified-scope contract:
// a hash names a torrent, not an instance, so hashes without targets are a 400
// instead of a fan-out to every instance that holds the hash (issue #2527).
func TestBulkAction_UnifiedScopeRejectsBareHashes(t *testing.T) {
	t.Parallel()

	handler := &TorrentsHandler{}
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"action": "recheck",
		"hashes": []string{"shared"},
	})

	rec := httptest.NewRecorder()
	handler.BulkAction(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "targets")
}

// TestBulkAction_UnifiedScopeTargetsReachOneInstance is the mirror case: the same
// payload with targets for one instance reaches only that instance, even though
// the other instance holds the same hash.
func TestBulkAction_UnifiedScopeTargetsReachOneInstance(t *testing.T) {
	t.Parallel()

	db := testdb.NewMigratedSQLite(t, "torrents-bulk-action")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	shared := []qbt.Torrent{{Name: "Shared", Hash: "shared", AddedOn: 1}}
	clients := make(map[int]*qbittorrent.Client, 2)
	instanceIDs := make(map[string]int, 2)
	recheckHits := map[string]*atomic.Int64{"alpha": {}, "beta": {}}
	for name, hits := range recheckHits {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/torrents/recheck":
				hits.Add(1)
			case "/api/v2/sync/maindata":
				_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{"shared":{"name":"Shared","hash":"shared","added_on":1}}}`))
				return
			}
			_, _ = w.Write([]byte("Ok."))
		}))
		t.Cleanup(srv.Close)

		instance, createErr := instanceStore.Create(t.Context(), name, srv.URL, "user", "pass", nil, nil, false, nil)
		require.NoError(t, createErr)
		instanceIDs[name] = instance.ID
		clients[instance.ID] = newStaleCachedClient(t, srv.URL, shared)
	}
	testutil.SetUnexportedField(t, clientPool, "clients", clients)

	handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"action":      "recheck",
		"hashes":      []string{"shared"},
		"targets":     []map[string]any{{"instanceId": instanceIDs["alpha"], "hash": "shared"}},
		"instanceIds": []int{instanceIDs["alpha"], instanceIDs["beta"]},
	})

	rec := httptest.NewRecorder()
	handler.BulkAction(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.EqualValues(t, 1, recheckHits["alpha"].Load())
	require.EqualValues(t, 0, recheckHits["beta"].Load())
}

// TestBulkAction_SelectAllExcludesHybridByV1Hash drives story 7 of #2749 through
// the handler: a unified select-all skips a hybrid torrent excluded by its v1
// hash, the same rule the copy route applies.
func TestBulkAction_SelectAllExcludesHybridByV1Hash(t *testing.T) {
	t.Parallel()

	db := testdb.NewMigratedSQLite(t, "torrents-bulk-action-exclude")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	var mu sync.Mutex
	var rechecked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/recheck":
			_ = r.ParseForm()
			mu.Lock()
			rechecked = append(rechecked, r.Form.Get("hashes"))
			mu.Unlock()
		case "/api/v2/sync/maindata":
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{"aaa":{"name":"Plain","hash":"aaa","added_on":2},"ddd":{"name":"Hybrid","hash":"ddd","infohash_v1":"eee","added_on":1}}}`))
			return
		}
		_, _ = w.Write([]byte("Ok."))
	}))
	t.Cleanup(srv.Close)

	instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	testutil.SetUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{instance.ID: newStaleCachedClient(t, srv.URL, []qbt.Torrent{
		{Name: "Plain", Hash: "aaa", AddedOn: 2},
		{Name: "Hybrid", Hash: "ddd", InfohashV1: "eee", AddedOn: 1},
	})})

	handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"action":        "recheck",
		"selectAll":     true,
		"instanceIds":   []int{instance.ID},
		"excludeHashes": []string{"EEE"},
	})

	rec := httptest.NewRecorder()
	handler.BulkAction(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"aaa"}, rechecked)
}

// regression: #3868
// TestBulkAction_SelectAllFollowsTheTorrentListRule drives Select all mode
// through the handler: it acts on the rows that its filters show, and a filter
// value that qui does not know touches nothing.
func TestBulkAction_SelectAllFollowsTheTorrentListRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		filters map[string]any
		want    []string // the rechecked hashes; nil when the action touches nothing
	}{
		{"running leaves out a paused torrent of qBittorrent 4.x", map[string]any{"status": []string{"running"}}, []string{"aaa"}},
		{"an unknown excludeStatus value", map[string]any{"excludeStatus": []string{"tpyo"}}, nil},
		{"an unknown status next to a known one", map[string]any{"status": []string{"running", "tpyo"}}, nil},
		{"an expression that does not compile", map[string]any{"expr": "Ratio >"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := testdb.NewMigratedSQLite(t, "torrents-bulk-action-rule")
			instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { _ = clientPool.Close() })

			var mu sync.Mutex
			var rechecked []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/torrents/recheck" {
					_ = r.ParseForm()
					mu.Lock()
					rechecked = append(rechecked, r.Form.Get("hashes"))
					mu.Unlock()
				}
				_, _ = w.Write([]byte("Ok."))
			}))
			t.Cleanup(srv.Close)

			instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
			require.NoError(t, err)
			testutil.SetUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{instance.ID: newStaleCachedClient(t, srv.URL, []qbt.Torrent{
				{Name: "Seeding", Hash: "aaa", State: qbt.TorrentStateUploading, AddedOn: 2},
				{Name: "Paused", Hash: "bbb", State: qbt.TorrentStatePausedUp, AddedOn: 1},
			})})

			handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
			req := newTorrentFieldRequest(t, instance.ID, map[string]any{
				"action":    "recheck",
				"selectAll": true,
				"filters":   tc.filters,
			})

			rec := httptest.NewRecorder()
			handler.BulkAction(rec, req)

			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, tc.want, rechecked)
			if tc.want == nil {
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			}
		})
	}
}

type recordingDeleter struct {
	deleteFiles []bool
	actors      []qbittorrent.DeleteActor
}

func (d *recordingDeleter) DeleteTorrents(_ context.Context, _ int, _ []string, deleteFiles bool, actor qbittorrent.DeleteActor) error {
	d.deleteFiles = append(d.deleteFiles, deleteFiles)
	d.actors = append(d.actors, actor)
	return nil
}

// A delete from the web UI or the API names the user in qui.
func TestBulkAction_DeleteNamesTheUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		payload         map[string]any
		wantDeleteFiles bool
	}{
		{"delete keeps files", map[string]any{"action": "delete"}, false},
		{"delete with the files flag", map[string]any{"action": "delete", "deleteFiles": true}, true},
		{"deleteWithFiles", map[string]any{"action": "deleteWithFiles"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deleter := &recordingDeleter{}
			handler := &TorrentsHandler{deleter: deleter}
			tt.payload["hashes"] = []string{"aaaa"}

			rec := httptest.NewRecorder()
			handler.BulkAction(rec, newTorrentFieldRequest(t, 1, tt.payload))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, []bool{tt.wantDeleteFiles}, deleter.deleteFiles)
			require.Equal(t, []qbittorrent.DeleteActor{{Kind: qbittorrent.DeleteActorUser}}, deleter.actors)
		})
	}
}
