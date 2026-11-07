// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json/v2"
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	copyGroupKaiju uint64 = 0xaa
	copyGroupAlbum uint64 = 0xbb
)

func copyGroupTorrent(hash, name string, addedOn, upSpeed int64, tracker string) qbt.Torrent {
	return qbt.Torrent{
		Hash:    hash,
		Name:    name,
		AddedOn: addedOn,
		UpSpeed: upSpeed,
		State:   qbt.TorrentStateUploading,
		Tracker: "https://" + tracker + "/announce",
	}
}

type copyGroupFixture struct {
	sm    *SyncManager
	a, b  int
	names map[int]string
}

func newCopyGroupFixture(t *testing.T) copyGroupFixture {
	t.Helper()

	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "qbittorrent-copy-groups"), make([]byte, 32))
	require.NoError(t, err)
	libraries := map[string][]qbt.Torrent{
		"a": {
			copyGroupTorrent("a1", "Kaiju.Squad.S01.1080p.WEB-DL-GRPA", 100, 5, "t1.example.invalid"),
			copyGroupTorrent("b1", "Kaiju.Squad.S01.1080p.WEB-DL-GRPA", 200, 50, "t2.example.invalid"),
			copyGroupTorrent("c1", "Kaiju Squad S01 1080p WEB-DL", 300, 1, "t1.example.invalid"),
			copyGroupTorrent("d1", "Other.Film.2020.720p.BluRay-GRPB", 150, 20, "t2.example.invalid"),
			copyGroupTorrent("e1", "Some.Album.2021.FLAC-GRPC", 50, 0, "t3.example.invalid"),
			copyGroupTorrent("f1", "Some.Album.2021.FLAC-GRPC", 60, 10, "t3.example.invalid"),
		},
		"b": {
			copyGroupTorrent("a1", "Kaiju.Squad.S01.1080p.WEB-DL-GRPA", 400, 99, "t4.example.invalid"),
		},
	}
	okTrackers := func(int) []qbt.TorrentTracker { return nil }

	f := copyGroupFixture{names: map[int]string{}}
	clients := map[int]*Client{}
	for _, name := range []string{"a", "b"} {
		instance, err := store.Create(t.Context(), name, "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		clients[instance.ID] = newListTestClient(t, instance.ID, libraries[name], okTrackers)
		f.names[instance.ID] = name
		if name == "a" {
			f.a = instance.ID
		} else {
			f.b = instance.ID
		}
	}
	f.sm = NewSyncManager(&ClientPool{instanceStore: store, clients: clients}, nil)
	return f
}

func (f copyGroupFixture) groups(read ...int) *CopyGroups {
	return NewCopyGroups(map[int]map[string]uint64{
		f.a: {"a1": copyGroupKaiju, "b1": copyGroupKaiju, "c1": copyGroupKaiju, "e1": copyGroupAlbum, "f1": copyGroupAlbum},
		f.b: {"a1": copyGroupKaiju},
	}, read)
}

func (f copyGroupFixture) row(instanceID int, view *TorrentView) string {
	out := view.Hash
	if instanceID != 0 {
		out = f.names[instanceID] + "/" + out
	}
	if g := view.CopyGroup; g != nil {
		out += " " + g.ID
		if g.FirstSorted != nil {
			out += fmt.Sprintf(" first=%s/%s", f.names[g.FirstSorted.InstanceID], g.FirstSorted.Hash)
		}
	}
	return out
}

func (f copyGroupFixture) rows(views []TorrentView) []string {
	out := make([]string, len(views))
	for i := range views {
		out[i] = f.row(0, &views[i])
	}
	return out
}

func (f copyGroupFixture) crossRows(views []CrossInstanceTorrentView) []string {
	out := make([]string, len(views))
	for i := range views {
		out[i] = f.row(views[i].InstanceID, views[i].TorrentView)
	}
	return out
}

func TestListWithoutGroupCopiesIgnoresTheMap(t *testing.T) {
	t.Parallel()

	f := newCopyGroupFixture(t)
	f.sm.SetCopyGroups(f.groups(f.a, f.b))

	single, err := f.sm.GetTorrentsWithFilters(t.Context(), f.a, 0, 0, "upspeed", "desc", "", FilterOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"b1", "d1", "f1", "a1", "c1", "e1"}, f.rows(single.Torrents))
	require.Nil(t, single.CopyGroupsReady)

	unified, err := f.sm.GetCrossInstanceTorrentsWithFilters(t.Context(), 0, 0, "upspeed", "desc", "", FilterOptions{}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"b/a1", "a/b1", "a/d1", "a/f1", "a/a1", "a/c1", "a/e1"}, f.crossRows(unified.CrossInstanceTorrents))
	require.Nil(t, unified.CopyGroupsReady)

	for _, response := range []*TorrentResponse{single, unified} {
		data, err := json.Marshal(response)
		require.NoError(t, err)
		require.NotContains(t, string(data), "copyGroupsReady")
		require.NotContains(t, string(data), "copy_group")
	}
}

func TestGroupCopiesInstanceView(t *testing.T) {
	t.Parallel()

	f := newCopyGroupFixture(t)
	f.sm.SetCopyGroups(f.groups(f.a, f.b))
	ctx := WithGroupCopies(t.Context())

	tests := []struct {
		name          string
		order         string
		limit, offset int
		filters       FilterOptions
		want          []string
		wantTotal     int
		wantHasMore   bool
	}{
		{
			name:      "descending sort places a group at its first-sorted member",
			order:     "desc",
			want:      []string{"a1 00000000000000aa first=a/b1", "b1 00000000000000aa", "c1 00000000000000aa", "d1", "e1 00000000000000bb first=a/f1", "f1 00000000000000bb"},
			wantTotal: 6,
		},
		{
			name:      "ascending sort places a group at its first-sorted member",
			order:     "asc",
			want:      []string{"e1 00000000000000bb first=a/e1", "f1 00000000000000bb", "a1 00000000000000aa first=a/c1", "c1 00000000000000aa", "b1 00000000000000aa", "d1"},
			wantTotal: 6,
		},
		{
			name:      "the oldest matching copy stands in for a representative that the filter removes",
			order:     "desc",
			filters:   FilterOptions{Hashes: []string{"b1", "c1", "d1"}},
			want:      []string{"b1 00000000000000aa first=a/b1", "c1 00000000000000aa", "d1"},
			wantTotal: 3,
		},
		{
			name:      "a group with one matching member is a plain row",
			order:     "desc",
			filters:   FilterOptions{Hashes: []string{"c1", "d1", "e1"}},
			want:      []string{"d1", "c1", "e1"},
			wantTotal: 3,
		},
		{
			name:      "two copies on the filtered tracker stay a group",
			order:     "desc",
			filters:   FilterOptions{Trackers: []string{"t1.example.invalid"}},
			want:      []string{"a1 00000000000000aa first=a/a1", "c1 00000000000000aa"},
			wantTotal: 2,
		},
		{
			name:        "a page counts top rows and holds all their members",
			order:       "desc",
			limit:       2,
			want:        []string{"a1 00000000000000aa first=a/b1", "b1 00000000000000aa", "c1 00000000000000aa", "d1"},
			wantTotal:   6,
			wantHasMore: true,
		},
		{
			name:      "an offset counts top rows",
			order:     "desc",
			limit:     2,
			offset:    2,
			want:      []string{"e1 00000000000000bb first=a/f1", "f1 00000000000000bb"},
			wantTotal: 6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response, err := f.sm.GetTorrentsWithFilters(ctx, f.a, tt.limit, tt.offset, "upspeed", tt.order, "", tt.filters)
			require.NoError(t, err)
			require.Equal(t, tt.want, f.rows(response.Torrents))
			require.Equal(t, tt.wantTotal, response.Total)
			require.Equal(t, tt.wantHasMore, response.HasMore)
			require.Equal(t, new(true), response.CopyGroupsReady)
		})
	}
}

func TestGroupCopiesHoldViewParity(t *testing.T) {
	t.Parallel()

	f := newCopyGroupFixture(t)
	f.sm.SetCopyGroups(f.groups(f.a, f.b))
	ctx := WithGroupCopies(t.Context())

	for _, order := range []string{"asc", "desc"} {
		single, err := f.sm.GetTorrentsWithFilters(ctx, f.a, 0, 0, "upspeed", order, "", FilterOptions{})
		require.NoError(t, err)
		scoped, err := f.sm.GetCrossInstanceTorrentsWithFilters(ctx, 0, 0, "upspeed", order, "", FilterOptions{}, []int{f.a})
		require.NoError(t, err)

		scopedRows := make([]TorrentView, 0, len(scoped.CrossInstanceTorrents))
		for _, row := range scoped.CrossInstanceTorrents {
			scopedRows = append(scopedRows, *row.TorrentView)
		}
		require.NotNil(t, single.Torrents[0].CopyGroup, order)
		require.Equal(t, single.Torrents, scopedRows, order)
		require.Equal(t, single.Total, scoped.Total, order)
		require.Equal(t, single.CopyGroupsReady, scoped.CopyGroupsReady, order)
	}
}

func TestGroupCopiesUnifiedView(t *testing.T) {
	t.Parallel()

	f := newCopyGroupFixture(t)
	ctx := WithGroupCopies(t.Context())

	t.Run("a hash on two instances whose younger copy sorts first", func(t *testing.T) {
		f.sm.SetCopyGroups(f.groups(f.a, f.b))
		response, err := f.sm.GetCrossInstanceTorrentsWithFilters(ctx, 0, 0, "upspeed", "desc", "", FilterOptions{}, nil)
		require.NoError(t, err)
		require.Equal(t, []string{
			"a/a1 00000000000000aa first=b/a1", "b/a1 00000000000000aa", "a/b1 00000000000000aa", "a/c1 00000000000000aa",
			"a/d1",
			"a/e1 00000000000000bb first=a/f1", "a/f1 00000000000000bb",
		}, f.crossRows(response.CrossInstanceTorrents))
		require.Equal(t, 7, response.Total)
		require.Equal(t, new(true), response.CopyGroupsReady)
	})

	t.Run("the rows of an instance outside the record stay flat", func(t *testing.T) {
		f.sm.SetCopyGroups(f.groups(f.a))
		response, err := f.sm.GetCrossInstanceTorrentsWithFilters(ctx, 0, 0, "upspeed", "desc", "", FilterOptions{}, nil)
		require.NoError(t, err)
		require.Equal(t, []string{
			"b/a1",
			"a/a1 00000000000000aa first=a/b1", "a/b1 00000000000000aa", "a/c1 00000000000000aa",
			"a/d1",
			"a/e1 00000000000000bb first=a/f1", "a/f1 00000000000000bb",
		}, f.crossRows(response.CrossInstanceTorrents))
		require.Equal(t, new(true), response.CopyGroupsReady)
	})
}

func TestGroupCopiesReady(t *testing.T) {
	t.Parallel()

	f := newCopyGroupFixture(t)
	instance, err := f.sm.clientPool.instanceStore.Create(t.Context(), "c", "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
	require.NoError(t, err)
	// An instance that never synced: its client is in failure backoff, so a
	// list read fails at once and makes no request.
	unsynced := instance.ID
	f.sm.clientPool.clients[unsynced] = &Client{}
	f.sm.clientPool.failureTracker = map[int]*failureInfo{unsynced: {nextRetry: time.Now().Add(time.Hour)}}
	ctx := WithGroupCopies(WithSkipFreshData(t.Context()))

	flatA := []string{"b1", "d1", "f1", "a1", "c1", "e1"}

	t.Run("no map gives the flat rows", func(t *testing.T) {
		sm := NewSyncManager(f.sm.clientPool, nil)
		response, err := sm.GetTorrentsWithFilters(ctx, f.a, 0, 0, "upspeed", "desc", "", FilterOptions{})
		require.NoError(t, err)
		require.Equal(t, flatA, f.rows(response.Torrents))
		require.Equal(t, new(false), response.CopyGroupsReady)

		unified, err := sm.GetCrossInstanceTorrentsWithFilters(ctx, 0, 0, "upspeed", "desc", "", FilterOptions{}, []int{unsynced})
		require.NoError(t, err)
		require.Equal(t, new(false), unified.CopyGroupsReady)
	})

	tests := []struct {
		name      string
		read      []int
		instances []int
		want      bool
	}{
		{name: "every synced instance in the record", read: []int{f.a, f.b}, instances: []int{f.a, f.b}, want: true},
		{name: "one of two synced instances in the record", read: []int{f.a}, instances: []int{f.b, f.a}, want: true},
		{name: "only a synced instance outside the record", read: []int{f.a}, instances: []int{f.b}, want: false},
		{name: "only an instance that never synced", read: []int{f.a}, instances: []int{unsynced}, want: true},
		{name: "an instance that never synced beside one outside the record", read: []int{f.a}, instances: []int{f.b, unsynced}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSyncManager(f.sm.clientPool, nil)
			sm.SetCopyGroups(f.groups(tt.read...))
			response, err := sm.GetCrossInstanceTorrentsWithFilters(ctx, 0, 0, "upspeed", "desc", "", FilterOptions{}, tt.instances)
			require.NoError(t, err)
			require.Equal(t, new(tt.want), response.CopyGroupsReady)
		})
	}

	t.Run("an instance view outside the record gives the flat rows", func(t *testing.T) {
		sm := NewSyncManager(f.sm.clientPool, nil)
		sm.SetCopyGroups(f.groups(f.b))
		response, err := sm.GetTorrentsWithFilters(ctx, f.a, 0, 0, "upspeed", "desc", "", FilterOptions{})
		require.NoError(t, err)
		require.Equal(t, flatA, f.rows(response.Torrents))
		require.Equal(t, new(false), response.CopyGroupsReady)
	})
}

func TestSetCopyGroupsNotifiesChangedInstances(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{}}, nil)
	sm.SetSyncEventSink(sink)
	updates := func() []int {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		out := sink.trackerHealthUpdates
		sink.trackerHealthUpdates = nil
		return out
	}

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}}, []int{1, 2}))
	require.Equal(t, []int{1, 2}, updates(), "a first map")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}}, []int{1, 2}))
	require.Empty(t, updates(), "an equal map")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}, 2: {"a1": 1}}, []int{1, 2}))
	require.Equal(t, []int{2}, updates(), "new entries on one instance")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}, 2: {"a1": 1}}, []int{1, 2, 3}))
	require.Equal(t, []int{3}, updates(), "an instance that joins the record")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 2, "b1": 2}, 2: {"a1": 1}}, []int{1, 2, 3}))
	require.Equal(t, []int{1}, updates(), "a changed group id")
}

func TestCompareRepresentative(t *testing.T) {
	t.Parallel()

	older := &qbt.Torrent{Hash: "bb", AddedOn: 100}
	younger := &qbt.Torrent{Hash: "aa", AddedOn: 200}
	sameAddedLowerHash := &qbt.Torrent{Hash: "aa", AddedOn: 100}

	require.Negative(t, CompareRepresentative(2, older, 1, younger), "the oldest comes first")
	require.Negative(t, CompareRepresentative(2, sameAddedLowerHash, 1, older), "then the lower infohash")
	require.Negative(t, CompareRepresentative(1, older, 2, older), "then the lower instance")
	require.Zero(t, CompareRepresentative(1, older, 1, older))
}
