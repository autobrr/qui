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
	sizes := map[string]int64{"a1": 1000, "b1": 1000, "c1": 1000, "d1": 300, "e1": 50, "f1": 50}
	for _, library := range libraries {
		for i := range library {
			library[i].Size = sizes[library[i].Hash]
		}
	}
	libraries["a"][3].State = qbt.TorrentStateStalledUp
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
	}, nil, read)
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

	single, err := f.sm.ListTorrents(t.Context(), TorrentListView{InstanceID: f.a, Sort: "upspeed", Order: "desc"})
	require.NoError(t, err)
	require.Equal(t, []string{"b1", "d1", "f1", "a1", "c1", "e1"}, f.rows(single.Torrents))
	require.Nil(t, single.CopyGroupsReady)

	unified, err := f.sm.ListTorrents(t.Context(), TorrentListView{Unified: true, Sort: "upspeed", Order: "desc"})
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

			response, err := f.sm.ListTorrents(t.Context(), TorrentListView{InstanceID: f.a, Limit: tt.limit, Offset: tt.offset, Sort: "upspeed", Order: tt.order, Filters: tt.filters, CopyGroups: true})
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

	for _, order := range []string{"asc", "desc"} {
		single, err := f.sm.ListTorrents(t.Context(), TorrentListView{InstanceID: f.a, Sort: "upspeed", Order: order, CopyGroups: true})
		require.NoError(t, err)
		scoped, err := f.sm.ListTorrents(t.Context(), TorrentListView{Unified: true, InstanceIDs: []int{f.a}, Sort: "upspeed", Order: order, CopyGroups: true})
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

	t.Run("a hash on two instances whose younger copy sorts first", func(t *testing.T) {
		f.sm.SetCopyGroups(f.groups(f.a, f.b))
		response, err := f.sm.ListTorrents(t.Context(), TorrentListView{Unified: true, Sort: "upspeed", Order: "desc", CopyGroups: true})
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
		response, err := f.sm.ListTorrents(t.Context(), TorrentListView{Unified: true, Sort: "upspeed", Order: "desc", CopyGroups: true})
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

	flatA := []string{"b1", "d1", "f1", "a1", "c1", "e1"}

	t.Run("no map gives the flat rows", func(t *testing.T) {
		sm := NewSyncManager(f.sm.clientPool, nil)
		response, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: f.a, Sort: "upspeed", Order: "desc", CopyGroups: true, CacheOnly: true})
		require.NoError(t, err)
		require.Equal(t, flatA, f.rows(response.Torrents))
		require.Equal(t, new(false), response.CopyGroupsReady)

		unified, err := sm.ListTorrents(t.Context(), TorrentListView{Unified: true, InstanceIDs: []int{unsynced}, Sort: "upspeed", Order: "desc", CopyGroups: true, CacheOnly: true})
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
			response, err := sm.ListTorrents(t.Context(), TorrentListView{Unified: true, InstanceIDs: tt.instances, Sort: "upspeed", Order: "desc", CopyGroups: true, CacheOnly: true})
			require.NoError(t, err)
			require.Equal(t, new(tt.want), response.CopyGroupsReady)
		})
	}

	t.Run("an instance view outside the record gives the flat rows", func(t *testing.T) {
		sm := NewSyncManager(f.sm.clientPool, nil)
		sm.SetCopyGroups(f.groups(f.b))
		response, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: f.a, Sort: "upspeed", Order: "desc", CopyGroups: true, CacheOnly: true})
		require.NoError(t, err)
		require.Equal(t, flatA, f.rows(response.Torrents))
		require.Equal(t, new(false), response.CopyGroupsReady)
	})
}

func TestCopyGroupsUniqueSize(t *testing.T) {
	t.Parallel()

	torrent := func(hash string, size int64) qbt.Torrent {
		return qbt.Torrent{Hash: hash, Size: size, State: qbt.TorrentStateUploading}
	}
	stalledTorrent := func(hash string, size int64) qbt.Torrent {
		t := torrent(hash, size)
		t.State = qbt.TorrentStateStalledUp
		return t
	}
	at := func(t qbt.Torrent, contentPath string) qbt.Torrent {
		t.ContentPath = contentPath
		return t
	}
	groups := NewCopyGroups(map[int]map[string]uint64{
		1: {"a1": copyGroupKaiju, "b1": copyGroupKaiju, "c1": copyGroupKaiju},
		2: {"a1": copyGroupKaiju},
	}, map[int]map[string][]SharedPart{
		// p1 holds all three episodes of a pack, and q1 holds two of them.
		1: {"p1": {{ID: 7, Size: 900}, {ID: 8, Size: 100}}, "q1": {{ID: 7, Size: 900}}, "r1": {{ID: 7, Size: 900}, {ID: 8, Size: 100}}, "s1": {{ID: 7, Size: 900}, {ID: 9, Size: 200}}},
	}, []int{1, 2})
	sizes := func(instanceID int, torrents ...qbt.Torrent) *TorrentStats {
		return (&SyncManager{}).calculateStats(torrentRefs(torrents), groups.uniqueSizes(instanceID))
	}

	tests := []struct {
		name        string
		torrents    []qbt.Torrent
		want        int64
		wantSeeding int64
	}{
		{name: "two copies in one instance count one time", torrents: []qbt.Torrent{torrent("a1", 1000), torrent("b1", 1000)}, want: 1000, wantSeeding: 1000},
		{name: "a torrent outside a group counts in full", torrents: []qbt.Torrent{torrent("a1", 1000), torrent("b1", 1000), torrent("g1", 1000)}, want: 2000, wantSeeding: 2000},
		{name: "a copy that skips files does not lower the size of its group", torrents: []qbt.Torrent{torrent("a1", 40), torrent("b1", 50), torrent("c1", 45)}, want: 50, wantSeeding: 50},
		{name: "a stalled torrent is not seeding", torrents: []qbt.Torrent{torrent("a1", 1000), stalledTorrent("d1", 300)}, want: 1300, wantSeeding: 1000},
		{name: "copies with a content path each count one time", torrents: []qbt.Torrent{at(torrent("a1", 1000), "/data/Rel"), at(torrent("b1", 1000), "/links/Rel")}, want: 1000, wantSeeding: 1000},
		{name: "torrents without a content path share no data", torrents: []qbt.Torrent{torrent("y1", 300), torrent("z1", 300)}, want: 600, wantSeeding: 600},
		{name: "a torrent outside a group with the content path of a member counts one time", torrents: []qbt.Torrent{at(torrent("a1", 1000), "/data/Rel"), at(torrent("y1", 1000), "/data/Rel")}, want: 1000, wantSeeding: 1000},
		{name: "two torrents outside a group with one content path count one time", torrents: []qbt.Torrent{at(torrent("y1", 300), "/data/Film"), at(torrent("z1", 300), "/data/Film")}, want: 300, wantSeeding: 300},
		// regression: #4020
		{name: "a stalled member still joins its content path", torrents: []qbt.Torrent{at(torrent("a1", 1000), "/data/Rel"), at(stalledTorrent("b1", 1000), "/links/Rel"), at(torrent("y1", 1000), "/links/Rel")}, want: 1000, wantSeeding: 1000},
		{name: "torrents that share some files count each Shared part one time", torrents: []qbt.Torrent{torrent("p1", 1000), torrent("q1", 900)}, want: 1000, wantSeeding: 1000},
		{name: "a Shared part seeds when a torrent that seeds holds it", torrents: []qbt.Torrent{stalledTorrent("p1", 1000), torrent("q1", 900)}, want: 1000, wantSeeding: 900},
		// regression: #4041
		{name: "a torrent on the content path of a torrent with Shared parts counts one time", torrents: []qbt.Torrent{at(torrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1000), "/data/Pack"), torrent("q1", 900)}, want: 1000, wantSeeding: 1000},
		{name: "a torrent on that content path counts its bytes above the Shared parts", torrents: []qbt.Torrent{at(torrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1200), "/data/Pack")}, want: 1200, wantSeeding: 1200},
		{name: "two torrents with the same Shared parts on that content path hold them one time", torrents: []qbt.Torrent{at(torrent("p1", 1000), "/data/Pack"), at(torrent("r1", 1000), "/data/Pack"), at(torrent("y1", 1200), "/data/Pack")}, want: 1200, wantSeeding: 1200},
		{name: "a smaller torrent on that content path adds nothing", torrents: []qbt.Torrent{at(torrent("p1", 1000), "/data/Pack"), at(torrent("y1", 800), "/data/Pack")}, want: 1000, wantSeeding: 1000},
		{name: "a torrent that seeds on that content path seeds the Shared parts", torrents: []qbt.Torrent{at(stalledTorrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1000), "/data/Pack")}, want: 1000, wantSeeding: 1000},
		{name: "a smaller torrent that seeds on that content path seeds only its own size", torrents: []qbt.Torrent{at(stalledTorrent("p1", 1000), "/data/Pack"), at(torrent("y1", 800), "/data/Pack")}, want: 1000, wantSeeding: 800},
		{name: "a torrent that seeds on that content path does not count a part again that seeds", torrents: []qbt.Torrent{at(stalledTorrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1000), "/data/Pack"), torrent("q1", 900)}, want: 1000, wantSeeding: 1000},
		{name: "two content paths with the same Shared parts hold one set of data", torrents: []qbt.Torrent{at(stalledTorrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1000), "/data/Pack"), at(stalledTorrent("r1", 1000), "/links/Pack"), at(torrent("z1", 1000), "/links/Pack")}, want: 1000, wantSeeding: 1000},
		{name: "two content paths that share one part keep their other parts", torrents: []qbt.Torrent{at(stalledTorrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1000), "/data/Pack"), at(stalledTorrent("s1", 1100), "/links/Pack"), at(torrent("z1", 1100), "/links/Pack")}, want: 1200, wantSeeding: 1200},
		{name: "a torrent that seeds on one content path keeps the part that the other path does not hold", torrents: []qbt.Torrent{at(stalledTorrent("p1", 1000), "/data/Pack"), at(torrent("y1", 1000), "/data/Pack"), at(torrent("s1", 1100), "/links/Pack"), at(stalledTorrent("z1", 1100), "/links/Pack")}, want: 1200, wantSeeding: 1200},
		{name: "a stalled torrent on that content path seeds nothing", torrents: []qbt.Torrent{at(stalledTorrent("q1", 900), "/links/Pack"), at(stalledTorrent("y1", 900), "/links/Pack")}, want: 900, wantSeeding: 0},
		{name: "a group joins the torrents of its content paths", torrents: []qbt.Torrent{at(torrent("a1", 1000), "/data/Rel"), at(torrent("y1", 1000), "/links/Rel"), at(torrent("b1", 1000), "/links/Rel")}, want: 1000, wantSeeding: 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stats := sizes(1, tt.torrents...)
			require.Equal(t, new(tt.want), stats.UniqueSize)
			require.Equal(t, new(tt.wantSeeding), stats.UniqueSeedingSize)
		})
	}

	one := sizes(1, torrent("a1", 1000), torrent("b1", 1000))
	two := sizes(2, torrent("a1", 1000))
	require.Equal(t, int64(2000), *one.UniqueSize+*two.UniqueSize, "copies on two instances count two times")

	require.Nil(t, sizes(3, torrent("a1", 1000)).UniqueSize, "an instance outside the record has no Unique size")
	require.Nil(t, (*CopyGroups)(nil).uniqueSizes(1), "no map has no Unique size")
}

func TestUniqueSizeInstanceView(t *testing.T) {
	t.Parallel()

	f := newCopyGroupFixture(t)
	f.sm.SetCopyGroups(f.groups(f.a))

	stats := func(instanceID int, uniqueSize bool) *TorrentStats {
		t.Helper()
		response, err := f.sm.ListTorrents(t.Context(), TorrentListView{InstanceID: instanceID, NoRows: true, UniqueSize: uniqueSize, CacheOnly: true})
		require.NoError(t, err)
		return response.Stats
	}

	a := stats(f.a, true)
	require.Equal(t, new(int64(1350)), a.UniqueSize, "each Copy group counts one time")
	require.Equal(t, new(int64(1050)), a.UniqueSeedingSize, "a stalled torrent is not seeding")
	require.Equal(t, int64(1000), a.TotalSize, "the content path rule stays")

	b := stats(f.b, true)
	require.Nil(t, b.UniqueSize, "no build read the instance yet")
	require.Nil(t, b.UniqueSeedingSize)

	f.sm.SetCopyGroups(f.groups(f.a, f.b))
	require.Equal(t, new(int64(1000)), stats(f.b, true).UniqueSize, "the copy on the second instance counts there")

	require.Nil(t, stats(f.a, false).UniqueSize, "a view without the Unique size has none")
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

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}}, nil, []int{1, 2}))
	require.Equal(t, []int{1, 2}, updates(), "a first map")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}}, nil, []int{1, 2}))
	require.Empty(t, updates(), "an equal map")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}, 2: {"a1": 1}}, nil, []int{1, 2}))
	require.Equal(t, []int{2}, updates(), "new entries on one instance")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 1, "b1": 1}, 2: {"a1": 1}}, nil, []int{1, 2, 3}))
	require.Equal(t, []int{3}, updates(), "an instance that joins the record")

	sm.SetCopyGroups(NewCopyGroups(map[int]map[string]uint64{1: {"a1": 2, "b1": 2}, 2: {"a1": 1}}, nil, []int{1, 2, 3}))
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
