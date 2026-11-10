// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	viewTestPasskey = "abc123xyz"
	viewTestTracker = "https://tracker.example.invalid/" + viewTestPasskey + "/announce"
	viewTestOrigin  = "https://tracker.example.invalid"
)

// newViewTestManager serves two active instances that each hold the same two
// torrents. It returns the ID of the first instance.
func newViewTestManager(t *testing.T) (*SyncManager, int) {
	t.Helper()
	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "qbittorrent-list-view"), make([]byte, 32))
	require.NoError(t, err)
	clients := map[int]*Client{}
	ids := make([]int, 0, 2)
	for _, name := range []string{"a", "b"} {
		instance, err := store.Create(t.Context(), name, "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		clients[instance.ID] = newRuleTestClient(t, instance.ID, map[string]any{
			"torrents": map[string]any{
				"aa": map[string]any{"name": "A", "state": "stalledUP", "category": "movies", "progress": 1,
					"tracker": viewTestTracker, "trackers_count": 1,
					"magnet_uri": "magnet:?xt=urn:btih:aa&tr=" + viewTestTracker},
				"bb": map[string]any{"name": "B", "state": "downloading", "category": "tv", "progress": 0.5},
			},
		})
		ids = append(ids, instance.ID)
	}
	return NewSyncManager(&ClientPool{instanceStore: store, clients: clients}, nil), ids[0]
}

func viewTestTrackers(resp *TorrentResponse) []string {
	trackers := make([]string, 0, len(resp.Torrents)+len(resp.CrossInstanceTorrents))
	for _, row := range resp.Torrents {
		trackers = append(trackers, row.Tracker)
	}
	for _, row := range resp.CrossInstanceTorrents {
		trackers = append(trackers, row.Tracker)
	}
	return trackers
}

func TestListTorrentsHidesPasskeysUnlessTheViewStatesFullAccess(t *testing.T) {
	t.Parallel()
	sm, id := newViewTestManager(t)

	for _, scope := range []struct {
		name string
		view TorrentListView
		rows int
	}{
		{"one instance", TorrentListView{InstanceID: id}, 1},
		{"Unified view", TorrentListView{Unified: true}, 2},
	} {
		for _, access := range []struct {
			name        string
			full        bool
			wantTracker string
			wantMatches int
		}{
			{"access not stated", false, viewTestOrigin, 0},
			{"full access", true, viewTestTracker, scope.rows},
		} {
			t.Run(scope.name+"/"+access.name, func(t *testing.T) {
				t.Parallel()
				view := scope.view
				view.FullPasskeyAccess = access.full
				view.Filters = FilterOptions{Hashes: []string{"aa"}}

				resp, err := sm.ListTorrents(t.Context(), view)
				require.NoError(t, err)
				trackers := viewTestTrackers(resp)
				require.Len(t, trackers, scope.rows)
				for _, tracker := range trackers {
					require.Equal(t, access.wantTracker, tracker)
				}

				for _, expression := range []string{
					`Tracker contains "` + viewTestPasskey + `"`,
					`MagnetURI contains "` + viewTestPasskey + `"`,
				} {
					view.Filters = FilterOptions{Expr: expression}
					resp, err := sm.ListTorrents(t.Context(), view)
					require.NoError(t, err)
					require.Len(t, viewTestTrackers(resp), access.wantMatches, expression)
				}
			})
		}
	}
}

func TestListTorrentsRejectsAViewThatCannotBeRead(t *testing.T) {
	t.Parallel()
	sm, id := newViewTestManager(t)

	for _, tc := range []struct {
		name string
		view TorrentListView
		want string
	}{
		{"Unified view without rows", TorrentListView{Unified: true, NoRows: true}, "summary needs a single instance"},
		{"no rows with Copy groups", TorrentListView{InstanceID: id, NoRows: true, CopyGroups: true}, "summary has no rows to group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := sm.ListTorrents(t.Context(), tc.view)
			require.EqualError(t, err, tc.want)
			require.Nil(t, resp)
		})
	}

	totals, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: id, NoRows: true})
	require.NoError(t, err)
	require.Empty(t, totals.Torrents)
	require.Equal(t, 2, totals.Total)
	require.Equal(t, 2, totals.Stats.Total)
	require.Equal(t, 1, totals.Stats.Downloading)
}

func TestListTorrentsGivesRowsAndSidebarCountsApart(t *testing.T) {
	t.Parallel()
	sm, id := newViewTestManager(t)
	read := func(view TorrentListView) *TorrentResponse {
		view.InstanceID = id
		resp, err := sm.ListTorrents(t.Context(), view)
		require.NoError(t, err)
		return resp
	}

	both := read(TorrentListView{SidebarCounts: true})
	require.Len(t, both.Torrents, 2)
	require.Equal(t, 2, both.Counts.Status["all"])
	require.Equal(t, 1, both.Counts.Categories["movies"])

	countsOnly := read(TorrentListView{SidebarCounts: true, NoRows: true})
	require.Empty(t, countsOnly.Torrents)
	require.Equal(t, both.Counts, countsOnly.Counts)

	rowsOnly := read(TorrentListView{})
	require.Len(t, rowsOnly.Torrents, 2)
	require.Nil(t, rowsOnly.Counts)
}
